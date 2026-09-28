package httpmodels_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foundry.test/consumer/httpmodels"
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/retrievalqueries"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
)

type countedExecutor struct {
	database.Executor
	queries int
}

func (e *countedExecutor) Query(ctx context.Context, statement string, args ...any) (*database.Rows, error) {
	e.queries++
	return e.Executor.Query(ctx, statement, args...)
}

func request(t *testing.T, ctx context.Context, handler http.Handler, url string, status int) {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", url, nil).WithContext(ctx))
	if w.Code != status {
		t.Errorf("%s: %d %s", url, w.Code, w.Body.String())
	}
}

func TestPostgresModelBindingHydratesUUIDOnceAndPreservesScope(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		reader := &countedExecutor{Executor: tx}
		r, err := httpmodels.Router(reader, httpmodels.Presenter{})
		if err != nil {
			return err
		}
		ctx := t.Context()
		for _, test := range []struct {
			key           string
			status, calls int
		}{
			{users[0].ID.String(), 200, 1}, {users[1].ID.String(), 404, 1},
			{"018f47aa-7c89-7f10-bf8f-9df04f572d86", 404, 1}, {"invalid", 400, 0},
		} {
			before := reader.queries
			request(t, ctx, r, "/api/users/"+test.key, test.status)
			if reader.queries-before != test.calls {
				t.Error("lookup repeated or malformed key reached database")
			}
		}
		// Refresh model state on the next request; no request-global model cache.
		if _, err := models.QueryUsers().Update(ctx, tx, users[0].ID, models.UserDraft{}.SetStatus(models.StatusDisabled)); err != nil {
			return err
		}
		request(t, ctx, r, "/api/users/"+users[0].ID.String(), 404)
		orders, err := models.QueryOrders().Where(models.OrderFields().BuyerID.Eq(users[2].ID)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(orders) != 0 {
			return errors.New("unexpected seeded child")
		}
		order, err := models.QueryOrders().Where(models.OrderFields().BuyerID.Eq(users[0].ID)).RequireFirst(ctx, tx)
		if err != nil {
			return err
		}
		resolver := httpmodels.OrderWithinUser(tx)
		if got, err := resolver.Resolve(ctx, httpmodels.OrderPath{User: users[0].ID, Order: order.ID}); err != nil || got.BuyerID != users[0].ID {
			return errors.New("parent-scoped child did not hydrate")
		}
		if _, err := resolver.Resolve(ctx, httpmodels.OrderPath{User: users[2].ID, Order: order.ID}); !errors.Is(err, foundryhttp.NotFound) {
			return errors.New("child escaped its declared parent")
		}
		return nil
	})
}

func TestPostgresModelBindingNaturalKeysAndSoftDeleteVisibility(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		ctx := t.Context()
		for _, ddl := range []string{
			`CREATE TABLE countries(code text PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE write_records(code bigint PRIMARY KEY,name text NOT NULL,enabled boolean NOT NULL DEFAULT true,note text,amount numeric NOT NULL DEFAULT 0)`,
			`CREATE TABLE soft_groups(code text PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`,
		} {
			if _, err := tx.Exec(ctx, ddl); err != nil {
				return err
			}
		}
		if _, err := models.QueryCountries().Create(ctx, tx, models.CountryDraft{}.SetCode("a/b + %").SetName("country")); err != nil {
			return err
		}
		if _, err := models.QueryWriteRecords().Create(ctx, tx, models.WriteRecordDraft{}.SetCode(42).SetName("record")); err != nil {
			return err
		}
		countries := foundryhttp.DefineEndpoint(
			foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "countries.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
				foundryhttp.DefinePath("/countries/{country}", foundryhttp.Param("country", foundryhttp.StringPath[models.CountryCode](), func(p *httpmodels.CountryPath) *models.CountryCode { return &p.Country }))),
			foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
		countryRoute := modelbinding.Bind(countries, httpmodels.CountryByCode(tx)).Handle(func(_ context.Context, in modelbinding.Input[httpmodels.CountryPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.Country]) (foundryhttp.NoContent, error) {
			if in.Model.Code != "a/b + %" || in.Model.Name != "country" {
				return foundryhttp.NoContent{}, errors.New("natural key hydration failed")
			}
			return foundryhttp.NoContent{}, nil
		})
		type numberPath struct{ Code models.RecordKey }
		records := foundryhttp.DefineEndpoint(
			foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "records.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
				foundryhttp.DefinePath("/records/{record}", foundryhttp.Param("record", foundryhttp.IntegerPath[models.RecordKey](), func(p *numberPath) *models.RecordKey { return &p.Code }))),
			foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
		recordRoute := modelbinding.Bind(records, modelbinding.ByKey(tx, models.QueryWriteRecords(), func(p numberPath) models.RecordKey { return p.Code })).Handle(func(_ context.Context, in modelbinding.Input[numberPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.WriteRecord]) (foundryhttp.NoContent, error) {
			if in.Model.Code != 42 || in.Model.Name != "record" {
				return foundryhttp.NoContent{}, errors.New("numeric key hydration failed")
			}
			return foundryhttp.NoContent{}, nil
		})
		r, err := foundryhttp.NewRouter(countryRoute, recordRoute)
		if err != nil {
			return err
		}
		url, err := countries.URL(ctx, httpmodels.CountryPath{Country: "a/b + %"}, foundryhttp.NoQuery{})
		if err != nil {
			return err
		}
		request(t, ctx, r, url, 204)
		request(t, ctx, r, "/countries/missing", 404)
		request(t, ctx, r, "/records/42", 204)
		request(t, ctx, r, "/records/99", 404)
		request(t, ctx, r, "/records/no-number", 400)
		groups := softqueries.QuerySoftGroups()
		group, err := groups.Create(ctx, tx, softqueries.GroupDraft{}.SetCode("hidden").SetName("group"))
		if err != nil {
			return err
		}
		if _, err := groups.Delete(ctx, tx, group.Code); err != nil {
			return err
		}
		type groupPath struct{ Code softqueries.GroupCode }
		selectKey := func(p groupPath) softqueries.GroupCode { return p.Code }
		if _, err := modelbinding.ByKey(tx, groups, selectKey).Resolve(ctx, groupPath{group.Code}); !errors.Is(err, foundryhttp.NotFound) {
			return errors.New("default binding exposed deleted model")
		}
		got, err := modelbinding.ByKey(tx, groups.WithTrashed(), selectKey).Resolve(ctx, groupPath{group.Code})
		if err != nil || got.DeletedAt.IsNull() {
			return errors.New("explicit deleted visibility lost")
		}
		return nil
	})
}

