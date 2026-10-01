package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

func (o ScaffoldOptions) hasSlots() bool {
	return len(o.Translated)+len(o.Attachment)+len(o.Attachments)+len(o.Metadata) != 0
}

// modelSlotImports validates the slot field names and disk of a model scaffold
// and returns the packages its file imports.
func modelSlotImports(options ScaffoldOptions) ([]string, error) {
	imports := []string{framework + "/model"}
	seen := map[string]bool{"ID": true}
	for _, kind := range []struct {
		names []string
		flag  string
		path  string
	}{{options.Translated, "--translated", "/translations"}, {options.Attachment, "--attachment", "/attachments"}, {options.Attachments, "--attachments", "/attachments"}, {options.Metadata, "--metadata", "/metadata"}} {
		for _, name := range kind.names {
			if len(name) > 128 || !token.IsIdentifier(name) || !ast.IsExported(name) || reservedExtensionSlotNames[name] || seen[name] {
				return nil, fmt.Errorf("%s needs distinct exported Go field names other than ID, From and All", kind.flag)
			}
			seen[name] = true
		}
		if len(kind.names) != 0 && !strings.HasSuffix(imports[len(imports)-1], kind.path) {
			imports = append(imports, framework+kind.path)
		}
	}
	attachments := len(options.Attachment)+len(options.Attachments) != 0
	if attachments != (options.Disk != "") {
		return nil, fmt.Errorf("--disk names the storage disk of attachment slots; use it exactly when --attachment or --attachments is set")
	}
	if attachments {
		// storage.DiskID accepts semantic identifiers.
		if !identifier.Semantic(options.Disk) {
			return nil, fmt.Errorf("--disk needs a semantic storage disk ID")
		}
		imports = append(imports, framework+"/storage")
	}
	return imports, nil
}

func developerScaffoldImports(options ScaffoldOptions) ([]string, error) {
	switch options.Kind {
	case ModelScaffold:
		if !sqlname.Table(options.Table) || options.ID != "" {
			return nil, fmt.Errorf("model scaffold requires --table; --id is not a model option")
		}
		imports, err := modelSlotImports(options)
		if err != nil || !options.hasSlots() {
			return imports, err
		}
		// The slot declarations generation adds import the binding package;
		// list its export data with the first package load.
		return append(imports, framework+"/extensions/slots"), nil
	case DTOScaffold:
		if options.ID != "" {
			return nil, fmt.Errorf("DTO identity comes from its Go type; omit --id")
		}
		return nil, nil
	case JobScaffold:
		if !identifier.Semantic(options.Queue) {
			return nil, fmt.Errorf("job scaffold requires a semantic --queue")
		}
		return []string{framework + "/jobs", framework + "/fault"}, nil
	case CommandScaffold:
		return []string{framework + "/cli", framework + "/foundation", framework + "/fault"}, nil
	}
	return nil, fmt.Errorf("unsupported developer scaffold")
}

func renderDeveloperScaffold(pkg string, options ScaffoldOptions) (string, error) {
	switch options.Kind {
	case ModelScaffold:
		if options.hasSlots() {
			return renderSlotModel(pkg, options)
		}
		return fmt.Sprintf(`package %s

import %q

// %s is a persisted model. Add domain fields, then run foundry generate.
//foundry:model table=%s
type %s struct {
	ID model.ID[%s]
}
`, pkg, framework+"/model", options.Name, options.Table, options.Name, options.Name), nil
	case DTOScaffold:
		return fmt.Sprintf(`package %s

// %s is an explicit transport contract. Add exported JSON-tagged fields,
// then run foundry generate to create its typed codec and schema.
//foundry:dto
type %s struct {}
`, pkg, options.Name, options.Name), nil
	case JobScaffold:
		return fmt.Sprintf(`package %s

import (
	"context"
	%q
	%q
)

// %s is the owned payload of an explicitly registered job.
type %s struct {}

// %sJob declares its initial version and delivery policy.
func %sJob() jobs.Definition[%s] {
	return jobs.Define[%s](%q, 1, jobs.DefaultPolicy(%q))
}

// Handle%s performs domain work. Register it through %sJob().Declare.
func Handle%s(ctx context.Context, input %s) error {
	// Capture concrete services in a constructor when this job needs them.
	return fault.New(fault.Invalid, %q)
}
`, pkg, framework+"/jobs", framework+"/fault", options.Name, options.Name, options.Name, options.Name, options.Name, options.Name, options.ID, options.Queue, options.Name, options.Name, options.Name, options.Name, "job "+options.ID+" has not been implemented"), nil
	case CommandScaffold:
		return fmt.Sprintf(`package %s

import (
	"context"
	"flag"
	%q
	%q
	%q
)

// %sArguments owns this command's typed flags.
type %sArguments struct {}

// %sCommand parses without starting application services.
func %sCommand() cli.Command[%sArguments] {
	return cli.Define(%q, %q, cli.Flags(func(flags *flag.FlagSet, args *%sArguments) {
		// Bind typed flags here, including their defaults and help.
	}, nil))
}

// New%s resolves concrete dependencies after bootstrap. Register the result
// with %sCommand().Declare(New%s) in the shared CLI registry.
func New%s(resolver foundation.Resolver) (cli.Handler[%sArguments], error) {
	return func(ctx context.Context, args %sArguments, streams cli.Streams) error {
		return fault.New(fault.Invalid, %q)
	}, nil
}
`, pkg, framework+"/cli", framework+"/foundation", framework+"/fault", options.Name, options.Name, options.Name, options.Name, options.Name, options.ID, "Implement "+options.Name, options.Name, options.Name, options.Name, options.Name, options.Name, options.Name, options.Name, "command "+options.ID+" has not been implemented"), nil
	}
	return "", fmt.Errorf("unsupported developer scaffold")
}

