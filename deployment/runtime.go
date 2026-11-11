// Package deployment enters the runtime described by a deployed release.
// Applications opt in only when neither --config nor --env-file was supplied.
package deployment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// RuntimeDescriptor is the release-relative deployment descriptor path.
const RuntimeDescriptor = "config/deploy-runtime.json"

const maxDescriptorBytes = 4 << 10
const enteredEnv = "FOUNDRY_DEPLOYMENT_ENTERED"

// Deployment instance and user names follow ^[a-z][a-z0-9-]{0,30}$.
var instanceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

// Runtime contains the release profile and the instance's runtime paths.
// Use Detect to obtain it before calling Enter.
type Runtime struct {
	Release    string
	Instance   string
	User       string
	ConfigFile string
	EnvFile    string
	WorkDir    string
	PublicDir  string
}

type descriptor struct {
	Schema   int    `json:"schema"`
	Instance string `json:"instance"`
	User     string `json:"user"`
	EnvFile  string `json:"env_file"`
	WorkDir  string `json:"work_dir"`
}

// Detect reads a bounded descriptor beside the resolved executable. An absent
// descriptor returns ok=false; an invalid or unreadable descriptor returns an error.
func Detect() (Runtime, bool, error) { return detect(os.Executable) }

func resolvedExecutable(executable func() (string, error)) (string, error) {
	path, err := executable()
	if err != nil {
		return "", safeerror.WrapFault(fault.Internal, "cannot locate deployment executable", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", safeerror.WrapFault(fault.Internal, "cannot resolve deployment executable", err)
	}
	return path, nil
}

func detect(executable func() (string, error)) (Runtime, bool, error) {
	path, err := resolvedExecutable(executable)
	if err != nil {
		return Runtime{}, false, err
	}
	release := filepath.Dir(path)
	path = filepath.Join(release, RuntimeDescriptor)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Runtime{}, false, nil
	}
	if err != nil {
		return Runtime{}, false, safeerror.WrapFault(fault.Internal, "cannot inspect deployment runtime descriptor", err)
	}
	if !info.Mode().IsRegular() {
		return Runtime{}, false, safeerror.NewFault(fault.Invalid, "deployment runtime descriptor must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Runtime{}, false, safeerror.WrapFault(fault.Internal, "cannot open deployment runtime descriptor", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDescriptorBytes+1))
	if err != nil {
		return Runtime{}, false, safeerror.WrapFault(fault.Internal, "cannot read deployment runtime descriptor", err)
	}
	var wire descriptor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > maxDescriptorBytes || decoder.Decode(&wire) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return Runtime{}, false, safeerror.NewFault(fault.Invalid, "invalid deployment runtime descriptor")
	}
	if wire.Schema != 1 || !instanceName.MatchString(wire.Instance) || wire.User != wire.Instance || wire.User == "root" || !filepath.IsAbs(wire.EnvFile) || !filepath.IsAbs(wire.WorkDir) {
		return Runtime{}, false, safeerror.NewFault(fault.Invalid, "invalid deployment runtime descriptor fields")
	}
	return Runtime{
		Release: release, Instance: wire.Instance, User: wire.User,
		ConfigFile: filepath.Join(release, "config/deploy.toml"), EnvFile: wire.EnvFile,
		WorkDir: wire.WorkDir, PublicDir: filepath.Join(release, "public"),
	}, true, nil
}

type process struct {
	platform   string
	executable func() (string, error)
	euid       func() int
	lookPath   func(string) (string, error)
	exec       func(string, []string, []string) error
	chdir      func(string) error
	lookupEnv  func(string) (string, bool)
	setenv     func(string, string) error
	environ    func() []string
}

// Enter replaces a root Linux process with runuser and forwards args unchanged
// (pass os.Args[1:]). Other users enter WorkDir and default WEB__PUBLIC_DIR to
// PublicDir only when unset. After Enter, load ConfigFile and EnvFile explicitly.
// A successful root exec never returns. Root on other platforms is unsupported.
func (r Runtime) Enter(args []string) error {
	return r.enter(args, process{
		platform: runtime.GOOS, executable: os.Executable, euid: os.Geteuid,
		lookPath: exec.LookPath, exec: syscall.Exec, chdir: os.Chdir,
		lookupEnv: os.LookupEnv, setenv: os.Setenv, environ: os.Environ,
	})
}

func (r Runtime) enter(args []string, p process) error {
	if p.euid() == 0 {
		if _, entered := p.lookupEnv(enteredEnv); entered {
			return safeerror.NewFault(fault.Invalid, "deployment application user must not be root")
		}
		if p.platform != "linux" {
			return safeerror.NewFault(fault.Invalid, "deployment root entry requires Linux runuser")
		}
		path, err := resolvedExecutable(p.executable)
		if err != nil {
			return err
		}
		runuser, err := p.lookPath("runuser")
		if err != nil {
			runuser, err = p.lookPath("/usr/sbin/runuser")
		}
		if err != nil {
			return safeerror.WrapFault(fault.Missing, "deployment entry requires runuser", err)
		}
		argv := append([]string{runuser, "-u", r.User, "--", path}, args...)
		env := append(p.environ(), enteredEnv+"=1")
		return safeerror.WrapFault(fault.Internal, "cannot enter deployment application user", p.exec(runuser, argv, env))
	}
	if err := p.chdir(r.WorkDir); err != nil {
		return safeerror.WrapFault(fault.Internal, "cannot enter deployment work directory", err)
	}
	if _, exists := p.lookupEnv("WEB__PUBLIC_DIR"); !exists {
		if err := p.setenv("WEB__PUBLIC_DIR", r.PublicDir); err != nil {
			return safeerror.WrapFault(fault.Internal, "cannot set deployment public directory", err)
		}
	}
	return nil
}
