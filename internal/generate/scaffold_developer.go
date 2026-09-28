package generate

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

func developerScaffoldImports(options ScaffoldOptions) ([]string, error) {
	switch options.Kind {
	case ModelScaffold:
		if !sqlname.Table(options.Table) || options.ID != "" {
			return nil, fmt.Errorf("model scaffold requires --table; --id is not a model option")
		}
		return []string{framework + "/model"}, nil
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
