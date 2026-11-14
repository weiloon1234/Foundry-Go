package deployment

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func bindingDocument() string {
	return `{"schema":2,"instance":"app-1","user":"app-1","env_file":"/etc/foundry/app.env","work_dir":"/var/lib/foundry/app","release":"/opt/foundry/app/releases/one","deployment_sha256":"` + strings.Repeat("a", 64) + `","unit":{"instance_id":"00000000-0000-4000-8000-000000000001","role":"http","ordinal":1},"bindings":{"schema":1,"role":"http","executables":[{"name":"checksum","path":"/usr/bin/sha256sum","source_sha256":"` + strings.Repeat("b", 64) + `"}],"endpoints":[{"name":"relay","endpoint":{"role":"relay","listener":"control","address":"127.0.0.1","port":8201},"scheme":"http"}],"bundles":[{"name":"data","path":"/run/foundry/view/data","artifact_sha256":"` + strings.Repeat("c", 64) + `"}]}}`
}

func writeBindingDocument(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBindingsConcreteValuesAndRuntimeSelection(t *testing.T) {
	path := writeBindingDocument(t, bindingDocument())
	t.Setenv(BindingsEnvironment, path)
	bindings, ok, err := LoadBindings()
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	executable, err := bindings.Executable("checksum")
	if err != nil || executable.Path != "/usr/bin/sha256sum" {
		t.Fatal(executable, err)
	}
	endpoint, err := bindings.Endpoint("relay")
	if err != nil || endpoint.Port != 8201 || endpoint.Role != "relay" {
		t.Fatal(endpoint, err)
	}
	url := endpoint.URL()
	if url.String() != "http://127.0.0.1:8201" {
		t.Fatal(url.String())
	}
	bundle, err := bindings.Bundle("data")
	if err != nil || bundle.Path != "/run/foundry/view/data" {
		t.Fatal(bundle, err)
	}
	if _, err := bindings.Endpoint("checksum"); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
	endpoint.Port = 9999
	again, _ := bindings.Endpoint("relay")
	if again.Port != 8201 {
		t.Fatal("caller changed snapshot")
	}
	runtime, ok, err := Detect()
	if err != nil || !ok || runtime.EnvFile != "/etc/foundry/app.env" || runtime.Release != "/opt/foundry/app/releases/one" {
		t.Fatal(runtime, ok, err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadBindings(); err == nil {
		t.Fatal("invalid selected descriptor fell back")
	}
	if _, ok, err := Detect(); err == nil || ok {
		t.Fatal("runtime fell back", ok, err)
	}
	again, _ = bindings.Endpoint("relay")
	if again.Port != 8201 {
		t.Fatal("snapshot changed with file")
	}
}

func TestBindingsRejectInvalidAndPrivateData(t *testing.T) {
	valid := bindingDocument()
	cases := []string{"{}", "null", valid + "{}", strings.Replace(valid, `"schema":2`, `"schema":2,"schema":2`, 1),
		strings.Replace(valid, `"ordinal":1`, `"ordinal":2`, 1), strings.Replace(valid, `"role":"http"`, `"role":"other"`, 1),
		strings.Replace(valid, "127.0.0.1", "203.0.113.10", 1), strings.Replace(valid, `"port":8201`, `"port":80`, 1),
		strings.Replace(valid, `"name":"relay"`, `"name":"checksum"`, 1),
		strings.Replace(valid, `"path":"/usr/bin/sha256sum"`, `"path":"/usr/../private-secret"`, 1),
		strings.Replace(valid, `"schema":2`, `"schema":2,"password":"private-secret"`, 1),
		valid + strings.Repeat(" ", maxBindingsBytes)}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(valid), &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "bindings")
	missing, _ := json.Marshal(raw)
	cases = append(cases, string(missing))
	for _, data := range cases {
		_, err := ReadBindings(writeBindingDocument(t, data))
		if !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "private-secret") {
			t.Fatal("invalid descriptor accepted or leaked", err)
		}
	}
	var zero Bindings
	if _, err := zero.Bundle("missing"); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
}

func publicBindingDocument() string {
	return strings.Replace(bindingDocument(), `"bundles":[`, `"public_listeners":[{"name":"relay_public","role":"relay","listener":"ingress","hostname":"relay.example.org","port":8554,"tls":"none","transport":"tcp","public_addresses":["203.0.113.10","2001:db8::10"]}],"bundles":[`, 1)
}

func TestPublicListenerBindingsSnapshot(t *testing.T) {
	for _, mode := range []TLSMode{TLSNone, TLSEdge} {
		path := writeBindingDocument(t, strings.Replace(publicBindingDocument(), `"tls":"none"`, `"tls":"`+string(mode)+`"`, 1))
		bindings, err := ReadBindings(path)
		if err != nil {
			t.Fatal(err)
		}
		public, err := bindings.PublicListener("relay_public")
		if err != nil || public.Role != "relay" || public.Listener != "ingress" || public.Hostname != "relay.example.org" ||
			public.Port != 8554 || public.TLS != mode || public.Transport != TCPTransport || len(public.PublicAddresses) != 2 {
			t.Fatal(public, err)
		}
		public.PublicAddresses[0] = public.PublicAddresses[1]
		public.Hostname = "changed.example.org"
		if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
			t.Fatal(err)
		}
		again, err := bindings.PublicListener("relay_public")
		if err != nil || again.Hostname != "relay.example.org" || again.PublicAddresses[0].String() != "203.0.113.10" {
			t.Fatal("caller or file changed the snapshot", again, err)
		}
	}
	legacy, err := ReadBindings(writeBindingDocument(t, bindingDocument()))
	if err != nil {
		t.Fatal(err)
	}
	for _, bindings := range []Bindings{{}, legacy} {
		if _, err := bindings.PublicListener("relay_public"); !errors.Is(err, fault.Missing) {
			t.Fatal(err)
		}
	}
}

func TestPublicListenerBindingsRejectMalformedValues(t *testing.T) {
	for _, change := range [][2]string{
		{`"name":"relay_public"`, `"name":"relay"`},
		{`"role":"relay","listener":"ingress"`, `"role":"bad/role","listener":"ingress"`},
		{`"listener":"ingress"`, `"listener":"bad/name"`},
		{`"hostname":"relay.example.org"`, `"hostname":"https://relay.example.org"`},
		{`"hostname":"relay.example.org"`, `"hostname":"Relay.example.org"`},
		{`"hostname":"relay.example.org"`, `"hostname":"relay.example.org."`},
		{`"port":8554`, `"port":0`}, {`"port":8554`, `"port":65536`},
		{`"tls":"none"`, `"tls":"passthrough"`}, {`"transport":"tcp"`, `"transport":"udp"`},
		{`["203.0.113.10","2001:db8::10"]`, `[]`},
		{`["203.0.113.10","2001:db8::10"]`, `null`},
		{`"203.0.113.10"`, `"127.0.0.1"`}, {`"203.0.113.10"`, `"0.0.0.0"`},
		{`"203.0.113.10"`, `"169.254.1.1"`}, {`"203.0.113.10"`, `"224.0.0.1"`},
		{`"203.0.113.10"`, `"::ffff:203.0.113.10"`}, {`"2001:db8::10"`, `"203.0.113.10"`},
		{`"2001:db8::10"`, `"2001:db8::10%eth0"`},
	} {
		data := strings.Replace(publicBindingDocument(), change[0], change[1], 1)
		if _, err := ReadBindings(writeBindingDocument(t, data)); !errors.Is(err, fault.Invalid) {
			t.Fatalf("accepted %s: %v", change[1], err)
		}
	}
}
