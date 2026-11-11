package deployment

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func deploymentExecutable(t *testing.T) (string, func() (string, error)) {
	t.Helper()
	release, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(release, "artisan")
	if err := os.WriteFile(path, nil, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(release, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	return release, func() (string, error) { return path, nil }
}

const validDescriptor = `{"schema":1,"instance":"app-1","user":"app-1","env_file":"/configuration/app.env","work_dir":"/data/app"}`

func TestDetectDeployment(t *testing.T) {
	release, executable := deploymentExecutable(t)
	if got, ok, err := detect(executable); err != nil || ok || got != (Runtime{}) {
		t.Fatalf("absent descriptor: %+v, %v, %v", got, ok, err)
	}
	path := filepath.Join(release, RuntimeDescriptor)
	if err := os.WriteFile(path, []byte(validDescriptor), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "current-artisan")
	target, _ := executable()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, ok, err := detect(func() (string, error) { return link, nil })
	want := Runtime{Release: release, Instance: "app-1", User: "app-1", ConfigFile: filepath.Join(release, "config/deploy.toml"), EnvFile: "/configuration/app.env", WorkDir: "/data/app", PublicDir: filepath.Join(release, "public")}
	if err != nil || !ok || got != want {
		t.Fatalf("symlink deployment: %+v, %v, %v", got, ok, err)
	}
	if err := os.WriteFile(path, []byte(validDescriptor+strings.Repeat(" ", maxDescriptorBytes-len(validDescriptor))), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := detect(executable); err != nil || !ok {
		t.Fatal("exact size limit rejected", err)
	}
}

func TestDetectRejectsInvalidDeployment(t *testing.T) {
	cases := map[string]string{
		"empty": "", "null": "null", "array": "[]", "malformed": "{",
		"oversize":        validDescriptor + strings.Repeat(" ", maxDescriptorBytes),
		"trailing object": validDescriptor + `{}`, "trailing null": validDescriptor + `null`,
		"trailing garbage": validDescriptor + `x`,
		"unknown":          strings.Replace(validDescriptor, `"schema":1`, `"schema":1,"secret":"private"`, 1),
		"schema":           strings.Replace(validDescriptor, `"schema":1`, `"schema":2`, 1),
		"relative env":     strings.Replace(validDescriptor, "/configuration/app.env", "app.env", 1),
		"relative work":    strings.Replace(validDescriptor, "/data/app", "app", 1),
		"different user":   strings.Replace(validDescriptor, `"user":"app-1"`, `"user":"other"`, 1),
		"root user":        strings.ReplaceAll(validDescriptor, `"app-1"`, `"root"`),
	}
	for _, name := range []string{"schema", "instance", "user", "env_file", "work_dir"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(validDescriptor), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, name)
		data, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		cases["missing "+name] = string(data)
	}
	for _, slug := range []string{"", "A", "1app", "app_name", "-app", strings.Repeat("a", 32), "app\n"} {
		encoded, _ := json.Marshal(slug)
		cases["slug "+slug] = strings.ReplaceAll(validDescriptor, `"app-1"`, string(encoded))
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			release, executable := deploymentExecutable(t)
			if err := os.WriteFile(filepath.Join(release, RuntimeDescriptor), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			got, ok, err := detect(executable)
			if !errors.Is(err, fault.Invalid) || ok || got != (Runtime{}) || strings.Contains(err.Error(), "private") {
				t.Fatalf("invalid descriptor accepted or leaked: %+v %v %v", got, ok, err)
			}
		})
	}
}

func TestDetectDeploymentFailures(t *testing.T) {
	release, executable := deploymentExecutable(t)
	if err := os.Mkdir(filepath.Join(release, RuntimeDescriptor), 0700); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := detect(executable); ok || !errors.Is(err, fault.Invalid) {
		t.Fatal("directory descriptor accepted", ok, err)
	}
	cause := errors.New("private executable detail")
	for _, lookup := range []func() (string, error){
		func() (string, error) { return "", cause },
		func() (string, error) { return filepath.Join(release, "private-missing"), nil },
	} {
		if _, ok, err := detect(lookup); ok || !errors.Is(err, fault.Internal) || strings.Contains(err.Error(), "private") {
			t.Fatal("executable error lost or leaked", ok, err)
		}
	}
}

func TestEnterDeploymentRoot(t *testing.T) {
	_, executable := deploymentExecutable(t)
	path, _ := executable()
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "path", true: "fallback"}[fallback], func(t *testing.T) {
			var lookups []string
			cause := errors.New("private exec detail")
			args := []string{"down", "--message", "two words"}
			p := process{
				platform: "linux", executable: executable, euid: func() int { return 0 },
				lookupEnv: func(key string) (string, bool) {
					if key != enteredEnv {
						t.Fatal(key)
					}
					return "", false
				},
				lookPath: func(name string) (string, error) {
					lookups = append(lookups, name)
					if fallback && name == "runuser" {
						return "", os.ErrNotExist
					}
					return "/usr/sbin/runuser", nil
				},
				environ: func() []string { return []string{"EXISTING=value"} },
				exec: func(binary string, argv, env []string) error {
					want := []string{"/usr/sbin/runuser", "-u", "app-1", "--", path, "down", "--message", "two words"}
					if binary != want[0] || !reflect.DeepEqual(argv, want) || !reflect.DeepEqual(env, []string{"EXISTING=value", enteredEnv + "=1"}) {
						t.Fatalf("exec changed arguments: %q %q %q", binary, argv, env)
					}
					return cause
				},
			}
			err := (Runtime{User: "app-1"}).enter(args, p)
			if !errors.Is(err, cause) || strings.Contains(err.Error(), "private") {
				t.Fatal("exec error lost or leaked", err)
			}
			want := []string{"runuser"}
			if fallback {
				want = append(want, "/usr/sbin/runuser")
			}
			if !reflect.DeepEqual(lookups, want) {
				t.Fatal("lookup order", lookups)
			}
		})
	}
	p := process{platform: "darwin", euid: func() int { return 0 }, lookupEnv: func(string) (string, bool) { return "", false }}
	if err := (Runtime{}).enter(nil, p); err == nil || !strings.Contains(err.Error(), "Linux runuser") {
		t.Fatal("non-Linux root accepted", err)
	}
	p.platform, p.executable = "linux", executable
	p.lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if err := (Runtime{}).enter(nil, p); !errors.Is(err, fault.Missing) {
		t.Fatal("missing runuser accepted", err)
	}
}

