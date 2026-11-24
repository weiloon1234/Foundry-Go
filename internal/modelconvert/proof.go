package modelconvert

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Generator writes owned generated files into the temporary package directory.
// Generator exists only for foundry models convert; remove it after migration.
type Generator interface {
	Generate(context.Context, string) error
}

// GeneratorFunc exists only for foundry models convert; remove it after migration.
type GeneratorFunc func(context.Context, string) error

// Generate exists only for foundry models convert; remove it after migration.
func (f GeneratorFunc) Generate(ctx context.Context, dir string) error { return f(ctx, dir) }

// ProofOptions identifies the current framework and a scratch directory. No
// command is run in the original package, and no original file is modified.
// ProofOptions exists only for foundry models convert; remove it after migration.
type ProofOptions struct {
	Generator                            Generator
	PublicationValidator                 PublicationValidator
	FrameworkDirectory, ScratchDirectory string
}

// PublicationValidator supplies the publisher's read-only ownership checks.
// It exists only for foundry models convert; remove it after migration.
type PublicationValidator interface {
	Validate(context.Context, string, []File) error
}

// Publisher is the command's guarded, recoverable source/output publisher. It
// must check Before bytes and publish the entire batch atomically or recoverably.
// Publisher exists only for foundry models convert; remove it after migration.
type Publisher interface {
	Publish(context.Context, string, []File) error
}

// Verified contains only a proven, immutable source/output batch.
// Verified exists only for foundry models convert; remove it after migration.
type Verified struct {
	directory string
	files     []File
	sources   map[string][]byte
}

// Files exists only for foundry models convert; remove it after migration.
func (v *Verified) Files() []File {
	var out []File
	for _, f := range v.files {
		out = append(out, File{Name: f.Name, Before: bytes.Clone(f.Before), After: bytes.Clone(f.After)})
	}
	return out
}

// Publish rechecks original source bytes before handing the proven batch to the
// command's ownership/recovery boundary. Unproved plans have no publish method.
// Publish exists only for foundry models convert; remove it after migration.
func (v *Verified) Publish(ctx context.Context, p Publisher) error {
	if v == nil || p == nil {
		return reject("publication requires a verified conversion and publisher")
	}
	for name, before := range v.sources {
		data, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(data, before) {
			return reject("source changed since conversion proof: " + filepath.Base(name))
		}
	}
	return safeerror.Prefix("publish proven model conversion", p.Publish(ctx, v.directory, v.publicationFiles()))
}

