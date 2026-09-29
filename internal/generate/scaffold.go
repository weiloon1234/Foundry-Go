package generate

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type ScaffoldKind string

const (
	MigrationScaffold ScaffoldKind = "migration"
	SeederScaffold    ScaffoldKind = "seeder"
	ModelScaffold     ScaffoldKind = "model"
	DTOScaffold       ScaffoldKind = "dto"
	JobScaffold       ScaffoldKind = "job"
	CommandScaffold   ScaffoldKind = "command"
	// EndpointScaffold creates request/response DTOs, generates their codecs,
	// then creates the typed route, endpoint and handler stub.
	EndpointScaffold     ScaffoldKind = "endpoint"
	EnumScaffold         ScaffoldKind = "enum"
	MiddlewareScaffold   ScaffoldKind = "middleware"
	RuleScaffold         ScaffoldKind = "rule"
	EventScaffold        ScaffoldKind = "event"
	ListenerScaffold     ScaffoldKind = "listener"
	PolicyScaffold       ScaffoldKind = "policy"
	NotificationScaffold ScaffoldKind = "notification"
)

// ScaffoldKinds lists every supported kind in documentation order.
func ScaffoldKinds() []ScaffoldKind {
	return []ScaffoldKind{ModelScaffold, DTOScaffold, EndpointScaffold, EnumScaffold, JobScaffold, CommandScaffold, EventScaffold, ListenerScaffold, PolicyScaffold, MiddlewareScaffold, RuleScaffold, NotificationScaffold, MigrationScaffold, SeederScaffold}
}

// ScaffoldOptions creates a consumer-owned declaration in an existing Go package.
// Explicit identities make output deterministic; no clock, SQL schema inspection,
// automatic registration or application bootstrap is involved.
//
// Kind-specific fields: Table (model); Queue (job); Origin, Version and the
// optional Create table (migration); Method and a static Path (endpoint);
// Cases (enum); Event payload type (listener); Subject and Resource types
// (policy). Type names refer to exported types of the same package.
type ScaffoldOptions struct {
	Dir      string
	Kind     ScaffoldKind
	Name     string
	ID       string
	Origin   string
	Version  string
	Table    string
	Queue    string
	Create   string
	Method   string
	Path     string
	Cases    []string
	Event    string
	Subject  string
	Resource string
}

var legacyScaffoldName = regexp.MustCompile(`^[\pL\pN_]+_(migration|seeder)\.go$`)
var scaffoldName = regexp.MustCompile(`^[\pL\pN_]+_(migration|seeder|model|dto|job|command|endpoint|enum|middleware|rule|event|listener|policy|notification)\.go$`)

// Scaffold checks the consumer package with the proposed declaration in memory,
// then creates one new file through the shared guarded publication/recovery path.
// The file is handwritten from that point onward and never manifest-owned.
func Scaffold(ctx context.Context, options ScaffoldOptions) (string, error) {
	imports, err := scaffoldImports(options)
	if err != nil {
		return "", err
	}
	if options.Dir == "" {
		options.Dir = "."
	}
	if options.Kind == EndpointScaffold || options.Kind == NotificationScaffold {
		return scaffoldWithContracts(ctx, options)
	}
	return scaffoldFile(ctx, options, imports)
}

// scaffoldFile creates the single declaration file of options.
func scaffoldFile(ctx context.Context, options ScaffoldOptions, imports []string) (string, error) {
	tree, err := prepareRecovery(ctx, Options{Dir: options.Dir})
	if err != nil {
		return "", err
	}
	graph, err := loadGraph(ctx, options.Dir, false, tree, imports)
	if err != nil {
		return "", err
	}
	writes, err := graph.prepare(ctx)
	if err != nil {
		return "", err
	}
	if len(writes) != 1 {
		return "", fmt.Errorf("scaffolding requires one existing Go package")
	}
	input := writes[0].input
	if len(writes[0].plan.changes) != 0 {
		return "", fmt.Errorf("generated output is stale; run foundry generate for the consumer before scaffolding")
	}
	name := snake(options.Name) + "_" + string(options.Kind) + ".go"
	old, err := readFile(filepath.Join(input.dir, name))
	if err != nil {
		return "", err
	}
	if old.exists {
		return "", fmt.Errorf("scaffold target %s already exists; existing files are never overwritten", name)
	}
	data, err := renderScaffold(input.name, options)
	if err != nil {
		return "", err
	}
	overlay := make(map[string][]byte, len(input.previous)+1)
	for name, data := range input.previous {
		overlay[name] = data
	}
	overlay[name] = data
	if err := input.check(overlay, nil); err != nil {
		return "", err
	}
	plan := writePlan{changes: map[string][]byte{name: data}, before: map[string]oldFile{name: {}}, scaffold: true}
	// Existing generated declarations participated in the overlay too. Their
	// bytes/modes and manifest must remain current until the scaffold publishes.
	for target, before := range writes[0].plan.before {
		plan.before[target] = before
	}
	if err := publishBatch(ctx, input.dir, []packageWrite{{input, plan}}, graph.checkSourceTree, nil); err != nil {
		return "", err
	}
	return filepath.Join(input.dir, name), nil
}

