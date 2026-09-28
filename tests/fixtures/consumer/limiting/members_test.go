package limiting_test

import (
	"foundry.test/consumer/limiting"
	"foundry.test/consumer/mutatorqueries"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type frozen struct{}

func (frozen) Now() time.Time { return time.Unix(1, 0) }
func TestModelOwnedQuotaAcrossDomainAndHTTP(t *testing.T) {
	b, err := memory.New(20, frozen{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	store, err := ratelimit.NewStore(b, ratelimit.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := limiting.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	decision, err := limiting.AdmitExport(t.Context(), limiter, id, 59)
	if err != nil || !decision.Allowed || decision.Remaining != 1 {
		t.Fatal(decision, err)
	}
	handler, err := foundryhttp.ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) }), limiting.MemberMiddleware(limiter, func(*stdhttp.Request) (model.ID[mutatorqueries.Member], error) { return id, nil }))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{204, 429} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if _, err := limiting.LoginMiddleware(store); err != nil {
		t.Fatal(err)
	}
}
