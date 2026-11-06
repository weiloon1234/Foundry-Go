package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func writeConfigFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvFileGrammar(t *testing.T) {
	path := writeConfigFile(t, "local.env", "# comment\r\n\nPLAIN = hello world # comment\nEMPTY=\nHASH=a#b\nSINGLE=' literal \\n $HOME ' # comment\nDOUBLE=\"a\\\"b\\\\c\"\nUNICODE=你好\nSHELL=$(touch should-not-exist)\nTICKS=`whoami`\n")
	document, err := readEnvFile(EnvFile{Path: path, Name: "local.env"})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string)
	for key, value := range document.values {
		got[key] = value.text
	}
	want := map[string]string{"PLAIN": "hello world", "EMPTY": "", "HASH": "a#b", "SINGLE": " literal \\n $HOME ", "DOUBLE": "a\"b\\c", "UNICODE": "你好", "SHELL": "$(touch should-not-exist)", "TICKS": "`whoami`"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("environment values did not preserve the documented literal grammar")
	}
}

func TestEnvFileLiteralValues(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                 "",
		"# comment":                        "",
		"hello\t# comment":                 "hello",
		"a#b":                              "a#b",
		"${HOME} $HOME $(whoami) `whoami`": "${HOME} $HOME $(whoami) `whoami`",
		`"${HOME} $HOME"`:                  "${HOME} $HOME",
		`'a\b"c'`:                          `a\b"c`,
		`""`:                               "",
		`''`:                               "",
		`"# literal" # comment`:            "# literal",
		`unquoted\n`:                       `unquoted\n`,
	} {
		path := writeConfigFile(t, "local.env", "\t_lower9\t=\t"+raw+" \t\r\n")
		document, err := readEnvFile(EnvFile{Path: path})
		if err != nil || document.values["_lower9"].text != want {
			t.Fatal("literal quoting or whitespace grammar changed", err)
		}
	}
}

func TestEnvFileRejectsMalformedInputWithoutValues(t *testing.T) {
	for name, input := range map[string]string{
		"duplicate":         "KEY=secret-canary\nKEY=secret-canary",
		"export":            "export KEY=secret-canary",
		"invalid key":       "1KEY=secret-canary",
		"empty key":         "=secret-canary",
		"unicode key":       "你好=secret-canary",
		"punctuated key":    "BAD-KEY=secret-canary",
		"missing equals":    "secret-canary",
		"unterminated":      "KEY='secret-canary",
		"multiline":         "KEY='secret-canary\nmore'",
		"escape":            "KEY=\"secret-canary\\n\"",
		"unfinished escape": "KEY=\"secret-canary\\",
		"trailing":          "KEY='secret-canary' unexpected",
		"unspaced comment":  "KEY='secret-canary'#comment",
		"carriage return":   "KEY=secret-canary\rmore",
		"utf8":              "KEY=secret-canary\xff",
		"nul":               "KEY=secret-canary\x00",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readEnvFile(EnvFile{Path: writeConfigFile(t, "local.env", input), Name: "local.env"})
			if !errors.Is(err, fault.Invalid) || got.values != nil {
				t.Fatal("malformed environment input accepted")
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "secret-canary") {
				t.Fatal("error exposed an input value")
			}
		})
	}
}

func TestEnvFileBoundsAndExplicitPaths(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir(), writeConfigFile(t, "large.env", "KEY="+strings.Repeat("x", maxEnvBytes))} {
		if _, err := readEnvFile(EnvFile{Path: path}); !errors.Is(err, fault.Invalid) {
			t.Fatal("missing, non-regular or oversized input accepted")
		} else if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), path) || strings.Contains(err.Error(), "xxxx") {
			t.Fatal("error exposed a path or input value")
		}
	}
	path := writeConfigFile(t, "boundary.env", "KEY="+strings.Repeat("x", maxEnvBytes-4))
	if _, err := readEnvFile(EnvFile{Path: path}); err != nil {
		t.Fatal("file at the documented size bound rejected:", err)
	}
}