func TestEnterDeploymentRejectsRootReentry(t *testing.T) {
	for _, marker := range []string{"1", ""} {
		t.Run("marker="+marker, func(t *testing.T) {
			p := process{
				platform: "linux", euid: func() int { return 0 },
				lookupEnv: func(key string) (string, bool) {
					if key != enteredEnv {
						t.Fatal(key)
					}
					return marker, true
				},
				exec: func(string, []string, []string) error {
					t.Fatal("root re-entry must not exec")
					return nil
				},
			}
			err := (Runtime{User: "root-alias"}).enter(nil, p)
			if !errors.Is(err, fault.Invalid) || err.Error() != "invalid: deployment application user must not be root" {
				t.Fatal("root re-entry was not rejected safely", err)
			}
		})
	}
}

func TestEnterDeploymentNonRoot(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, value := range []string{"", "/custom/public"} {
			var operations []string
			p := process{
				euid:  func() int { return 1234 },
				chdir: func(path string) error { operations = append(operations, "chdir "+path); return nil },
				lookupEnv: func(key string) (string, bool) {
					if key != "WEB__PUBLIC_DIR" {
						t.Fatal(key)
					}
					return value, exists
				},
				setenv: func(key, value string) error { operations = append(operations, key+"="+value); return nil },
			}
			if err := (Runtime{WorkDir: "/data/app", PublicDir: "/release/public"}).enter(nil, p); err != nil {
				t.Fatal(err)
			}
			want := []string{"chdir /data/app"}
			if !exists {
				want = append(want, "WEB__PUBLIC_DIR=/release/public")
			}
			if !reflect.DeepEqual(operations, want) {
				t.Fatal("entry operations", operations)
			}
		}
	}
	cause := errors.New("private process detail")
	p := process{euid: func() int { return 1000 }, chdir: func(string) error { return cause }}
	if err := (Runtime{}).enter(nil, p); !errors.Is(err, cause) || strings.Contains(err.Error(), "private") {
		t.Fatal("chdir error lost or leaked", err)
	}
	p.chdir = func(string) error { return nil }
	p.lookupEnv = func(string) (string, bool) { return "", false }
	p.setenv = func(string, string) error { return cause }
	if err := (Runtime{}).enter(nil, p); !errors.Is(err, cause) || strings.Contains(err.Error(), "private") {
		t.Fatal("setenv error lost or leaked", err)
	}
}
