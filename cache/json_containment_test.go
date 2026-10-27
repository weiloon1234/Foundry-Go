package cache_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"strings"
	"testing"
)

type brokenJSON struct {
	Cause error
	Mode  string
}

func (v brokenJSON) MarshalJSON() ([]byte, error) {
	if v.Cause != nil {
		return nil, v.Cause
	}
	return []byte(`"` + v.Mode + `"`), nil
}
func (v *brokenJSON) UnmarshalJSON(data []byte) error {
	return &errorgraphtest.Error{Mode: strings.Trim(string(data), `"`)}
}
func TestCacheJSONPreservesOwnedDiagnosticAndContainsMethods(t *testing.T) {
	s, _, _ := store(t, nil)
	dates, err := cache.Define("dates", cache.StringKeys[string](), cache.JSON[temporal.Date]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = dates.Put(t.Context(), "empty", temporal.Date{}, cache.Forever()); err == nil || !strings.Contains(err.Error(), "date is absent") {
		t.Fatal("cache lost absent-date diagnostic", err)
	}
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		family, err := cache.Define(cache.Name("broken_"+mode), cache.StringKeys[string](), cache.JSON[brokenJSON]()).Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		err = family.Put(t.Context(), "key", brokenJSON{Cause: cause}, cache.Forever())
		if err == nil || !errors.Is(err, cause) || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
			t.Fatal("cache JSON error escaped containment")
		}
		if err := family.Put(t.Context(), "decode", brokenJSON{Mode: mode}, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		_, _, err = family.Get(t.Context(), "decode")
		var decoded *errorgraphtest.Error
		if err == nil || !errors.As(err, &decoded) || decoded.Formatted.Load() != 0 || decoded.Visits.Load() > 512 {
			t.Fatal("cache decoder escaped containment")
		}

	}
}
