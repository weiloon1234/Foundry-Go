package http

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestTableOperationMetadataOwnsSnapshotsAndAdapters(t *testing.T) {
	binding := contract.TableOperationBinding{Table: "inventory", Kind: contract.TableQuery}
	endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "read", Method: POST, Access: Public}, StaticPath("/read")), EmptyQuery(), JSONBody(endpointPatchJSON()), JSONResponse(200, endpointReplyJSON()), TableOperation(binding))
	endpoint = ConfigureEndpoint(endpoint, Timeout(time.Minute), Within(DefineScope("/admin", "admin")))
	binding.Table = "mutated"
	info, err := InspectEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if info.TableOperation == nil || info.TableOperation.Table != "inventory" {
		t.Fatal(info.TableOperation)
	}
	info.TableOperation.Table = "inspection"
	signed := Signed(endpoint, urlTestSigner(t, testkit.NewClock(urlTestTime)))
	signedInfo, err := InspectSigned(signed)
	if err != nil {
		t.Fatal(err)
	}
	if signedInfo.TableOperation.Table != "inventory" {
		t.Fatal("signing lost binding")
	}
	adapted := Idempotent(endpoint, declarationStore(t), idempotency.Definition{ID: "table-query", Version: 1})
	replayInfo, err := InspectIdempotent(adapted)
	if err != nil {
		t.Fatal(err)
	}
	if replayInfo.TableOperation.Table != "inventory" {
		t.Fatal("idempotency lost binding")
	}
	router, err := NewRouter(endpoint.Handle(func(context.Context, Input[NoPath, NoQuery, EndpointPatch]) (EndpointReply, error) {
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	router.Endpoints()[0].TableOperation.Table = "router inspection"
	if router.Endpoints()[0].TableOperation.Table != "inventory" {
		t.Fatal("router exposed shared binding")
	}
	duplicate := ConfigureEndpoint(endpoint, TableOperation(contract.TableOperationBinding{Table: "inventory", Kind: contract.TableQuery}))
	if ValidateEndpoint(duplicate) == nil || ValidateEndpoint(ConfigureEndpoint(duplicate, Timeout(time.Second))) == nil {
		t.Fatal("duplicate option error is not sticky")
	}
	original := endpointContributionContract(endpoint)
	plain := endpoint
	plain.hasTableOperation = false
	plain.tableOperation = contract.TableOperationBinding{}
	changed := endpoint
	changed.tableOperation.Table = "other"
	if original.accepts(endpointContributionContract(plain)) || endpointContributionContract(plain).accepts(original) || original.accepts(endpointContributionContract(changed)) {
		t.Fatal("factory accepted a changed table association")
	}
}