func (v *Verified) publicationFiles() []File {
	files := v.Files()
	changed := map[string]bool{}
	for _, f := range files {
		changed[f.Name] = true
	}
	for path, data := range v.sources {
		name := filepath.Base(path)
		if !changed[name] {
			files = append(files, File{Name: name, Before: bytes.Clone(data), After: bytes.Clone(data)})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}

// Prove snapshots the package before and after generation in a private module
// copy. Any refusal, metadata difference or changed behavior rejects the batch.
// Prove exists only for foundry models convert; remove it after migration.
func Prove(ctx context.Context, plan *Plan, options ProofOptions) (result *Verified, resultErr error) {
	stage := "validate proof inputs"
	defer func() {
		if resultErr == nil {
			return
		}
		models := []string{}
		if plan != nil {
			for _, model := range plan.Models {
				models = append(models, model.Name)
			}
		}
		message, ok := safeerror.OwnedMessage(resultErr)
		if !ok {
			message = fmt.Sprintf("unowned %T error; diagnostic unavailable", resultErr)
		}
		resultErr = safeerror.Owned("models "+strings.Join(models, ", ")+": "+stage+": "+message, resultErr)
	}()
	if plan == nil || options.Generator == nil {
		return nil, reject("proof requires a plan and generator")
	}
	if len(plan.Refused) > 0 {
		return nil, reject("package contains refused models; no conversion published")
	}
	if len(plan.Models) == 0 {
		return &Verified{directory: plan.Directory, sources: plan.inputs}, nil
	}
	if options.FrameworkDirectory == "" {
		return nil, reject("proof requires the framework source directory")
	}
	stage = "prepare snapshot workspace"
	root, err := moduleRoot(plan.Directory)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	temp, err := os.MkdirTemp(options.ScratchDirectory, "foundry-model-proof-")
	if err != nil {
		return nil, safeerror.Owned("create proof workspace: "+err.Error(), err)
	}
	defer os.RemoveAll(temp)
	copyRoot := filepath.Join(temp, "module")
	if err := copyModule(ctx, root, copyRoot); err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	relative, err := filepath.Rel(root, plan.Directory)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	dir := filepath.Join(copyRoot, relative)
	for name, source := range plan.sources {
		data, err := os.ReadFile(filepath.Join(dir, filepath.Base(name)))
		if err != nil || !bytes.Equal(data, source) {
			return nil, reject("source changed since analysis: " + filepath.Base(name))
		}
	}
	module, err := os.ReadFile(filepath.Join(copyRoot, "go.mod"))
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	if !strings.HasPrefix(string(module), "module "+framework+"\n") {
		if err := runGo(ctx, copyRoot, "mod", "edit", "-replace="+framework+"="+options.FrameworkDirectory); err != nil {
			return nil, safeerror.Owned(err.Error(), err)
		}
	}
	stage = "snapshot original declarations"
	before, err := capture(ctx, dir, plan)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	original, err := packageFiles(dir)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	for _, file := range plan.Files {
		if file.Name == "" || filepath.Base(file.Name) != file.Name || !strings.HasSuffix(file.Name, ".go") {
			return nil, reject("staged source name is outside the package")
		}
		if err := os.WriteFile(filepath.Join(dir, file.Name), file.After, 0600); err != nil {
			return nil, safeerror.Owned("stage converted source: "+err.Error(), err)
		}
	}
	outsideBefore, err := outsidePackage(copyRoot, dir)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	stage = "generate staged declarations"
	if err := options.Generator.Generate(ctx, dir); err != nil {
		return nil, safeerror.Prefix("generate staged model declarations", err)
	}
	outsideAfter, err := outsidePackage(copyRoot, dir)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	if len(outsideBefore) != len(outsideAfter) {
		return nil, reject("generator changed files outside its package")
	}
	for path, digest := range outsideBefore {
		if outsideAfter[path] != digest {
			return nil, reject("generator changed files outside its package")
		}
	}
	stage = "check generated declaration types and behaviors"
	packages, err := generate.LoadAnalysis(ctx, dir, []string{"."})
	if err != nil {
		return nil, safeerror.Prefix("load generated table inventory", err)
	}
	expected := map[string]bool{}
	for _, model := range plan.snapshotModels {
		expected[model.Variable] = true
	}
	var mismatches []string
	for _, pkg := range packages {
		if err := checkTableTypes(plan.analysis, pkg, plan.snapshotModels); err != nil {
			mismatches = append(mismatches, err.Error())
		}
		if err := checkBehaviorBindings(pkg, plan); err != nil {
			mismatches = append(mismatches, err.Error())
		}
		for _, name := range pkg.Types.Scope().Names() {
			object := pkg.Types.Scope().Lookup(name)
			if _, ok := object.(*types.Var); !ok || tableModel(object.Type()) == "" {
				continue
			}
			if !expected[name] && outsideDirective(pkg.Types.Path(), object.Type()) == "" {
				return nil, reject("generator introduced an unproved table: " + name)
			}
		}
	}
	if len(mismatches) > 0 {
		return nil, reject(strings.Join(mismatches, "\n"))
	}
	stage = "snapshot generated declarations"
	after, err := capture(ctx, dir, plan)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	stage = "compare declaration snapshots"
	for _, model := range plan.snapshotModels {
		stage = "compare model " + model.Name
		a, b := before[model.Variable], after[model.Variable]
		if len(a) == 0 || len(b) == 0 {
			return nil, reject("snapshot missing for model " + model.Name)
		}
		a, err = normalizeExtracted(a, b, model)
		if err != nil {
			return nil, safeerror.Owned(err.Error(), err)
		}
		differences, err := orm.FoundryExplainDeclarationSnapshots(a, b, sqlowner.Metadata())
		if err != nil {
			return nil, safeerror.Owned(err.Error(), err)
		}
		if len(differences) > 0 {
			position := plan.analysis.Fset.Position(plan.analysis.Types.Scope().Lookup(model.Name).Pos())
			for _, difference := range differences {
				mismatches = append(mismatches, position.String()+": model "+model.Name+": declaration changed: "+difference)
			}
		}
	}
	if len(mismatches) > 0 {
		return nil, reject(strings.Join(mismatches, "\n"))
	}
	stage = "validate publication and retained behavior"
	current, err := packageFiles(dir)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	verified := &Verified{directory: plan.Directory, sources: map[string][]byte{}}
	for name, data := range plan.inputs {
		verified.sources[name] = bytes.Clone(data)
	}
	names := map[string]bool{}
	for name := range original {
		names[name] = true
	}
	for name := range current {
		names[name] = true
	}
	for name := range names {
		if !bytes.Equal(original[name], current[name]) {
			verified.files = append(verified.files, File{Name: name, Before: bytes.Clone(original[name]), After: bytes.Clone(current[name])})
		}
	}
	sort.Slice(verified.files, func(i, j int) bool { return verified.files[i].Name < verified.files[j].Name })
	if options.PublicationValidator != nil {
		if err := options.PublicationValidator.Validate(ctx, plan.Directory, verified.publicationFiles()); err != nil {
			return nil, safeerror.Prefix("validate conversion ownership", err)
		}
	}
	if err := checkBehaviors(dir, plan, options.PublicationValidator != nil); err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	return verified, nil
}

func normalizeExtracted(before, after json.RawMessage, m Model) (json.RawMessage, error) {
	var a, b map[string]json.RawMessage
	if err := json.Unmarshal(before, &a); err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	if err := json.Unmarshal(after, &b); err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	for _, behavior := range m.Behaviors {
		if !behavior.Inline {
			continue
		}
		var actual string
		if err := json.Unmarshal(b[behavior.Option], &actual); err != nil {
			return nil, safeerror.Owned(err.Error(), err)
		}
		if !strings.HasSuffix(actual, "."+behavior.Name) {
			return nil, reject("extracted behavior does not match named runtime factory: " + behavior.Name)
		}
		a[behavior.Option] = b[behavior.Option]
	}
	return json.Marshal(a)
}
func checkBehaviors(dir string, p *Plan, ownedGenerated bool) error {
	files, err := packageFiles(dir)
	if err != nil {
		return err
	}
	functions := map[string]string{}
	for name, data := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, data, parser.ParseComments)
		if err != nil {
			return safeerror.Owned("parse generated behavior: "+err.Error(), err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				key := fn.Name.Name
				if fn.Recv != nil {
					key = nodeText(fset, fn.Recv.List[0].Type) + "." + key
				}
				body := *fn
				body.Doc = nil
				functions[key] = nodeText(fset, &body)
			}
		}
	}
	for _, model := range p.Models {
		for _, behavior := range model.Behaviors {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "behavior.go", "package p\n"+behavior.After, parser.ParseComments)
			if err != nil {
				return safeerror.Owned("parse expected behavior: "+err.Error(), err)
			}
			if functions[behavior.Name] != nodeText(fset, f.Decls[0]) {
				return reject("model " + model.Name + ": behavior body changed: " + behavior.Name)
			}
		}
	}
	// A generator may create owned output, but may not rewrite staged handwritten code.
	for path, data := range p.sources {
		expected := data
		for _, file := range p.Files {
			if file.Name == filepath.Base(path) {
				expected = file.After
				break
			}
		}
		if !bytes.Equal(files[filepath.Base(path)], expected) {
			if ownedGenerated && strings.HasSuffix(path, "_foundry.gen.go") {
				continue
			}
			return reject("generator changed handwritten source: " + filepath.Base(path))
		}
	}
	return nil
}
func capture(ctx context.Context, dir string, p *Plan) (map[string]json.RawMessage, error) {
	var source strings.Builder
	fmt.Fprintf(&source, "package %s\nimport (\"encoding/json\";\"os\";\"testing\";orm %q", p.PackageName, ormPath)
	hasSlots := false
	for _, m := range p.snapshotModels {
		if len(m.Owners) > 0 {
			hasSlots = true
		}
	}
	if hasSlots {
		fmt.Fprintf(&source, ";extensions %q", framework+"/extensions")
	}
	source.WriteString(")\nfunc TestFoundryConversionSnapshot(t *testing.T){ result:=map[string]json.RawMessage{}\n")
	for _, m := range p.snapshotModels {
		source.WriteString("{var slots []orm.DeclarationSlot;var owners []orm.DeclarationOwner\n")
		for _, expression := range m.Slots {
			fmt.Fprintf(&source, "{s:=%s;slots=append(slots,orm.DeclarationSlot{Field:s.Field,Kind:s.Kind,Name:s.Name,Storage:s.Storage})}\n", expression)
		}
		for _, owner := range m.Owners {
			fmt.Fprintf(&source, "owners=append(owners,orm.DeclarationOwner{Name:string(%s.Name()),Model:extensions.ORMIdentity(%s).ModelName(),StorageModel:%s.StorageModel()})\n", owner, owner, owner)
		}
		identity := "\"\""
		if m.Identity != "" {
			identity = "(*new(" + m.Name + ")).FoundryReference().ModelName()"
		}
		fmt.Fprintf(&source, "data,err:=orm.DeclarationSnapshot(%s,%s,slots,owners);if err!=nil{t.Fatal(err)};result[%q]=data}\n", m.Variable, identity, m.Variable)
	}
	source.WriteString("data,err:=json.Marshal(result);if err!=nil{t.Fatal(err)};if err=os.WriteFile(os.Getenv(\"FOUNDRY_MODEL_SNAPSHOT\"),data,0600);err!=nil{t.Fatal(err)}}\n")
	path := filepath.Join(dir, "zz_foundry_conversion_snapshot_test.go")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil, reject("snapshot test path already exists")
	}
	if err := os.WriteFile(path, []byte(source.String()), 0600); err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	defer os.Remove(path)
	resultPath := filepath.Join(dir, ".foundry-conversion-snapshot.json")
	defer os.Remove(resultPath)
	cmd := exec.CommandContext(ctx, "go", "test", "-p", "4", "-count=1", "-run", "^TestFoundryConversionSnapshot$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "FOUNDRY_MODEL_SNAPSHOT="+resultPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, safeerror.Owned("model snapshot subprocess failed: "+err.Error()+": "+string(output), err)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, safeerror.Owned("read declaration snapshots: "+err.Error(), err)
	}
	result := map[string]json.RawMessage{}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, safeerror.Owned("decode declaration snapshots: "+err.Error(), err)
	}
	return result, nil
}
func runGo(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return safeerror.Owned("conversion Go command failed: "+err.Error()+": "+string(output), err)
	}
	return nil
}
func moduleRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", reject("conversion package has no module root")
		}
		dir = parent
	}
}
func copyModule(ctx context.Context, from, to string) error {
	embeds, err := moduleEmbeds(ctx, from)
	if err != nil {
		return err
	}
	embedDirectories := map[string]bool{}
	for path := range embeds {
		for dir := filepath.Dir(path); dir != from && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			embedDirectories[dir] = true
		}
	}
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return safeerror.Owned("read proof module: "+err.Error(), err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel != "." && (strings.HasPrefix(entry.Name(), ".env") || entry.Name() == ".git" || strings.HasPrefix(entry.Name(), ".") && !embedDirectories[path] || entry.Name() == "deploy" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(to, rel), 0700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".env") || (filepath.ToSlash(rel) == "config/app.toml" || strings.HasSuffix(filepath.ToSlash(rel), "/config/app.toml")) || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		if !embeds[path] && !strings.HasSuffix(entry.Name(), ".go") && entry.Name() != "go.mod" && entry.Name() != "go.sum" && entry.Name() != ".foundry-gen.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return safeerror.Owned("read proof source: "+err.Error(), err)
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0600)
	})
}
func packageFiles(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, safeerror.Owned(err.Error(), err)
	}
	result := map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".env") || (!strings.HasSuffix(entry.Name(), ".go") && entry.Name() != ".foundry-gen.json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, reject("package contains a symlinked source")
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, safeerror.Owned(err.Error(), err)
		}
		result[entry.Name()] = data
	}
	return result, nil
}

