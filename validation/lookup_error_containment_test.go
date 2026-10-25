package validation_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type lookupContainmentError struct {
	method, mode string
	calls        int
}

func (e *lookupContainmentError) invoke(method string) {
	e.calls++
	if method != e.method {
		return
	}
	switch e.mode {
	case "panic":
		panic("private lookup callback")
	case "goexit":
		runtime.Goexit()
	}
}
func (e *lookupContainmentError) Error() string { e.invoke("Error"); return "private lookup failure" }
func (e *lookupContainmentError) Format(s fmt.State, _ rune) {
	e.invoke("Format")
	_, _ = s.Write([]byte("private lookup format"))
}
func (e *lookupContainmentError) Is(error) bool { e.invoke("Is"); return e.mode == "match" }
func (e *lookupContainmentError) As(any) bool   { e.invoke("As"); return false }
func (e *lookupContainmentError) Unwrap() error {
	e.invoke("Unwrap")
	if e.mode == "cycle" && e.calls < 512 {
		return e
	}
	return nil
}

type hostileLookup struct{ declaration, result error }

func (l hostileLookup) Validate() error                                  { return l.declaration }
func (l hostileLookup) Exists(context.Context, string) (bool, error)     { return false, l.result }
func (l hostileLookup) AllExist(context.Context, []string) (bool, error) { return false, l.result }
func (l hostileLookup) AnyExist(context.Context, []string) (bool, error) { return false, l.result }

type lookupRecord struct{ Code string }

func TestLookupDeclarationsDoNotFormatApplicationErrors(t *testing.T) {
	field := validation.DefineField("code", func(item lookupRecord) string { return item.Code })
	declarations := map[string]func(hostileLookup) error{
		"exists":      func(l hostileLookup) error { return validation.Exists[string](l).Validate() },
		"unique":      func(l hostileLookup) error { return validation.Unique[string](l).Validate() },
		"exists-all":  func(l hostileLookup) error { return validation.ExistsAll[[]string](l).Validate() },
		"unique-all":  func(l hostileLookup) error { return validation.UniqueAll[[]string](l).Validate() },
		"exists-each": func(l hostileLookup) error { return validation.ExistsEach[[]lookupRecord](field, l).Validate() },
		"unique-each": func(l hostileLookup) error { return validation.UniqueEach[[]lookupRecord](field, l).Validate() },
	}
	for name, declare := range declarations {
		for _, method := range []string{"Error", "Format"} {
			for _, mode := range []string{"private", "panic", "goexit"} {
				t.Run(name+"/"+method+"/"+mode, func(t *testing.T) {
					cause := &lookupContainmentError{method: method, mode: mode}
					err := declare(hostileLookup{declaration: cause})
					var original *lookupContainmentError
					if !errors.Is(err, cause) || !errors.As(err, &original) || original != cause || !errors.Is(err, fault.Invalid) {
						t.Fatal("declaration failure identity or classification lost")
					}
					if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") || cause.calls != 0 {
						t.Fatal("declaration formatted private error")
					}
				})
			}
		}
	}
}
func TestBatchLookupFailuresDoNotInspectOrSuppressApplicationErrors(t *testing.T) {
	field := validation.DefineField("code", func(item lookupRecord) string { return item.Code })
	checks := map[string]func(hostileLookup) error{
		"exists-all": func(l hostileLookup) error {
			return validation.ExistsAll[[]string](l).Check(t.Context(), []string{"input"}, validation.DefaultLimits())
		},
		"unique-all": func(l hostileLookup) error {
			return validation.UniqueAll[[]string](l).Check(t.Context(), []string{"input"}, validation.DefaultLimits())
		},
		"exists-each": func(l hostileLookup) error {
			return validation.ExistsEach[[]lookupRecord](field, l).Check(t.Context(), []lookupRecord{{"input"}}, validation.DefaultLimits())
		},
		"unique-each": func(l hostileLookup) error {
			return validation.UniqueEach[[]lookupRecord](field, l).Check(t.Context(), []lookupRecord{{"input"}}, validation.DefaultLimits())
		},
	}
	for name, check := range checks {
		for _, method := range []string{"Error", "Format", "Is", "As", "Unwrap"} {
			for _, mode := range []string{"private", "panic", "goexit", "cycle", "match"} {
				t.Run(name+"/"+method+"/"+mode, func(t *testing.T) {
					cause := &lookupContainmentError{method: method, mode: mode}
					err := check(hostileLookup{result: cause})
					var original *lookupContainmentError
					if !errors.Is(err, fault.Internal) || !errors.Is(err, cause) || !errors.As(err, &original) || original != cause {
						t.Fatal("lookup failure became success or lost identity")
					}
					if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") || cause.calls != 0 {
						t.Fatal("lookup inspected or formatted application failure")
					}
				})
			}
		}
	}
}
