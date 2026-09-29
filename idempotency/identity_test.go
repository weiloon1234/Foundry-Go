package idempotency

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIdentityBoundsAndPrivateFormatting(t *testing.T) {
	key, err := ParseKey("private-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewScope("tenant-a", "caller-a")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := NewScope("tenant", "a-caller-a")
	if first == second {
		t.Fatal("scope components collided")
	}
	for _, text := range []string{"", strings.Repeat("x", 15), strings.Repeat("x", 257), "private key-0123456789", "private\x00key-0123456789", "private/0123456789"} {
		if _, err := ParseKey(text); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	for _, text := range []string{"", strings.Repeat("x", 257), " actor", "actor\n", "\xff"} {
		if _, err := NewScope(text, "caller"); err == nil {
			t.Fatal("invalid trusted identity accepted")
		}
	}
	for _, v := range []any{key, first, Result[string]{value: "private-body", encoded: []byte("private-body")}, failure(Unavailable, fmt.Errorf("private cause"))} {
		for _, verb := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(verb, v), "private") {
				t.Fatal("private operation material formatted")
			}
		}
	}
	if digest("a", "bc", "d") == digest("a", "b", "cd") || digest("a", "b") == digest("ab", "") {
		t.Fatal("digest framing collision")
	}
}
func TestConfigBounds(t *testing.T) {
	base := DefaultConfig()
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Config){func(c *Config) { c.MaxActive = 0 }, func(c *Config) { c.MaxRetainedPerCaller = 0 }, func(c *Config) { c.MaxKeyBytes = 257 }, func(c *Config) { c.MaxInputBytes = 1 << 21 }, func(c *Config) { c.MaxResultBytes = 0 }, func(c *Config) { c.DuplicateWait = c.Timeout + time.Second }, func(c *Config) { c.Timeout = 0 }, func(c *Config) { c.Retention = 0 }, func(c *Config) { c.Schema = "a.b" }, func(c *Config) { c.PruneInterval = time.Millisecond }, func(c *Config) { c.PruneInterval = -time.Second }, func(c *Config) { c.PruneBatch = 0 }, func(c *Config) { c.PruneBatch = MaxPrune + 1 }, func(c *Config) { c.PruneInterval = 0; c.PruneBatch = -1 }} {
		c := base
		change(&c)
		if c.Validate() == nil {
			t.Fatal("invalid bounds accepted")
		}
	}
}
func TestPruningCanBeDisabledExplicitly(t *testing.T) {
	c := DefaultConfig()
	if c.PruneInterval <= 0 || c.PruneBatch < 1 || c.PruneBatch > MaxPrune {
		t.Fatal("default pruning is not enabled and bounded", c.PruneInterval, c.PruneBatch)
	}
	c.PruneInterval = 0
	if err := c.Validate(); err != nil {
		t.Fatal("disabled pruning rejected", err)
	}
	c.PruneBatch = 0
	if err := c.Validate(); err != nil {
		t.Fatal("disabled pruning requires a batch", err)
	}
}
func FuzzKey(f *testing.F) {
	for _, s := range []string{"", "1234567890123456", "a.b:c_d-0123456789", "秘密-secret-key-123456789"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		key, err := ParseKey(s)
		if err != nil {
			return
		}
		again, err := ParseKey(key.text)
		if err != nil || again != key {
			t.Fatal("key identity changed")
		}
		if len(key.text) < MinKeyBytes || len(key.text) > MaxKeyBytes {
			t.Fatal("key escaped bounds")
		}
	})
}
