package redis

import (
	"context"
	_ "embed"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

//go:embed data.lua
var dataBody string
var dataScript = expiryScript + dataBody

//go:embed data_batch.lua
var dataBatchScript string
var _ data.Backend = (*Client)(nil)
var _ data.HashBackend = (*Client)(nil)
var _ data.SetBackend = (*Client)(nil)

func (c *Client) dataCommand(ctx context.Context, key data.Key, op string, l data.Limits, args ...string) ([]any, int64, error) {
	if err := key.Validate(); err != nil {
		return nil, 0, err
	}
	if err := l.Validate(); err != nil {
		return nil, 0, err
	}
	if err := c.valid(ctx); err != nil {
		return nil, 0, err
	}
	// The client-wide value ceiling also applies to this borrowed feature.
	l.ValueBytes = min(l.ValueBytes, c.config.MaxValueBytes)
	if len(args) > 0 && key.Kind() == data.HashKind && op != "expire" {
		if err := l.ValidateField(args[0]); err != nil {
			return nil, 0, err
		}
	}
	if op == "sadd" || op == "srem" || op == "contains" {
		if err := l.ValidateValue(args[0]); err != nil {
			return nil, 0, err
		}
	}
	if op == "hset" {
		if err := l.ValidateValue(args[1]); err != nil {
			return nil, 0, err
		}
	}
	values := []any{op, string(key.Kind()), l.Entries, l.FieldBytes, l.ValueBytes, l.ReplyBytes}
	for _, arg := range args {
		values = append(values, arg)
	}
	reply, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return raw.Eval(ctx, dataScript, []string{key.String()}, values...).Result()
	})
	if err != nil {
		return nil, 0, err
	}
	return dataReply(reply)
}
func dataReply(reply any) ([]any, int64, error) {
	fields, ok := reply.([]any)
	if !ok || len(fields) == 0 {
		return nil, 0, invalidDataReply()
	}
	code, ok := fields[0].(int64)
	if !ok {
		return nil, 0, invalidDataReply()
	}
	if code == -1 && len(fields) == 1 {
		return nil, 0, fault.New(fault.Invalid, "stored Redis data has the wrong type or exceeds its bound")
	}
	if code != 0 && code != 1 || code == 0 && len(fields) != 1 {
		return nil, 0, invalidDataReply()
	}
	return fields[1:], code, nil
}
func invalidDataReply() error { return fault.New(fault.Internal, "invalid Redis data reply") }
func dataBoolean(fields []any, code int64, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	if len(fields) != 0 {
		return false, invalidDataReply()
	}
	return code == 1, nil
}
func dataCount(fields []any, code int64, err error, max int) (uint64, error) {
	if err != nil {
		return 0, err
	}
	if code != 1 || len(fields) != 1 {
		return 0, invalidDataReply()
	}
	count, ok := fields[0].(int64)
	if !ok || count < 0 || count > int64(max) {
		return 0, invalidDataReply()
	}
	return uint64(count), nil
}
func checkDataKind(key data.Key, kind data.Kind) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if key.Kind() != kind {
		return fault.New(fault.Invalid, "wrong Redis data key kind")
	}
	return nil
}
func (c *Client) DataExists(ctx context.Context, key data.Key) (bool, error) {
	return dataBoolean(c.dataCommand(ctx, key, "exists", data.DefaultLimits()))
}
func (c *Client) DataExpire(ctx context.Context, key data.Key, ttl cache.TTL) (bool, error) {
	expiry, err := milliseconds(ttl)
	if err != nil {
		return false, err
	}
	return dataBoolean(c.dataCommand(ctx, key, "expire", data.DefaultLimits(), expiry))
}
func (c *Client) DataCount(ctx context.Context, key data.Key, l data.Limits) (uint64, error) {
	fields, code, err := c.dataCommand(ctx, key, "count", l)
	return dataCount(fields, code, err, l.Entries)
}
func (c *Client) DataDeleteMany(ctx context.Context, keys []data.Key) (uint64, error) {
	if err := data.ValidateBatch(keys); err != nil {
		return 0, err
	}
	reply, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		if len(keys) == 0 {
			return []any{int64(1), int64(0)}, nil
		}
		addresses := make([]string, len(keys))
		kinds := make([]any, len(keys))
		for i, k := range keys {
			addresses[i] = k.String()
			kinds[i] = string(k.Kind())
		}
		return raw.Eval(ctx, dataBatchScript, addresses, kinds...).Result()
	})
	if err != nil {
		return 0, err
	}
	fields, code, err := dataReply(reply)
	return dataCount(fields, code, err, len(keys))
}
func (c *Client) HashGet(ctx context.Context, key data.Key, field string, l data.Limits) (string, bool, error) {
	if err := checkDataKind(key, data.HashKind); err != nil {
		return "", false, err
	}
	fields, code, err := c.dataCommand(ctx, key, "hget", l, field)
	if err != nil {
		return "", false, err
	}
	if code == 0 {
		return "", false, nil
	}
	if len(fields) != 1 {
		return "", false, invalidDataReply()
	}
	text, ok := fields[0].(string)
	if !ok || l.ValidateValue(text) != nil || len(text) > c.config.MaxValueBytes {
		return "", false, invalidDataReply()
	}
	return text, true, nil
}
func (c *Client) HashSet(ctx context.Context, key data.Key, field, value string, l data.Limits) (bool, error) {
	if err := checkDataKind(key, data.HashKind); err != nil {
		return false, err
	}
	return dataBoolean(c.dataCommand(ctx, key, "hset", l, field, value))
}
func (c *Client) HashDelete(ctx context.Context, key data.Key, field string, l data.Limits) (bool, error) {
	if err := checkDataKind(key, data.HashKind); err != nil {
		return false, err
	}
	return dataBoolean(c.dataCommand(ctx, key, "hdel", l, field))
}
func (c *Client) SetAdd(ctx context.Context, key data.Key, member string, l data.Limits) (bool, error) {
	if err := checkDataKind(key, data.SetKind); err != nil {
		return false, err
	}
	return dataBoolean(c.dataCommand(ctx, key, "sadd", l, member))
}
func (c *Client) SetRemove(ctx context.Context, key data.Key, member string, l data.Limits) (bool, error) {
	if err := checkDataKind(key, data.SetKind); err != nil {
		return false, err
	}
	return dataBoolean(c.dataCommand(ctx, key, "srem", l, member))
}
func (c *Client) SetContains(ctx context.Context, key data.Key, member string, l data.Limits) (bool, error) {
	if err := checkDataKind(key, data.SetKind); err != nil {
		return false, err
	}
	return dataBoolean(c.dataCommand(ctx, key, "contains", l, member))
}
func (c *Client) SetMembers(ctx context.Context, key data.Key, l data.Limits) ([]string, error) {
	if err := checkDataKind(key, data.SetKind); err != nil {
		return nil, err
	}
	fields, code, err := c.dataCommand(ctx, key, "members", l)
	if err != nil {
		return nil, err
	}
	if code != 1 || len(fields) > l.Entries {
		return nil, invalidDataReply()
	}
	result := make([]string, len(fields))
	remaining := l.ReplyBytes
	for i, field := range fields {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, ok := field.(string)
		if !ok || l.ValidateValue(text) != nil || len(text) > c.config.MaxValueBytes || len(text) > remaining || i > 0 && text <= result[i-1] {
			return nil, invalidDataReply()
		}
		remaining -= len(text)
		result[i] = text
	}
	return result, nil
}
