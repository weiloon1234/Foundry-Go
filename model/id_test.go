package model_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

type user struct{ Values []string }

func TestUUIDv7LayoutOwnershipAndSerialization(t *testing.T) {
	now := time.UnixMilli(1645557742000)
	id, err := model.NewIDAt[user](now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id.String(), "017f22e2-79b0-7") {
		t.Fatalf("UUIDv7 timestamp/version mismatch: %s", id)
	}
	bytes := id.Bytes()
	if bytes[8]&0xc0 != 0x80 {
		t.Fatal("invalid variant")
	}
	if model.IDFromBytes[user](bytes) != id {
		t.Fatal("binary round trip failed")
	}
	seen := map[model.ID[user]]bool{id: true}
	for range 1000 {
		next, err := model.NewIDAt[user](now)
		if err != nil {
			t.Fatal(err)
		}
		if seen[next] {
			t.Fatal("duplicate UUID")
		}
		seen[next] = true
	}
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	var decoded model.ID[user]
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != id {
		t.Fatalf("JSON round trip failed: %v", err)
	}
	upper, err := model.ParseID[user](strings.ToUpper(id.String()))
	if err != nil || upper != id {
		t.Fatal("uppercase input failed")
	}
	var zero model.ID[user]
	if !zero.IsZero() {
		t.Fatal("zero ID not nil")
	}
	parsed, err := model.ParseID[user](zero.String())
	if err != nil || parsed != zero {
		t.Fatal("nil UUID failed")
	}
}

func TestIDRejectsMalformedInputAndUnsupportedGenerationTime(t *testing.T) {
	original, err := model.NewID[user]()
	if err != nil {
		t.Fatal(err)
	}
	decoded := original
	if err := json.Unmarshal([]byte("null"), &decoded); !errors.Is(err, fault.Invalid) || decoded != original {
		t.Fatal("JSON null accepted or mutated identity")
	}
	for _, input := range []string{"", "017f22e279b07cc398c4dc0c0c07398f", " 017f22e2-79b0-7cc3-98c4-dc0c0c07398f", "017f22e2-79b0-7cc3-98c4-dc0c0c07398g"} {
		if _, err := model.ParseID[user](input); !errors.Is(err, fault.Invalid) {
			t.Fatalf("accepted %q", input)
		}
	}
	for _, now := range []time.Time{time.UnixMilli(-1), time.UnixMilli(1 << 48), time.Date(1000000000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if _, err := model.NewIDAt[user](now); !errors.Is(err, fault.Invalid) {
			t.Fatal("out-of-range timestamp accepted")
		}
	}
}

func FuzzIDRoundTrip(f *testing.F) {
	f.Add("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	f.Fuzz(func(t *testing.T, input string) {
		id, err := model.ParseID[user](input)
		if err != nil {
			return
		}
		other, err := model.ParseID[user](id.String())
		if err != nil || id != other {
			t.Fatal("round trip failed")
		}
	})
}
