package http

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// maxResponseStatuses bounds the success statuses one response declares.
const maxResponseStatuses = 8

// Statused is a typed JSON result together with the success status its handler
// selected from the response descriptor's declared statuses (see JSONResponses).
// A zero Status selects the primary status.
type Statused[R any] struct {
	Status int
	Value  R
}

// JSONResponses declares a JSON response with several success statuses, primary
// first, for example 200 and 201 for an upsert. The handler returns Statused[R]
// and selects one; zero selects status. Every status shares the descriptor.
// Selecting an undeclared status is an internal failure and publishes nothing.
// Metadata, OpenAPI and generated clients expose every declared status; the
// TypeScript client returns the received status with the decoded value.
// Idempotent endpoints require a single status, because a replay reproduces it.
func JSONResponses[R any](descriptor contract.JSON[R], status int, alternatives ...int) Response[Statused[R]] {
	return Response[Statused[R]]{
		kind: payloadJSON, status: status, json: statusedJSON[R]{descriptor: descriptor},
		statuses: append([]int{status}, alternatives...), selectStatus: func(result Statused[R]) int { return result.Status },
	}
}

// statusedJSON encodes the selected value with the declared descriptor.
type statusedJSON[R any] struct{ descriptor contract.JSON[R] }

func (j statusedJSON[R]) Validate() error                       { return j.descriptor.Validate() }
func (j statusedJSON[R]) Description() (contract.Schema, error) { return j.descriptor.Description() }
func (j statusedJSON[R]) Encode(ctx context.Context, result Statused[R], limits contract.JSONLimits) ([]byte, error) {
	return j.descriptor.Encode(ctx, result.Value, limits)
}

func validateResponseStatuses(statuses []int, primary int) error {
	if len(statuses) < 2 || len(statuses) > maxResponseStatuses || statuses[0] != primary {
		return fault.New(fault.Invalid, "response statuses require a primary and one to seven alternatives")
	}
	for index, status := range statuses {
		if !bodySuccessStatus(status) || slices.Contains(statuses[:index], status) {
			return fault.New(fault.Invalid, "response statuses must be unique body-bearing success statuses")
		}
	}
	return nil
}

// selectedStatus returns the declared status a handler chose for its result.
func (r Response[R]) selectedStatus(result R) (int, error) {
	if r.selectStatus == nil {
		return r.status, nil
	}
	status := r.selectStatus(result)
	if status == 0 {
		return r.status, nil
	}
	if !slices.Contains(r.statuses, status) {
		return 0, InternalError.WithCause(fault.New(fault.Internal, "handler selected an undeclared response status"))
	}
	return status, nil
}
