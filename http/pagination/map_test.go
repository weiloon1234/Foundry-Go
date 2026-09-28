package pagination_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type storedItem struct{ raw string }

func (s storedItem) Label() string { return "display:" + s.raw }
func TestPageMappingPreservesMetadataAndUsesExplicitGetter(t *testing.T) {
	source := query.Page[storedItem]{Items: []storedItem{{raw: "stored"}}, Number: 2, Size: 1, Total: 3, Pages: 3}
	mapped, err := pagination.MapPage(t.Context(), source, func(row storedItem) (Item, error) { return Item{Label: row.Label()}, nil })
	if err != nil || mapped.Items[0].Label != "display:stored" || mapped.Number != 2 || mapped.Total != 3 || mapped.Pages != 3 || source.Items[0].raw != "stored" {
		t.Fatalf("mapped=%+v err=%v", mapped, err)
	}
	simple, err := pagination.MapSimplePage(t.Context(), query.SimplePage[storedItem]{Items: source.Items, Number: 2, Size: 1, HasMore: true}, func(row storedItem) (Item, error) { return Item{Label: row.Label()}, nil })
	if err != nil || !simple.HasMore || simple.Number != 2 || simple.Items[0].Label != "display:stored" {
		t.Fatal("simple mapping changed page")
	}
}
func TestPageMappingFailuresReturnNoPartialResults(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("mapping failed")
			page, err := pagination.MapPage(ctx, query.Page[int]{Items: []int{1, 2}, Number: 1, Size: 2, Total: 2, Pages: 1}, func(n int) (Item, error) {
				if n == 2 {
					switch mode {
					case "error":
						return Item{}, cause
					case "panic":
						panic("private")
					case "goexit":
						runtime.Goexit()
					case "cancel":
						cancel()
					}
				}
				return Item{Label: "first"}, nil
			})
			if err == nil || !reflect.DeepEqual(page, query.Page[Item]{}) {
				t.Fatal("partial result escaped")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("mapping error identity lost")
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
		})
	}
	if _, err := pagination.MapPage[int, Item](nil, query.Page[int]{Number: 1, Size: 1}, func(int) (Item, error) { return Item{}, nil }); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := pagination.MapPage[int, Item](t.Context(), query.Page[int]{Number: 1, Size: 1}, nil); err == nil {
		t.Fatal("nil mapper accepted")
	}
}