func TestPostgresModelBindingRetainsRetrievedHooksAndStoredFields(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		ctx := t.Context()
		if _, err := tx.Exec(ctx, `CREATE TABLE retrieval_members(id uuid PRIMARY KEY,name text NOT NULL,nickname text,parent_id uuid)`); err != nil {
			return err
		}
		q := retrievalqueries.QueryRetrievalMembers()
		parent, err := q.Create(ctx, tx, retrievalqueries.MemberDraft{}.SetName(" PARENT "))
		if err != nil {
			return err
		}
		child, err := q.Create(ctx, tx, retrievalqueries.MemberDraft{}.SetName(" CHILD ").SetParentID(parent.ID))
		if err != nil {
			return err
		}
		type memberPath struct {
			ID model.ID[retrievalqueries.Member]
		}
		resolver := modelbinding.ByKey(tx, q.With(retrievalqueries.MemberRelations().Parent), func(p memberPath) model.ID[retrievalqueries.Member] { return p.ID })
		endpoint := foundryhttp.DefineEndpoint(
			foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "retrieval.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
				foundryhttp.DefinePath("/retrieval/{member}", foundryhttp.Param("member", foundryhttp.ModelIDPath[retrievalqueries.Member](), func(p *memberPath) *model.ID[retrievalqueries.Member] { return &p.ID }))),
			foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
		handled := 0
		r, err := foundryhttp.NewRouter(modelbinding.Bind(endpoint, resolver).Handle(func(_ context.Context, in modelbinding.Input[memberPath, foundryhttp.NoQuery, foundryhttp.NoBody, retrievalqueries.Member]) (foundryhttp.NoContent, error) {
			handled++
			name, err := in.Model.AccessName()
			if err != nil {
				return foundryhttp.NoContent{}, err
			}
			optional, set := in.Model.Parent.Get()
			loaded, present := optional.Get()
			if in.Model.Name != "child" || name != "CHILD" || !set || !present || loaded.ID != parent.ID {
				return foundryhttp.NoContent{}, errors.New("stored field, getter or eager relation lost")
			}
			return foundryhttp.NoContent{}, nil
		}))
		if err != nil {
			return err
		}
		url := "/retrieval/" + child.ID.String()
		trace := &retrievalqueries.Trace{}
		request(t, retrievalqueries.WithTrace(ctx, trace), r, url, 204)
		if handled != 1 || len(trace.Calls) != 2 || strings.Join(trace.Names, ",") != "child,parent" {
			return errors.New("retrieval hooks lost or duplicated")
		}
		veto := &retrievalqueries.Trace{FailAt: "local"}
		request(t, retrievalqueries.WithTrace(ctx, veto), r, url, 500)
		if handled != 1 || len(veto.Calls) != 1 {
			return errors.New("retrieval veto did not stop model handler")
		}
		return nil
	})
}
