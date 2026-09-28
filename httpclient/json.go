package httpclient

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func jsonLimits(maximum int64) contract.JSONLimits {
	return contract.JSONLimits{Bytes: int(maximum), Depth: 32, Nodes: 10000, Steps: 100000, Issues: 16}
}

// JSON reuses the request DTO's generated codec and supplies Content-Type. It
// retains the concrete payload type and encodes once before any network attempt.
func JSON[T any](ctx context.Context, request Request, descriptor contract.JSON[T], payload T) (Request, error) {
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	data, err := descriptor.Encode(ctx, payload, jsonLimits(request.client.config.RequestBytes))
	if err != nil {
		return Request{}, failure(InvalidRequest, request.client.Name(), request.method, 0, 0, err)
	}
	result := request.WithBody(Bytes(data)).Header("Content-Type", "application/json")
	return result, result.Validate()
}

// DecodeJSON validates the complete received DTO against its existing generated
// contract. Status handling is explicit through Response.EnsureSuccess.
func DecodeJSON[T any](ctx context.Context, response Response, descriptor contract.JSON[T]) (T, error) {
	if response.info.status == 0 {
		return *new(T), invalid()
	}
	result, err := descriptor.Decode(ctx, response.data, jsonLimits(response.info.maximum))
	if err != nil {
		return *new(T), failure(DecodeFailed, response.info.name, response.info.method, response.info.attempt, response.info.status, err)
	}
	return result, nil
}
