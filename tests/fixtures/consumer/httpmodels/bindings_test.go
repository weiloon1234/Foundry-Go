package httpmodels_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpmodels"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestGeneratedModelBindingKeepsStoredModelOutOfResponse(t *testing.T) {
	id, err := model.ParseID[models.User]("018f47aa-7c89-7f10-bf8f-9df04f572d86")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	resolver := modelbinding.Define(func(_ context.Context, p httpkernel.UserPath) (value.Optional[models.User], error) {
		calls++
		if p.User != id {
			t.Fatal("path ID changed")
		}
		return value.Set(models.User{ID: id, Email: "member@example.test", Status: models.StatusActive, Age: 37, Scratch: []string{"private"}}), nil
	})
	bound := modelbinding.Bind(httpmodels.Show, resolver)
	r, err := foundryhttp.NewRouter(bound.Handle(httpmodels.Presenter{}.Show))
	if err != nil {
		t.Fatal(err)
	}
	location, err := httpmodels.Show.URL(t.Context(), httpkernel.UserPath{User: id}, foundryhttp.NoQuery{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", location, nil))
	if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), id.String()) || !strings.Contains(w.Body.String(), "member@example.test") || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "age") {
		t.Fatal(w.Code, w.Body.String(), calls)
	}
	expected, err := httpmodels.Show.Description()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Endpoints(); len(got) != 1 || got[0].Response.Schema.Root != expected.Response.Schema.Root || len(got[0].Path) != 1 {
		t.Fatal("transport schema changed")
	}
}

type failingExecutor struct {
	calls     int
	statement string
	arguments []any
	cause     error
}

func (e *failingExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	panic("read binding wrote data")
}
func (e *failingExecutor) Query(_ context.Context, statement string, args ...any) (*database.Rows, error) {
	e.calls++
	e.statement = statement
	e.arguments = append([]any(nil), args...)
	return nil, e.cause
}

func TestGeneratedKeyBindingsUseScopedParameterizedQueries(t *testing.T) {
	cause := errors.New("private database failure")
	db := &failingExecutor{cause: cause}
	id, err := model.ParseID[models.User]("018f47aa-7c89-7f10-bf8f-9df04f572d86")
	if err != nil {
		t.Fatal(err)
	}
	resolver := httpmodels.UserByID(db)
	if err := resolver.Validate(); err != nil || db.calls != 0 {
		t.Fatal("query ran at assembly", err)
	}
	if _, err := resolver.Resolve(t.Context(), httpkernel.UserPath{User: id}); !errors.Is(err, cause) {
		t.Fatal("query failure changed", err)
	}
	if db.calls != 1 || !strings.Contains(db.statement, `"status"`) || !strings.Contains(db.statement, `"id"`) || strings.Contains(db.statement, id.String()) || len(db.arguments) < 2 {
		t.Fatal("query scope/parameters lost", db.statement)
	}
	db.calls = 0
	if _, err := httpmodels.CountryByCode(db).Resolve(t.Context(), httpmodels.CountryPath{Country: models.CountryCode("private-country")}); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if db.calls != 1 || !strings.Contains(db.statement, `"code"`) || strings.Contains(db.statement, "private-country") {
		t.Fatal("natural key was not bound", db.statement)
	}
	order, err := model.ParseID[models.Order]("018f47aa-7c89-7f10-bf8f-9df04f572d87")
	if err != nil {
		t.Fatal(err)
	}
	db.calls = 0
	if _, err := httpmodels.OrderWithinUser(db).Resolve(t.Context(), httpmodels.OrderPath{User: id, Order: order}); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if db.calls != 1 || !strings.Contains(db.statement, `"buyer_id"`) || len(db.arguments) < 2 {
		t.Fatal("child parent scope lost", db.statement)
	}
}
