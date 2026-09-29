package redis

import (
	"context"
	_ "embed"
	"errors"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

var _ raw.Backend = (*Client)(nil)

//go:embed raw_expiry.lua
var rawExpiryBody string
var rawExpiryScript = expiryScript + rawExpiryBody

// rawArguments is the single driver argument encoder for commands, scripts and
// both pipeline modes. Only explicitly declared keys enter a script's KEYS list.
func rawArguments(request raw.Request) []any {
	text := request.Arguments()
	if !request.IsScript() {
		args := make([]any, len(text))
		for i, v := range text {
			args[i] = v
		}
		return args
	}
	keys := request.Keys()
	args := make([]any, 0, 3+len(keys)+len(text))
	args = append(args, "EVAL", request.Source(), len(keys))
	for _, key := range keys {
		args = append(args, key.String())
	}
	for _, v := range text {
		args = append(args, v)
	}
	return args
}
func validateRawRequest(request raw.Request, limits raw.Limits) error {
	keys := request.Keys()
	if len(keys) == 0 {
		return fault.New(fault.Invalid, "raw Redis command requires a key")
	}
	return request.Validate(keys[0].Namespace(), limits)
}

// ExecuteRaw performs one attempt. Missing Redis replies remain explicit nil
// values for a nullable decoder; server and transport errors never become misses.
func (c *Client) ExecuteRaw(ctx context.Context, request raw.Request, limits raw.Limits) (raw.Reply, error) {
	if err := validateRawRequest(request, limits); err != nil {
		return raw.Reply{}, err
	}
	result, err := c.execute(ctx, func(ctx context.Context, client *driver.Client) (any, error) {
		value, err := client.Do(ctx, rawArguments(request)...).Result()
		if err != nil && !errors.Is(err, driver.Nil) {
			return nil, err
		}
		return raw.CaptureReply(ctx, value, limits.Reply)
	})
	if err != nil {
		return raw.Reply{}, err
	}
	return result.(raw.Reply), nil
}
func (c *Client) ExecuteRawBatch(ctx context.Context, requests []raw.Request, mode raw.Mode, limits raw.Limits) ([]raw.Reply, error) {
	if err := mode.Validate(); err != nil {
		return nil, err
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if len(requests) > limits.Commands {
		return nil, fault.New(fault.Invalid, "raw Redis pipeline exceeds its command bound")
	}
	remaining := limits.RequestBytes
	for i, r := range requests {
		if err := validateRawRequest(r, limits); err != nil {
			return nil, err
		}
		if r.Bytes() > remaining {
			return nil, fault.New(fault.Invalid, "raw Redis pipeline exceeds its byte bound")
		}
		remaining -= r.Bytes()
		if i > 0 && r.Keys()[0].Namespace() != requests[0].Keys()[0].Namespace() {
			return nil, fault.New(fault.Invalid, "raw Redis pipeline keys must share a namespace")
		}
	}
	result, err := c.execute(ctx, func(ctx context.Context, client *driver.Client) (any, error) {
		if len(requests) == 0 {
			return []raw.Reply{}, nil
		}
		var pipeline driver.Pipeliner
		if mode == raw.Transaction {
			pipeline = client.TxPipeline()
		} else {
			pipeline = client.Pipeline()
		}
		commands := make([]*driver.Cmd, len(requests))
		for i, r := range requests {
			commands[i] = pipeline.Do(ctx, rawArguments(r)...)
		}
		_, err := pipeline.Exec(ctx)
		if err != nil && !errors.Is(err, driver.Nil) {
			return nil, err
		}
		values := make([]any, len(commands))
		for i, command := range commands {
			value, err := command.Result()
			if err != nil && !errors.Is(err, driver.Nil) {
				return nil, err
			}
			values[i] = value
		}
		return raw.CaptureReplies(ctx, values, limits.Reply)
	})
	if err != nil {
		return nil, err
	}
	return result.([]raw.Reply), nil
}
func (c *Client) ExpireRaw(ctx context.Context, key raw.Key, ttl cache.TTL) (bool, error) {
	if err := key.Validate(); err != nil {
		return false, err
	}
	expiry, err := milliseconds(ttl)
	if err != nil {
		return false, err
	}
	result, err := c.execute(ctx, func(ctx context.Context, client *driver.Client) (any, error) {
		return evalScript(ctx, client, rawExpiryScript, []string{key.String()}, expiry).Result()
	})
	if err != nil {
		return false, err
	}
	n, ok := result.(int64)
	if !ok || n != 0 && n != 1 {
		return false, fault.New(fault.Internal, "invalid Redis expiry result")
	}
	return n == 1, nil
}
