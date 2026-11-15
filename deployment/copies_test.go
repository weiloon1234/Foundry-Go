package deployment

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func copiesBindingDocument(unix, secret bool) string {
	rows := make([]string, 0, 2)
	for ordinal := 1; ordinal <= 2; ordinal++ {
		endpoint := fmt.Sprintf(`"address":"127.0.0.1","port":%d`, 8200+ordinal)
		scheme := "http"
		if unix {
			name := "worker"
			if ordinal > 1 {
				name += "@2"
			}
			directory := "/run/foundry/services/00000000-0000-4000-8000-000000000001/sockets"
			endpoint = fmt.Sprintf(`"directory":%q,"name":%q,"path":%q`, directory, name, directory+"/"+name)
			scheme = "unix"
		}
		grant := ""
		if secret {
			grant = fmt.Sprintf(`,"secret_file":"/etc/foundry/instances/app-1/copy-secrets/service-%d.secret"`, ordinal)
		}
		rows = append(rows, fmt.Sprintf(`{"ordinal":%d,"endpoint":{"role":"worker","ordinal":%d,"listener":"control",%s},"scheme":%q%s}`, ordinal, ordinal, endpoint, scheme, grant))
	}
	return strings.Replace(bindingDocument(), `"bundles":[`, `"copies":[{"name":"workers","copies":[`+strings.Join(rows, ",")+`]}],"bundles":[`, 1)
}

func TestCopiesAreOrderedOwnedSnapshots(t *testing.T) {
	for _, unix := range []bool{false, true} {
		for _, secret := range []bool{false, true} {
			bindings, err := ReadBindings(writeBindingDocument(t, copiesBindingDocument(unix, secret)))
			if err != nil {
				t.Fatal(err)
			}
			copies, err := bindings.Copies("workers")
			if err != nil || len(copies) != 2 || copies[0].Ordinal != 1 || copies[1].Ordinal != 2 ||
				copies[1].Endpoint.Role != "worker" || (copies[0].SecretFile != "") != secret {
				t.Fatal(copies, err)
			}
			if unix && copies[1].Endpoint.Name != "worker@2" || !unix && copies[1].Endpoint.Port != 8202 {
				t.Fatal(copies)
			}
			original := copies[0]
			copies[0] = Copy{Ordinal: 99, SecretFile: "/changed"}
			again, _ := bindings.Copies("workers")
			if again[0] != original {
				t.Fatal("caller changed binding snapshot")
			}
			if _, err := bindings.Copies("relay"); !errors.Is(err, fault.Missing) {
				t.Fatal(err)
			}
		}
	}
	legacy, err := ReadBindings(writeBindingDocument(t, bindingDocument()))
	if err != nil {
		t.Fatal(err)
	}
	for _, bindings := range []Bindings{{}, legacy} {
		if _, err := bindings.Copies("workers"); !errors.Is(err, fault.Missing) {
			t.Fatal(err)
		}
	}
	empty := strings.Replace(bindingDocument(), `"bundles":[`, `"copies":[{"name":"workers","copies":[]}],"bundles":[`, 1)
	bindings, err := ReadBindings(writeBindingDocument(t, empty))
	if err != nil {
		t.Fatal(err)
	}
	copies, err := bindings.Copies("workers")
	if err != nil || len(copies) != 0 {
		t.Fatal(copies, err)
	}
}