// ValidateScaffold checks options without loading packages or touching files.
func ValidateScaffold(options ScaffoldOptions) error { _, err := scaffoldImports(options); return err }

func scaffoldImports(options ScaffoldOptions) ([]string, error) {
	if len(options.Name) > 128 || !token.IsIdentifier(options.Name) || !ast.IsExported(options.Name) {
		return nil, fmt.Errorf("scaffold needs an exported Go --name of at most 128 bytes")
	}
	if options.Kind != ModelScaffold && options.Kind != DTOScaffold && options.Kind != EnumScaffold && !identifier.Semantic(options.ID) {
		return nil, fmt.Errorf("scaffold needs a valid semantic --id")
	}
	if options.Kind != ModelScaffold && options.Table != "" {
		return nil, fmt.Errorf("--table is only valid for a model")
	}
	if options.Kind != JobScaffold && options.Queue != "" {
		return nil, fmt.Errorf("--queue is only valid for a job")
	}
	if options.Kind != MigrationScaffold && (options.Origin != "" || options.Version != "" || options.Create != "") {
		return nil, fmt.Errorf("migration origin, version and --create are only valid for a migration")
	}
	if err := validateComponentOptions(options); err != nil {
		return nil, err
	}
	var imports []string
	switch options.Kind {
	case MigrationScaffold:
		if !identifier.Semantic(options.Origin) || !identifier.Semantic(options.Version) {
			return nil, fmt.Errorf("migration scaffold needs valid --origin and introducing --version")
		}
		if options.Create != "" && !sqlname.Table(options.Create) {
			return nil, fmt.Errorf("--create needs a valid table name")
		}
		imports = []string{framework + "/database/migrate"}
	case SeederScaffold:
		imports = []string{framework + "/database/seed"}
	case ModelScaffold, DTOScaffold, JobScaffold, CommandScaffold:
		var err error
		imports, err = developerScaffoldImports(options)
		if err != nil {
			return nil, err
		}
	case EndpointScaffold, EnumScaffold, MiddlewareScaffold, RuleScaffold, EventScaffold, ListenerScaffold, PolicyScaffold, NotificationScaffold:
		imports = componentScaffoldImports(options)
	default:
		return nil, fmt.Errorf("unsupported scaffold kind")
	}
	return imports, nil
}

func renderScaffold(pkg string, options ScaffoldOptions) ([]byte, error) {
	var source string
	if options.Kind == MigrationScaffold {
		source = fmt.Sprintf(`package %s

import %q

// %sID is the immutable identity of this historical schema change.
const %sID migrate.ID = %q

// %s declares one migration. Register the returned definition explicitly.
func %s() migrate.Definition {
	return migrate.Definition{
		Key: migrate.Key{Origin: %q, ID: %sID},
		Version: %q,
		// Add typed migration prerequisites when this change depends on them.
		Requires: []migrate.Key{},
		// Add reviewed, transaction-compatible SQL. An empty list fails registry validation.
		SQL: []string{%s},
	}
}
`, pkg, framework+"/database/migrate", options.Name, options.Name, options.ID, options.Name, options.Name, options.Origin, options.Name, options.Version, createTableSQL(options.Create))
	} else if options.Kind == SeederScaffold {
		source = fmt.Sprintf(`package %s

import (
	"context"
	%q
	%q
	%q
)

// %sID names this explicitly invoked seeder.
const %sID seed.ID = %q

// %s declares transactional domain work. Register the returned definition explicitly.
func %s() seed.Definition {
	return seed.Definition{
		ID: %sID,
		Requires: []seed.ID{},
		Run: func(ctx context.Context, tx *database.Tx) error {
			// Implement repeat-safe domain work using tx and ctx.
			return fault.New(fault.Invalid, %q)
		},
	}
}
`, pkg, framework+"/database", framework+"/database/seed", framework+"/fault", options.Name, options.Name, options.ID, options.Name, options.Name, options.Name, "seeder "+options.ID+" has not been implemented")
	} else if slices.Contains([]ScaffoldKind{ModelScaffold, DTOScaffold, JobScaffold, CommandScaffold}, options.Kind) {
		var err error
		source, err = renderDeveloperScaffold(pkg, options)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		source, err = renderComponentScaffold(pkg, options)
		if err != nil {
			return nil, err
		}
	}
	return format.Source([]byte(source))
}

// Link publishes a fully synced staged file without replacing a target created
// concurrently. Staging cleanup removes only the other link, leaving the new file.
func confinedCreate(dir string) (func(string, string) error, func() error, error) {
	tree, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	return func(from, to string) error {
		a, err := filepath.Rel(dir, from)
		if err != nil {
			return err
		}
		b, err := filepath.Rel(dir, to)
		if err != nil {
			return err
		}
		return tree.Link(a, b)
	}, tree.Close, nil
}
