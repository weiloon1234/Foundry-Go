// Package doctor performs bounded, offline prerequisite inspection. It neither
// compiles consumer packages nor opens application services or configuration.
package doctor

import (
	"context"
	"encoding/json"
	"go/version"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

type Options struct {
	Dir, Go, Gopls, Python string
	RequireGopls           bool
	Timeout                time.Duration
	// Framework is the running tool's framework build. A known release must
	// equal the consumer's selected framework version.
	Framework frameworkinfo.Build
}
type Check struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Passed   bool   `json:"passed"`
	Detail   string `json:"detail"`
}
type Report struct {
	FrameworkAPI manifest.Version `json:"framework_api"`
	// FrameworkModule is the tool's framework module version when its build
	// records one; development builds and local replacements leave it empty.
	FrameworkModule string  `json:"framework_module,omitempty"`
	Checks          []Check `json:"checks"`
}

func (r Report) Check() error {
	for _, check := range r.Checks {
		if check.Required && !check.Passed {
			return safeerror.NewFault(fault.Missing, "doctor prerequisites failed; inspect the reported checks")
		}
	}
	return nil
}

type probe func(context.Context, Options, string, ...string) ([]byte, error)
type moduleInfo struct {
	Path, Version, GoVersion, Dir string
	Main                          bool
	Replace                       *moduleInfo
	Error                         *struct{ Err string }
}

func (m moduleInfo) selection() frameworkinfo.Selection {
	selection := frameworkinfo.Selection{Path: m.Path, Main: m.Main, Version: m.Version}
	if m.Replace != nil {
		selection.Path = m.Replace.Path
		selection.Version, selection.Local = m.Replace.Version, m.Replace.Version == ""
	}
	return selection
}

// Framework is the framework module a consumer selects, read without downloads or compilation.
type Framework struct {
	Dir       string
	Selection frameworkinfo.Selection
}

func SelectedFramework(ctx context.Context, options Options) (Framework, error) {
	return selectedFramework(ctx, options, run)
}

func selectedFramework(ctx context.Context, options Options, execute probe) (Framework, error) {
	if options.Go == "" {
		options.Go = "go"
	}
	data, err := execute(ctx, options, options.Go, "list", "-m", "-json", frameworkinfo.ModulePath)
	var module moduleInfo
	if err != nil || json.Unmarshal(data, &module) != nil || module.Error != nil || module.Path != frameworkinfo.ModulePath || module.Dir == "" {
		return Framework{}, safeerror.New("The selected Foundry-Go module could not be read. Check --dir, run `go mod download " + frameworkinfo.ModulePath + "` in the application module, and see `go tool foundry doctor` if it still fails")
	}
	return Framework{Dir: module.Dir, Selection: module.selection()}, nil
}

func Inspect(ctx context.Context, options Options) (Report, error) { return inspect(ctx, options, run) }
func inspect(ctx context.Context, options Options, execute probe) (Report, error) {
	var result Report
	if ctx == nil || options.Timeout <= 0 || options.Timeout > time.Minute {
		return result, safeerror.NewFault(fault.Invalid, "doctor requires a context and timeout between zero and one minute")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if options.Dir == "" {
		options.Dir = "."
	}
	if options.Go == "" {
		options.Go = "go"
	}
	if options.Gopls == "" {
		options.Gopls = "gopls"
	}
	if options.Python == "" {
		options.Python = "python3"
	}
	result.FrameworkAPI = manifest.FrameworkVersion
	result.FrameworkModule = options.Framework.Version
	add := func(name string, required, passed bool, detail string) {
		result.Checks = append(result.Checks, Check{name, required, passed, detail})
	}
	data, err := execute(ctx, options, options.Go, "env", "GOVERSION")
	selectedGo := strings.TrimSpace(string(data))
	goOK := err == nil && version.IsValid(selectedGo)
	if goOK {
		add("go", true, true, selectedGo)
	} else {
		add("go", true, false, "Go is unavailable or returned invalid version metadata; select an installed Go executable with --go")
	}
	if goOK {
		data, err = execute(ctx, options, options.Go, "list", "-m", "-json")
		var consumer moduleInfo
		if err != nil || json.Unmarshal(data, &consumer) != nil || !consumer.Main || consumer.Path == "" || consumer.Error != nil {
			add("module", true, false, "Select an existing Go module with --dir; its offline module metadata could not be read")
		} else {
			add("module", true, true, "Consumer module metadata is readable without compilation")
			framework := consumer
			if consumer.Path != frameworkinfo.ModulePath {
				data, err = execute(ctx, options, options.Go, "list", "-m", "-json", frameworkinfo.ModulePath)
				framework = moduleInfo{}
				if err == nil {
					err = json.Unmarshal(data, &framework)
				}
			}
			if err != nil || framework.Error != nil || framework.Path != frameworkinfo.ModulePath {
				add("framework", true, false, "The selected module needs an available Foundry-Go requirement or explicit local replacement; doctor never downloads it")
			} else {
				frameworkGo := framework.GoVersion
				if framework.Replace != nil && framework.Replace.GoVersion != "" {
					frameworkGo = framework.Replace.GoVersion
				}
				detail := "Selected framework metadata is readable"
				if framework.Replace != nil {
					detail += "; an explicit module replacement is active"
				}
				add("framework", true, true, detail)
				entry := filepath.Join(framework.Dir, frameworkinfo.DeployEntry)
				if _, err := os.Stat(entry); framework.Dir == "" || err != nil {
					add("deploy-tool", false, false, "The selected framework has no deploy tool")
				} else if _, err := execute(ctx, options, options.Python, "-B", entry, "help"); err != nil {
					add("deploy-tool", false, false, "Python could not run the pinned deploy tool; `go tool foundry deploy help` shows why, and --python selects another executable")
				} else {
					add("deploy-tool", false, true, "Python runs the pinned deploy tool")
				}
				selection := framework.selection()
				if verified, err := options.Framework.Check(selection); err != nil {
					add("tool-version", true, false, err.Error())
				} else if !verified {
					add("tool-version", false, false, "The tool/framework version pair is not verifiable for development builds or local replacements; run the module-pinned go tool foundry")
				} else {
					add("tool-version", true, true, "The foundry tool matches the selected framework "+selection.Version)
				}
				minimum := "go" + consumer.GoVersion
				if version.Compare("go"+frameworkGo, minimum) > 0 {
					minimum = "go" + frameworkGo
				}
				valid := version.IsValid("go"+consumer.GoVersion) && version.IsValid("go"+frameworkGo)
				if !valid {
					add("go-requirement", true, false, "A selected module has missing or invalid Go requirement metadata")
				} else if version.Compare(selectedGo, minimum) < 0 {
					add("go-requirement", true, false, "Install/select Go "+minimum+" or newer; automatic toolchain download is disabled")
				} else {
					add("go-requirement", true, true, "Selected Go satisfies the modules' "+minimum+" requirement")
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	data, err = execute(ctx, options, options.Gopls, "version")
	goplsOK := err == nil && len(data) > 0 && strings.Contains(string(data), "gopls")
	if goplsOK {
		add("gopls", options.RequireGopls, true, "The selected gopls executable responds; use agent completion to inspect this consumer's actual API")
	} else {
		add("gopls", options.RequireGopls, false, "Select an installed gopls executable with --gopls; doctor never installs tools")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, result.Check()
}
