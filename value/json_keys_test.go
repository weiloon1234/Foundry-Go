package value_test

import (
	"context"
	"errors"
	"math"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func keyLimits() value.JSONKeyLimits { return value.JSONKeyLimits{Bytes: 4096, Keys: 20} }

func TestJSONKeyChecksPreserveNativeWidthsAndSpelling(t *testing.T) {
	for _, keys := range [][]string{{"1", "-128", "127"}, {"0"}} {
		if err := value.CheckJSONKeys[int8](t.Context(), keys, keyLimits()); err != nil {
			t.Fatal(err)
		}
	}
	for _, keys := range [][]string{{"01"}, {"1", "01"}, {"-0"}, {"+1"}, {"128"}, {"1", "1"}} {
		if err := value.CheckJSONKeys[int8](t.Context(), keys, keyLimits()); err == nil {
			t.Fatal("invalid integer keys accepted", keys)
		}
	}
	if err := value.CheckJSONKeys[uint64](t.Context(), []string{"18446744073709551615"}, keyLimits()); err != nil {
		t.Fatal("exact unsigned key changed", err)
	}
	if err := value.CheckJSONKeys[string](t.Context(), []string{"", "a/b", "\x00", "<é>"}, keyLimits()); err != nil {
		t.Fatal("native text key changed", err)
	}
	if err := value.CheckJSONKeys[string](t.Context(), []string{"ab"}, value.JSONKeyLimits{Bytes: 1, Keys: 1}); err == nil {
		t.Fatal("key byte limit ignored")
	}
	if err := value.CheckJSONKeys[string](t.Context(), []string{"a", "b"}, value.JSONKeyLimits{Bytes: 10, Keys: 1}); err == nil {
		t.Fatal("key count limit ignored")
	}
	if value.SupportsJSONKeys[bool]() {
		t.Fatal("plain boolean JSON keys accepted")
	}
}

type foldedObjectKey string

func (k *foldedObjectKey) UnmarshalText(data []byte) error {
	*k = foldedObjectKey(strings.ToLower(string(data)))
	return nil
}

type jsonOnlyObjectKey string

func (jsonOnlyObjectKey) MarshalJSON() ([]byte, error) {
	panic("JSON methods must be ignored for keys")
}
func (*jsonOnlyObjectKey) UnmarshalJSON([]byte) error { panic("JSON methods must be ignored for keys") }

type numericObjectKey struct{ Number int }

func (k numericObjectKey) MarshalText() ([]byte, error) { return []byte(strconv.Itoa(k.Number)), nil }
func (k *numericObjectKey) UnmarshalText(data []byte) error {
	n, err := strconv.Atoi(string(data))
	k.Number = n
	return err
}

type nonreflexiveObjectKey float64

func (nonreflexiveObjectKey) MarshalText() ([]byte, error) { return []byte("nan"), nil }
func (k *nonreflexiveObjectKey) UnmarshalText([]byte) error {
	*k = nonreflexiveObjectKey(math.NaN())
	return nil
}

type expansiveObjectKey struct{}

func (expansiveObjectKey) MarshalText() ([]byte, error) {
	return []byte(strings.Repeat("x", 1024)), nil
}
func (*expansiveObjectKey) UnmarshalText([]byte) error { return nil }

func TestJSONKeyChecksUseNativeMethodPrecedenceAndCanonicality(t *testing.T) {
	if err := value.CheckJSONKeys[jsonOnlyObjectKey](t.Context(), []string{"native"}, keyLimits()); err != nil {
		t.Fatal("JSON method affected key", err)
	}
	if err := value.CheckJSONKeys[foldedObjectKey](t.Context(), []string{"lower"}, keyLimits()); err != nil {
		t.Fatal(err)
	}
	if err := value.CheckJSONKeys[foldedObjectKey](t.Context(), []string{"LOWER", "lower"}, keyLimits()); err == nil {
		t.Fatal("decoded identities collapsed")
	}
	if err := value.CheckJSONKeys[numericObjectKey](t.Context(), []string{"42"}, keyLimits()); err != nil {
		t.Fatal("native struct key rejected", err)
	}
	if err := value.CheckJSONKeys[numericObjectKey](t.Context(), []string{"042"}, keyLimits()); err == nil {
		t.Fatal("custom spelling accepted")
	}
	if err := value.CheckJSONKeys[nonreflexiveObjectKey](t.Context(), []string{"nan"}, keyLimits()); err == nil {
		t.Fatal("nonreflexive key accepted")
	}
	if err := value.CheckJSONKeys[expansiveObjectKey](t.Context(), []string{"x"}, keyLimits()); err == nil {
		t.Fatal("unbounded/noncanonical formatter accepted")
	}
}

var keyStarted, keyRelease chan struct{}

type controlledObjectKey string

func (k *controlledObjectKey) UnmarshalText(data []byte) error {
	switch string(data) {
	case "panic":
		panic("private key detail")
	case "goexit":
		runtime.Goexit()
	case "wait":
		close(keyStarted)
		<-keyRelease
	}
	*k = controlledObjectKey(data)
	return nil
}

func TestJSONKeyCallbackOwnershipAndFailureClassification(t *testing.T) {
	for _, name := range []string{"panic", "goexit"} {
		err := value.CheckJSONKeys[controlledObjectKey](t.Context(), []string{name}, keyLimits())
		if !errors.Is(err, fault.Internal) || strings.Contains(err.Error(), "private") {
			t.Fatal("key callback failure escaped", err)
		}
	}
	keyStarted, keyRelease = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- value.CheckJSONKeys[controlledObjectKey](ctx, []string{"wait"}, keyLimits()) }()
	<-keyStarted
	cancel()
	select {
	case err := <-result:
		t.Fatal("active codec abandoned", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(keyRelease)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled key check returned", err)
	}
}

type appendedObjectKey struct{ Number int }

func (k appendedObjectKey) AppendText(dst []byte) ([]byte, error) {
	return strconv.AppendInt(dst, int64(k.Number), 10), nil
}
func (k *appendedObjectKey) UnmarshalText(data []byte) error {
	n, err := strconv.Atoi(string(data))
	k.Number = n
	return err
}

type encodedIntegerKey int

func (k encodedIntegerKey) MarshalText() ([]byte, error) {
	return []byte("n" + strconv.Itoa(int(k))), nil
}

func TestJSONKeyNativeTextAppenderAndOneWayEncoder(t *testing.T) {
	if err := value.CheckJSONKeys[appendedObjectKey](t.Context(), []string{"42"}, keyLimits()); err != nil {
		t.Fatal("native text appender key", err)
	}
	// Legacy decoding uses the integer itself, while encoding uses MarshalText.
	// Neither possible input spelling is a canonical native round trip.
	for _, key := range []string{"42", "n42"} {
		if err := value.CheckJSONKeys[encodedIntegerKey](t.Context(), []string{key}, keyLimits()); err == nil {
			t.Fatal("one-way integer encoder accepted", key)
		}
	}
}