// renderSlotModel renders a model with extension slot fields. Metadata slots
// get an empty DTO each; attachment slots share one disk declaration and need
// DefineExtensions, whose policies accept nothing until the application lists
// their media types, so assembly refuses them rather than guessing a policy.
func renderSlotModel(pkg string, options ScaffoldOptions) (string, error) {
	imports, err := modelSlotImports(options)
	if err != nil {
		return "", err
	}
	var source strings.Builder
	fmt.Fprintf(&source, "package %s\n\nimport (\n", pkg)
	for _, path := range imports {
		fmt.Fprintf(&source, "\t%q\n", path)
	}
	fmt.Fprintf(&source, ")\n\n// %s is a persisted model. Add domain fields, then run foundry generate.\n// Its extension slots are stored in the shared framework tables, not in columns.\n//foundry:model table=%s\ntype %s struct {\n\tID model.ID[%s]\n\n", options.Name, options.Table, options.Name, options.Name)
	for _, name := range options.Translated {
		fmt.Fprintf(&source, "\t%s translations.Text\n", name)
	}
	for _, name := range options.Attachment {
		fmt.Fprintf(&source, "\t%s attachments.One[%s]\n", name, options.Name)
	}
	for _, name := range options.Attachments {
		fmt.Fprintf(&source, "\t%s attachments.Many[%s]\n", name, options.Name)
	}
	for _, name := range options.Metadata {
		fmt.Fprintf(&source, "\t%s metadata.Value[%s%s]\n", name, options.Name, name)
	}
	source.WriteString("}\n")
	for _, name := range options.Metadata {
		fmt.Fprintf(&source, "\n// %s%s is the metadata value of %s.%s. Add JSON-tagged fields; removing or\n// renaming one later requires a metadata version increment.\n//foundry:dto\ntype %s%s struct{}\n", options.Name, name, options.Name, name, options.Name, name)
	}
	if options.Disk == "" {
		return source.String(), nil
	}
	fmt.Fprintf(&source, "\n// %sDisk names the storage disk holding %s attachments. Configure disk %q\n// in the application's storage settings; models sharing a disk can share one declaration.\nvar %sDisk = storage.DefineDisk(%q)\n", options.Name, options.Name, options.Disk, options.Name, options.Disk)
	fmt.Fprintf(&source, "\n// DefineExtensions declares %s's slot policies; omitted slots use their defaults.\nfunc (%s) DefineExtensions() %sExtensionSet {\n\treturn %sExtensionSet{\n", options.Name, options.Name, options.Name, options.Name)
	for _, name := range append(append([]string{}, options.Attachment...), options.Attachments...) {
		fmt.Fprintf(&source, "\t\t// List the accepted media types (or set an Image plan); assembly rejects\n\t\t// a policy that accepts nothing.\n\t\t%s: attachments.Policy{Disk: %sDisk, Accepted: []storage.MediaType{}},\n", name, options.Name)
	}
	source.WriteString("\t}\n}\n")
	return source.String(), nil
}
