package generate

import (
	"context"
	"fmt"
	"path/filepath"
)

// scaffoldWithContracts creates the DTOs a component uses, generates their
// codecs through the ordinary generator and then creates the component file
// that references them. Every target and option is checked before the first
// write, so an existing file stops the sequence early. Each step publishes
// through the guarded single-file path; if a later step fails, the earlier
// files remain valid, consumer-owned declarations.
func scaffoldWithContracts(ctx context.Context, options ScaffoldOptions) (string, error) {
	var contracts []string
	switch options.Kind {
	case EndpointScaffold:
		request, response := endpointContracts(options)
		if request != "" {
			contracts = append(contracts, request)
		}
		contracts = append(contracts, response)
	case NotificationScaffold:
		contracts = []string{options.Name + "Payload"}
	default:
		return "", fmt.Errorf("unsupported contract scaffold")
	}
	targets := []string{snake(options.Name) + "_" + string(options.Kind) + ".go"}
	for _, name := range contracts {
		if err := ValidateScaffold(ScaffoldOptions{Kind: DTOScaffold, Name: name}); err != nil {
			return "", err
		}
		targets = append(targets, snake(name)+"_"+string(DTOScaffold)+".go")
	}
	for _, target := range targets {
		old, err := readFile(filepath.Join(options.Dir, target))
		if err != nil {
			return "", err
		}
		if old.exists {
			return "", fmt.Errorf("scaffold target %s already exists; existing files are never overwritten", target)
		}
	}
	// Each scaffold requires current generated output, so each DTO's codec is
	// generated before the next file is created.
	for _, name := range contracts {
		if _, err := Scaffold(ctx, ScaffoldOptions{Dir: options.Dir, Kind: DTOScaffold, Name: name}); err != nil {
			return "", err
		}
		if _, err := Generate(ctx, options.generation()); err != nil {
			return "", fmt.Errorf("generate contracts for the %s scaffold: %w", options.Kind, err)
		}
	}
	imports, err := scaffoldImports(options)
	if err != nil {
		return "", err
	}
	return scaffoldFile(ctx, options, imports)
}