func TestCopiesRejectMalformedAndConflictingGrants(t *testing.T) {
	valid := copiesBindingDocument(false, true)
	for _, change := range [][2]string{
		{`"name":"workers"`, `"name":"relay"`},
		{`"ordinal":2`, `"ordinal":1`},
		{`"ordinal":2`, `"ordinal":129`},
		{`"role":"worker","ordinal":2`, `"role":"other","ordinal":2`},
		{`"role":"worker","ordinal":2`, `"role":"worker","ordinal":1`},
		{`"port":8202`, `"port":0`},
		{`"address":"127.0.0.1"`, `"address":"203.0.113.10"`},
		{`"scheme":"http"`, `"scheme":"udp"`},
		{`"secret_file":"/etc/foundry/instances/app-1/copy-secrets/service-1.secret"`, `"secret_file":""`},
		{`"secret_file":"/etc/foundry/instances/app-1/copy-secrets/service-1.secret"`, `"secret_file":"/etc/foundry/instances/other/copy-secrets/service-1.secret"`},
		{`,"secret_file":"/etc/foundry/instances/app-1/copy-secrets/service-1.secret"`, ``},
		{`"copies":[{"ordinal":1`, `"copies":[{"unexpected":true,"ordinal":1`},
	} {
		if _, err := ReadBindings(writeBindingDocument(t, strings.Replace(valid, change[0], change[1], 1))); !errors.Is(err, fault.Invalid) {
			t.Fatal("accepted invalid copies descriptor", change, err)
		}
	}
	badUnix := strings.Replace(copiesBindingDocument(true, false), `worker@2`, `worker@3`, -1)
	if _, err := ReadBindings(writeBindingDocument(t, badUnix)); !errors.Is(err, fault.Invalid) {
		t.Fatal("accepted socket for another ordinal", err)
	}
}

func TestPublicListenerCopyPorts(t *testing.T) {
	valid := strings.Replace(publicBindingDocument(), `"port":8554`, `"port":8554,"ports":[{"ordinal":1,"port":8554},{"ordinal":2,"port":8555}]`, 1)
	bindings, err := ReadBindings(writeBindingDocument(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := bindings.PublicListener("relay_public")
	if err != nil || len(listener.Ports) != 2 || listener.Ports[1] != (PublicPort{Ordinal: 2, Port: 8555}) {
		t.Fatal(listener, err)
	}
	listener.Ports[0].Port = 1
	again, _ := bindings.PublicListener("relay_public")
	if again.Ports[0].Port != again.Port {
		t.Fatal("caller changed public port snapshot")
	}
	for _, change := range [][2]string{
		{`"ordinal":2`, `"ordinal":1`}, {`"port":8555`, `"port":8554`},
		{`"port":8555`, `"port":65536`}, {`"port":8554`, `"port":8556`},
	} {
		if _, err := ReadBindings(writeBindingDocument(t, strings.Replace(valid, change[0], change[1], 1))); !errors.Is(err, fault.Invalid) {
			t.Fatal("accepted malformed public port collection", err)
		}
	}
	legacy, err := ReadBindings(writeBindingDocument(t, publicBindingDocument()))
	if err != nil {
		t.Fatal(err)
	}
	listener, _ = legacy.PublicListener("relay_public")
	if len(listener.Ports) != 1 || listener.Ports[0] != (PublicPort{Ordinal: 1, Port: listener.Port}) {
		t.Fatal(listener)
	}
}

func TestCopySecretFileRejectsExplicitNullAndMalformedValues(t *testing.T) {
	for _, value := range []string{`null`, `false`, `42`, `[]`, `{}`, `""`} {
		t.Run(value, func(t *testing.T) {
			document := copiesBindingDocument(false, true)
			for ordinal := 1; ordinal <= 2; ordinal++ {
				field := fmt.Sprintf(`"secret_file":"/etc/foundry/instances/app-1/copy-secrets/service-%d.secret"`, ordinal)
				document = strings.Replace(document, field, `"secret_file":`+value, 1)
			}
			if _, err := ReadBindings(writeBindingDocument(t, document)); !errors.Is(err, fault.Invalid) {
				t.Fatal("accepted explicit invalid secret file", err)
			}
		})
	}
}

func TestPublicCopyPortsRejectExplicitNullAndMalformedValues(t *testing.T) {
	for _, value := range []string{`null`, `false`, `42`, `"ports"`, `{}`, `[]`, `[null,null]`,
		`[{"ordinal":1,"port":8554,"unexpected":true},{"ordinal":2,"port":8555}]`} {
		t.Run(value, func(t *testing.T) {
			document := strings.Replace(publicBindingDocument(), `"port":8554`, `"port":8554,"ports":`+value, 1)
			if _, err := ReadBindings(writeBindingDocument(t, document)); !errors.Is(err, fault.Invalid) {
				t.Fatal("accepted explicit invalid public ports", err)
			}
		})
	}
}
