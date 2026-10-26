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
	imports := []string{framework + "/model", ormPackage}
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
		// Handwritten slot handles use the ordinary framework binding packages.
		return append(imports, framework+"/extensions", framework+"/extensions/slots"), nil
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
		return renderORMModel(pkg, options)
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
