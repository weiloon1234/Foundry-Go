package pagination

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type paginationCallbackError struct {
	method, mode     string
	calls, formatted int
}

func (e *paginationCallbackError) Error() string {
	e.format("Error")
	return "private pagination error"
}
func (e *paginationCallbackError) Format(state fmt.State, _ rune) {
	e.format("Format")
	_, _ = state.Write([]byte("private pagination format"))
}
func (e *paginationCallbackError) format(method string) {
	e.formatted++
	if e.method == method {
		e.fail()
	}
}
func (e *paginationCallbackError) fail() {
	switch e.mode {
	case "panic":
		panic("private pagination callback")
	case "goexit":
		runtime.Goexit()
	}
}
func (e *paginationCallbackError) Is(target error) bool {
	if e.method != "Is" {
		return false
	}
	e.calls++
	e.fail()
	return e.mode == "match" && target == errBeyondMaximumPage
}
func (e *paginationCallbackError) Unwrap() error {
	if e.method != "Unwrap" {
		return nil
	}
	e.calls++
	e.fail()
	if e.mode == "cycle" && e.calls < 512 {
		return e
	}
	return nil
}

func TestPaginationMappingNeverFormatsApplicationFailures(t *testing.T) {
	for _, mapper := range []string{"page", "simple", "cursor"} {
		for _, method := range []string{"Error", "Format"} {
			for _, mode := range []string{"private", "panic", "goexit"} {
				t.Run(mapper+"/"+method+"/"+mode, func(t *testing.T) {
					cause := &paginationCallbackError{method: method, mode: mode}
					mapItem := func(int) (int, error) { return 0, cause }
					var err error
					switch mapper {
					case "page":
						var result orm.PageResult[int]
						result, err = MapPage(t.Context(), orm.PageResult[int]{Items: []int{1}, Number: 1, Size: 1, Total: 1, Pages: 1}, mapItem)
						if !reflect.DeepEqual(result, orm.PageResult[int]{}) {
							t.Fatal("partial page returned")
						}
					case "simple":
						var result orm.SimplePage[int]
						result, err = MapSimplePage(t.Context(), orm.SimplePage[int]{Items: []int{1}, Number: 1, Size: 1}, mapItem)
						if !reflect.DeepEqual(result, orm.SimplePage[int]{}) {
							t.Fatal("partial simple page returned")
						}
					case "cursor":
						var result CursorResult[int, int]
						result, err = MapCursorPage(t.Context(), orm.CursorPage[int]{Items: []int{1}, Size: 1}, mapItem)
						if result.defined || len(result.items) != 0 {
							t.Fatal("partial cursor page returned")
						}
					}
					var original *paginationCallbackError
					if !errors.Is(err, cause) || !errors.As(err, &original) || original != cause {
						t.Fatal("mapping cause identity lost")
					}
					if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") || cause.formatted != 0 {
						t.Fatal("mapping formatted application failure")
					}
				})
			}
		}
	}
}
func TestPaginationNavigationBoundsErrorInspection(t *testing.T) {
	for _, method := range []string{"Is", "Unwrap"} {
		for _, mode := range []string{"panic", "goexit", "cycle"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				cause := &paginationCallbackError{method: method, mode: mode}
				links, err := navigation(orm.PageRequest{Number: 1, Size: 1}, true, value.Optional[int64]{}, navigationPolicy{}, func(orm.PageRequest) (string, error) { return "", cause })
				if !errors.Is(err, cause) || !reflect.DeepEqual(links, Links{}) {
					t.Fatal("navigation failure was swallowed")
				}
				if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") || cause.formatted != 0 || cause.calls > 65 {
					t.Fatal("navigation inspected without bounds or formatted failure")
				}
			})
		}
	}
	for _, cause := range []error{errBeyondMaximumPage, &paginationCallbackError{method: "Is", mode: "match"}} {
		links, err := navigation(orm.PageRequest{Number: 1, Size: 1}, true, value.Optional[int64]{}, navigationPolicy{}, func(orm.PageRequest) (string, error) { return "", cause })
		if err != nil || !links.Next.IsNull() {
			t.Fatal("maximum-page omission changed")
		}
	}
}
