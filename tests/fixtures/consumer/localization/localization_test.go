package localization_test

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestGeneratedMessagesShareTypedParametersAndExactWireContracts(t *testing.T) {
	catalog, err := localization.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result, err := localization.Welcome(t.Context(), catalog, "ms", "{{name}}")
	if err != nil || result.Text != "Helo {{name}}" {
		t.Fatal(result, err)
	}
	number, err := decimal.Parse("9007199254740993.125")
	if err != nil {
		t.Fatal(err)
	}
	result, err = localization.CartArgsMessage().Format(t.Context(), catalog, "en", localization.CartArgs{Name: "Ada", Count: number})
	if err != nil || result.Text != "Ada has 9007199254740993.125 items" {
		t.Fatal(result, err)
	}
	result, err = localization.CartArgsMessage().Format(t.Context(), catalog, "ar", localization.CartArgs{Count: decimal.FromInt64(2)})
	if err != nil || result.Text != "two 2" {
		t.Fatal(result, err)
	}
	result, err = localization.PositionArgsMessage().Format(t.Context(), catalog, "ms", localization.PositionArgs{Position: 21})
	if err != nil || result.Text != "21st" || !result.Fallback {
		t.Fatal(result, err)
	}
	result, err = localization.LiteralArgsMessage().Format(t.Context(), catalog, "en", localization.LiteralArgs{Text: `"{{text}}"`, Enabled: true})
	if err != nil || result.Text != `"{{text}}" true` {
		t.Fatal(result, err)
	}
	description, err := localization.CartArgsMessage().Description()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := localization.CartArgsJSON().Description()
	if err != nil {
		t.Fatal(err)
	}
	left, _ := json.Marshal(description.Arguments)
	right, _ := json.Marshal(schema)
	if string(left) != string(right) {
		t.Fatal("message duplicated DTO contract")
	}
	if description.Message.Plural != "count" || description.Message.Kind != i18n.Cardinal || !reflect.DeepEqual(description.Message.Parameters, []i18n.Parameter{{Name: "count", Kind: i18n.NumberParameter}, {Name: "name", Kind: i18n.TextParameter}}) {
		t.Fatal(description.Message)
	}
	definitions := catalog.Definitions()
	for i := range definitions {
		if definitions[i].Key == localization.WelcomeArgsMessage().Key() {
			definitions[i].Parameters[0].Kind = i18n.NumberParameter
		}
	}
	set, _ := catalog.Snapshot(t.Context())
	mismatched, err := i18n.NewCatalog(t.Context(), set, i18n.CatalogOptions{}, definitions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := localization.WelcomeArgsMessage().Format(t.Context(), mismatched, "en", localization.WelcomeArgs{Name: "Ada"}); err == nil {
		t.Fatal("same key bypassed parameter signature")
	}
}

func TestGeneratedLabelsComposeWithValidationEnumsAndPermissions(t *testing.T) {
	catalog, err := localization.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	field := localization.WelcomeArgsValidationFields().Name.WithLabel("Name").WithLabelKey(localization.NameLabelMessage().Key())
	err = field.Rules(validation.NonBlank[string]()).Check(t.Context(), localization.WelcomeArgs{}, validation.DefaultLimits())
	var issues *validation.Errors
	if !errors.As(err, &issues) {
		t.Fatal(err)
	}
	localized, err := issues.LocalizeLabels(t.Context(), catalog, "ms")
	if err != nil || localized.Issues()[0].Label != "Nama" || localized.Issues()[0].Path != "/name" {
		t.Fatal(localized, err)
	}
	descriptor := localization.Ready.EnumDescriptor()
	label, err := descriptor.Label(t.Context(), catalog, "ms", localization.Ready)
	if err != nil || label != "Sedia" {
		t.Fatal(label, err)
	}
	wire, err := descriptor.Definition()
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.Cases) != len(localization.StatusValues()) || wire.Cases[1].LabelKey != "enum.localization.status.ready" || string(wire.Cases[1].Value) != `"ready"` {
		t.Fatal(wire)
	}
	permission := auth.DefinePermission("localization.manage", func(context.Context, localization.WelcomeArgs) (bool, error) { return true, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), permission.Registration())
	if err != nil {
		t.Fatal(err)
	}
	labeled := permission.WithLabelKey(localization.ManageLabelMessage().Key())
	if err := labeled.ValidateIn(registry); err != nil {
		t.Fatal(err)
	}
	info, err := labeled.Description()
	if err != nil || info.LabelKey != "permissions.manage" {
		t.Fatal(info, err)
	}
}

func TestRequestLocaleMiddlewareKeepsConcurrentRequestsIndependent(t *testing.T) {
	catalog, err := localization.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := foundryhttp.ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		locale, ok := i18n.RequestLocale(r.Context())
		if !ok {
			t.Error("missing request locale")
			return
		}
		result, err := localization.Welcome(r.Context(), catalog, locale, "Ada")
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write([]byte(result.Text))
	}), foundryhttp.Locale(catalog))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			language, want := "en", "Hello Ada"
			if i%2 == 1 {
				language, want = "ms-MY,en;q=0.1", "Helo Ada"
			}
			r := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
			r.Header.Set("Accept-Language", language)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 200 || strings.TrimSpace(w.Body.String()) != want {
				t.Error(w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	ctx, err := i18n.WithLocale(t.Context(), catalog, "ms")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequestWithContext(ctx, stdhttp.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Body.String() != "Helo Ada" {
		t.Fatal("explicit locale lost priority")
	}
}
