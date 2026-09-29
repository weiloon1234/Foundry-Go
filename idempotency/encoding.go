package idempotency

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// Encoding retains the concrete operation input/result at persistence boundaries.
// JSONEncoding reuses generated DTO metadata and codecs. Transport adapters may
// explicitly compose existing descriptors with DefineEncoding; they must include
// all semantic input and keep codecs deterministic, bounded and concurrency-safe.
type Encoding[T any] struct {
	identity string
	encode   func(context.Context, T, int) ([]byte, error)
	decode   func(context.Context, []byte, int) (T, error)
	err      error
}

// DefineEncoding is the explicit adapter boundary. identity is a versioned wire
// contract digest recorded with each outcome. An outcome stored under another
// identity replays only when decode accepts its exact representation, so decode
// must reject anything the current contract does not describe (unknown fields,
// missing required values); an incompatible outcome stays unavailable.
// Encoded values must be complete JSON documents, checked by the shared parser.
func DefineEncoding[T any](identity string, encode func(context.Context, T, int) ([]byte, error), decode func(context.Context, []byte, int) (T, error)) Encoding[T] {
	e := Encoding[T]{identity: identity, encode: encode, decode: decode}
	if !identityPart(identity) || encode == nil || decode == nil {
		e.err = invalid("idempotency encoding needs a bounded identity and both codecs")
	}
	return e
}
func JSONEncoding[T any](descriptor contract.JSON[T]) Encoding[T] {
	schema, err := descriptor.Description()
	if err != nil {
		return Encoding[T]{err: err}
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return Encoding[T]{err: err}
	}
	return DefineEncoding(digest("foundry.idempotency.json.v1", string(data)), func(ctx context.Context, v T, limit int) ([]byte, error) {
		return descriptor.Encode(ctx, v, encodingLimits(limit))
	}, func(ctx context.Context, data []byte, limit int) (T, error) {
		return descriptor.Decode(ctx, data, encodingLimits(limit))
	})
}
func encodingLimits(limit int) contract.JSONLimits {
	return contract.JSONLimits{Bytes: limit, Depth: jsonwire.MaxDepth, Nodes: jsonwire.MaxNodes, Steps: 100000, Issues: 32}
}
func (e Encoding[T]) Validate() error {
	if e.err != nil {
		return e.err
	}
	if e.identity == "" || e.encode == nil || e.decode == nil {
		return invalid("idempotency encoding is undefined")
	}
	return nil
}
func (Encoding[T]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("idempotency encoding")) }

// Input is the one-way, concrete fingerprint boundary. It never deserializes
// stored input because raw request input is not persisted.
type Input[T any] struct {
	identity string
	encode   func(context.Context, T, int) ([]byte, error)
	err      error
}

func DefineInput[T any](identity string, encode func(context.Context, T, int) ([]byte, error)) Input[T] {
	i := Input[T]{identity: identity, encode: encode}
	if !identityPart(identity) || encode == nil {
		i.err = invalid("idempotency input needs a bounded identity and codec")
	}
	return i
}
func JSONInput[T any](descriptor contract.JSON[T]) Input[T] {
	e := JSONEncoding(descriptor)
	return Input[T]{identity: e.identity, encode: e.encode, err: e.err}
}
func (i Input[T]) Validate() error {
	if i.err != nil {
		return i.err
	}
	if i.identity == "" || i.encode == nil {
		return invalid("idempotency input is undefined")
	}
	return nil
}
func (Input[T]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("idempotency input")) }
func (e Encoding[T]) prepare(ctx context.Context, v T, limit int) ([]byte, error) {
	return (Input[T]{identity: e.identity, encode: e.encode}).prepare(ctx, v, limit)
}
func (e Input[T]) prepare(ctx context.Context, v T, limit int) ([]byte, error) {
	var data []byte
	err := callback.Isolated("idempotency encode", func() error { var err error; data, err = e.encode(ctx, v, limit); return err })
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: limit, Depth: jsonwire.MaxDepth, Nodes: jsonwire.MaxNodes}); err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}
func (e Encoding[T]) restore(ctx context.Context, data []byte, limit int) (T, error) {
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: limit, Depth: jsonwire.MaxDepth, Nodes: jsonwire.MaxNodes}); err != nil {
		return *new(T), err
	}
	var value T
	err := callback.Isolated("idempotency decode", func() error {
		var err error
		value, err = e.decode(ctx, append([]byte(nil), data...), limit)
		return err
	})
	if err != nil {
		return *new(T), err
	}
	if err := ctx.Err(); err != nil {
		return *new(T), err
	}
	return value, nil
}
