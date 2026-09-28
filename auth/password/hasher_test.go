package password

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func testHasher(t *testing.T) *Hasher {
	t.Helper()
	config := DefaultConfig()
	config.Parameters = Parameters{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}
	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func plaintext(t *testing.T, text string) Plaintext {
	t.Helper()
	value, err := NewPlaintext(secret.New(text))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestHashCheckAndExactPasswordBytes(t *testing.T) {
	h := testHasher(t)
	input := plaintext(t, "  MiXeD-é-password  ")
	first, err := h.Hash(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Hash(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Encoded().Reveal() == second.Encoded().Reveal() {
		t.Fatal("reused password salt")
	}
	parsed, err := ParseHash(first.Encoded())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct {
		text  string
		match bool
	}{{"  MiXeD-é-password  ", true}, {"MiXeD-é-password", false}, {"  mixed-é-password  ", false}, {"wrong", false}} {
		match, err := h.Check(t.Context(), plaintext(t, candidate.text), parsed)
		if err != nil || match != candidate.match {
			t.Fatal("password was normalized, truncated or mismatched", err)
		}
	}
	if needs, err := h.NeedsRehash(parsed); err != nil || needs {
		t.Fatal("current hash needs rehash", err)
	}
	changed := DefaultConfig()
	changed.Parameters = h.config.Parameters
	changed.Parameters.Iterations++
	next, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if needs, err := next.NeedsRehash(parsed); err != nil || !needs {
		t.Fatal("changed policy missed rehash", err)
	}
	for _, v := range []any{input, first, first.Encoded()} {
		encoded, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		output := string(encoded) + fmt.Sprintf("%v %#v", v, v)
		if strings.Contains(output, "MiXeD-é-password") || strings.Contains(output, first.Encoded().Reveal()) {
			t.Fatal("sensitive value disclosed")
		}
	}
}
func TestPasswordInputJSONAndBounds(t *testing.T) {
	for _, raw := range []string{"", strings.Repeat("a", MaxPasswordBytes+1)} {
		if _, err := NewPlaintext(secret.New(raw)); !errors.Is(err, fault.Invalid) {
			t.Fatal("password byte bound", err)
		}
	}
	if _, err := NewPlaintext(secret.New(strings.Repeat("a", MaxPasswordBytes))); err != nil {
		t.Fatal(err)
	}
	input := plaintext(t, "old")
	for _, raw := range []string{`null`, `42`, `{}`, `""`} {
		if err := json.Unmarshal([]byte(raw), &input); err == nil || input.Validate() == nil {
			t.Fatal("invalid input retained password")
		}
	}
	if err := json.Unmarshal([]byte(`"  exact password  "`), &input); err != nil || input.value.Reveal() != "  exact password  " {
		t.Fatal("input normalized", err)
	}
	if err := input.JSONContract().Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestPHCParserRejectsMalformedAndUnboundedParameters(t *testing.T) {
	salt := "MTIzNDU2Nzg5MDEyMzQ1Ng"
	key := "MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI"
	valid := "$argon2id$v=19$m=65536,t=3,p=4$" + salt + "$" + key
	if _, err := ParseHash(secret.New(valid)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"", strings.Repeat("a", MaxEncodedBytes+1), strings.Replace(valid, "argon2id", "argon2i", 1),
		strings.Replace(valid, "v=19", "v=16", 1), strings.Replace(valid, "m=65536", "m=4294967295", 1),
		strings.Replace(valid, "m=65536", "m=065536", 1), strings.Replace(valid, "m=65536", "m=+65536", 1),
		strings.Replace(valid, "t=3", "t=0", 1), strings.Replace(valid, "p=4", "p=0", 1), strings.Replace(valid, "p=4", "p=256", 1),
		strings.Replace(valid, "t=3,p=4", "p=4,t=3", 1), valid + "$", strings.Replace(valid, salt, salt+"=", 1),
		strings.Replace(valid, salt, salt+"\n", 1), strings.Replace(valid, salt, "YQ", 1), strings.Replace(valid, key, "YQ", 1),
	} {
		value, err := ParseHash(secret.New(bad))
		if err == nil || !value.Encoded().IsZero() {
			t.Fatal("malformed PHC accepted")
		}
		if bad != "" && strings.Contains(fmt.Sprint(err), bad) {
			t.Fatal("parse error disclosed hash")
		}
	}
}
func TestStoredCostsCannotExceedVerificationCeiling(t *testing.T) {
	h := testHasher(t)
	// Valid PHC syntax at a hard-supported cost, above this instance's ceiling.
	// This must reject before invoking the memory-intensive KDF.
	encoded := encodedHash(Parameters{MemoryKiB: MaxMemoryKiB, Iterations: 3, Parallelism: 4}, make([]byte, SaltBytes), make([]byte, KeyBytes))
	if _, err := ParseHash(encoded.Encoded()); err != nil {
		t.Fatal(err)
	}
	match, err := h.Check(t.Context(), plaintext(t, "test"), encoded)
	if !errors.Is(err, fault.Invalid) || match {
		t.Fatal("accepted excessive verification cost", err)
	}
}
func TestCanceledAndOverloadedHasherReturnsNoHash(t *testing.T) {
	h := testHasher(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := h.Hash(ctx, plaintext(t, "test")); !errors.Is(err, context.Canceled) || !result.Encoded().IsZero() {
		t.Fatal("canceled hash published", err)
	}
	config := DefaultConfig()
	config.Parameters = h.config.Parameters
	config.MaxConcurrent = 1
	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- h.gate.Execute(t.Context(), func(context.Context) error { close(entered); <-release; return nil })
	}()
	<-entered
	hash, err := h.Hash(t.Context(), plaintext(t, "test"))
	close(release)
	if !errors.Is(err, fault.Conflict) || !hash.Encoded().IsZero() {
		t.Error("overloaded hasher admitted work", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestInvalidHashPolicyRejectedAtConstruction(t *testing.T) {
	for _, alter := range []func(*Config){
		func(c *Config) { c.Parameters.MemoryKiB = 1024 }, func(c *Config) { c.Parameters.Iterations = 1 },
		func(c *Config) { c.Parameters.Parallelism = 0 }, func(c *Config) { c.VerifyLimit.MemoryKiB = c.Parameters.MemoryKiB - 1 },
		func(c *Config) { c.MaxConcurrent = 0 }, func(c *Config) { c.Timeout = -time.Second },
	} {
		c := DefaultConfig()
		alter(&c)
		if _, err := New(c); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid hash policy accepted", err)
		}
	}
}
func FuzzPHCParser(f *testing.F) {
	f.Add("")
	f.Add("$argon2id$v=19$m=65536,t=3,p=4$MTIzNDU2Nzg5MDEyMzQ1Ng$MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI")
	f.Fuzz(func(t *testing.T, raw string) {
		hash, err := ParseHash(secret.New(raw))
		if err == nil {
			if hash.Encoded().Reveal() != raw || hash.Validate() != nil {
				t.Fatal("PHC did not round trip")
			}
		} else if !hash.Encoded().IsZero() {
			t.Fatal("partial hash escaped")
		}
	})
}
