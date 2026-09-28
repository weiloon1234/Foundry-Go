package countries

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
)

func TestPinnedReferenceShapeAndTimezones(t *testing.T) {
	for _, file := range []struct {
		data []byte
		hash string
	}{{builtinSeed, "8839a7f4c6a0406dc772be081cf2e442a28170e280befe0e454411e001b06d58"}, {[]byte(builtinZones), "586b4207e6c76722de82adcda6bf49d761f668517f45a673f64da83b333eecc4"}} {
		sum := sha256.Sum256(file.data)
		if hex.EncodeToString(sum[:]) != file.hash {
			t.Fatal("reference input changed without a reviewed dataset version")
		}
	}
	rows, err := loadReference()
	if err != nil || len(rows) != BuiltinCount {
		t.Fatal("country data", len(rows), err)
	}
	byCode := map[Code]reference{}
	for i, row := range rows {
		byCode[row.ISO2] = row
		if i > 0 && rows[i-1].ISO2 >= row.ISO2 {
			t.Fatal("reference order or uniqueness")
		}
	}
	malaysia := byCode["MY"]
	if malaysia.ISO3 != "MYS" || !slices.Equal(malaysia.Timezones, []string{"Asia/Kuala_Lumpur", "Asia/Kuching"}) || malaysia.Currencies[0].Code != "MYR" {
		t.Fatal("country reference parity")
	}
	if !slices.Equal(byCode["XK"].Timezones, []string{"Europe/Belgrade"}) || len(byCode["BV"].Timezones) != 0 || len(byCode["HM"].Timezones) != 0 {
		t.Fatal("timezone exceptions")
	}
	rows[0].Currencies[0].Code = "BAD"
	fresh, err := loadReference()
	if err != nil || fresh[0].Currencies[0].Code == "BAD" {
		t.Fatal("reference loader retained mutable output", err)
	}
}
func TestCountryCodeNormalization(t *testing.T) {
	for _, text := range []string{"my", " MY ", "MY"} {
		code, err := ParseCode(text)
		if err != nil || code != "MY" {
			t.Fatal(code, err)
		}
	}
	for _, text := range []string{"", "M", "MYS", "M1", "M\x00", "マイ"} {
		if _, err := ParseCode(text); err == nil {
			t.Fatal("invalid country code", text)
		}
	}
	if Code("my").Validate() == nil {
		t.Fatal("noncanonical typed country code")
	}
}
