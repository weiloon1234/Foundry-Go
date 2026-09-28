package secret_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestExplicitRevealAndSafeBoundaries(t *testing.T) {
	value := secret.New("private-credential")
	if value.Reveal() != "private-credential" || value.IsZero() {
		t.Fatal("secret lost")
	}
	for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
		if strings.Contains(fmt.Sprintf(format, value), "private-credential") {
			t.Fatal("format leaked secret")
		}
	}
	data, err := json.Marshal(struct{ Password secret.String }{value})
	if err != nil || strings.Contains(string(data), "private-credential") {
		t.Fatalf("JSON leaked secret: %s %v", data, err)
	}
	data, err = value.MarshalText()
	if err != nil || string(data) != secret.Redacted {
		t.Fatal("text exposed secret")
	}
	var parsed secret.String
	if err := parsed.UnmarshalText([]byte("input")); err != nil || parsed.Reveal() != "input" {
		t.Fatal("input decoding failed")
	}
	var zero secret.String
	if !zero.IsZero() {
		t.Fatal("zero value nonempty")
	}
}
