package auth

import (
	"fmt"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// CredentialName identifies a declared input source, such as api.bearer.
// It is not a guard name, permission, cookie name or credential value.
type CredentialName string

const MaxCredentials = 32
const MaxCredentialBytes = 16 << 10
const MaxCredentialsBytes = 64 << 10

// Credential is a transport adapter's input. Omit missing sources entirely;
// a present empty secret is invalid and must not become anonymous access.
type Credential struct {
	Name   CredentialName
	Secret secret.String
}

// Credentials is an immutable bounded input snapshot for one authorization
// scope. Routine formatting and JSON never expose its contents.
type Credentials struct {
	values map[CredentialName]secret.String
	bound  map[CredentialName]BoundCredential
}

func (Credentials) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("authentication credentials")) }

func NewCredentials(inputs ...Credential) (Credentials, error) {
	if len(inputs) > MaxCredentials {
		return Credentials{}, fault.New(fault.Invalid, "too many authentication credential sources")
	}
	values := make(map[CredentialName]secret.String, len(inputs))
	size := 0
	for _, input := range inputs {
		if !identifier.Semantic(string(input.Name)) {
			return Credentials{}, fault.New(fault.Invalid, "invalid authentication credential source")
		}
		if _, ok := values[input.Name]; ok {
			return Credentials{}, fault.New(fault.Duplicate, "authentication credential source is repeated")
		}
		text := input.Secret.Reveal()
		if len(text) == 0 || len(text) > MaxCredentialBytes || len(text) > MaxCredentialsBytes-size {
			return Credentials{}, Unauthenticated
		}
		size += len(text)
		values[input.Name] = secret.New(strings.Clone(text))
	}
	return Credentials{values: values}, nil
}

// Get returns an immutable secret for a declared source. Zero means absent;
// transport adapters cannot construct a present empty credential.
func (c Credentials) Get(name CredentialName) secret.String { return c.values[name] }