func outsidePackage(root, packageDir string) (map[string][32]byte, error) {
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".env") || entry.Name() == "app.toml" {
			return reject("generator introduced private configuration")
		}
		if filepath.Dir(path) == packageDir && entry.Name() == frameworkinfo.GenerationGuard {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Size() != 0 {
				return reject("invalid generated publication guard")
			}
			return nil
		}
		if filepath.Dir(path) == packageDir && (strings.HasSuffix(entry.Name(), ".go") || entry.Name() == ".foundry-gen.json") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return reject("generator introduced a symlink outside its package")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		return nil, safeerror.Owned("inspect proof module files: "+err.Error(), err)
	}
	return result, nil
}

// Let Go resolve embed patterns, including directory and all: semantics.
func moduleEmbeds(ctx context.Context, root string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-deps", "-json", "./...")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, safeerror.Owned("list proof embedded assets: "+err.Error()+": "+stderr.String(), err)
	}
	assets := map[string]bool{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg struct {
			Dir        string
			EmbedFiles []string
		}
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, safeerror.Owned("decode proof embedded assets: "+err.Error(), err)
		}
		relative, err := filepath.Rel(root, pkg.Dir)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		for _, name := range pkg.EmbedFiles {
			assets[filepath.Join(pkg.Dir, name)] = true
		}
	}
	return assets, nil
}
