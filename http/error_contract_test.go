package http_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestErrorContractMatchesActualEnvelopeAndOwnsMetadata(t *testing.T) {
	dto := foundryhttp.ErrorResponseJSON()
	info, err := dto.Description()
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []foundryhttp.ErrorResponse{
		{Status: 404, Code: foundryhttp.NotFound, Message: "Resource not found"},
		{Status: 422, Code: foundryhttp.ValidationFailed, Message: "Validation failed", RequestID: "request-1", IssuesTruncated: true, Issues: []contract.Issue{{Path: "/body/name", Code: "foundry.non_blank", LabelKey: "profile.name"}}},
	} {
		wire, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := dto.Decode(t.Context(), wire, foundryhttp.DefaultEndpointLimits().Response)
		if err != nil || decoded.Status != input.Status || decoded.Code != input.Code {
			t.Fatal("error contract differs from runtime wire", err)
		}
	}
	for _, wire := range []string{`{"status":"404","error_code":"not_found","message":"Missing"}`, `{"status":404,"message":"Missing"}`, `{"status":404,"error_code":"not_found","message":"Missing","private":"secret"}`} {
		if _, err := dto.Decode(t.Context(), []byte(wire), foundryhttp.DefaultEndpointLimits().Response); err == nil {
			t.Fatal("invalid error shape accepted")
		}
	}
	info.Types[0].ID = "changed"
	again, err := dto.Description()
	if err != nil || again.Types[0].ID == "changed" {
		t.Fatal("metadata aliases caller")
	}
}
