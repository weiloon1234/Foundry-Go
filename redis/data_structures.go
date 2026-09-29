package redis

import (
	"context"
	_ "embed"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
	"github.com/weiloon1234/Foundry-Go/redis/data"
	"github.com/weiloon1234/Foundry-Go/value"
)

//go:embed data_prelude.lua
var dataPrelude string

//go:embed data_hash.lua
var dataHashBody string

//go:embed data_zset.lua
var dataSortedSetBody string

//go:embed data_list.lua
var dataListBody string

var dataHashScript = dataPrelude + dataHashBody
var dataSortedSetScript = dataPrelude + dataSortedSetBody
var dataListScript = dataPrelude + dataListBody

var _ data.HashReadBackend = (*Client)(nil)
var _ data.HashCounterBackend = (*Client)(nil)
var _ data.SortedSetBackend = (*Client)(nil)
var _ data.ListBackend = (*Client)(nil)

// structureLimits validates the key kind and bounds and applies the client-wide
// value ceiling, like dataCommand, for the structure scripts.
func (c *Client) structureLimits(ctx context.Context, key data.Key, kind data.Kind, l data.Limits) (data.Limits, error) {
	if err := checkDataKind(key, kind); err != nil {
		return l, err
	}
	if err := l.Validate(); err != nil {
		return l, err
	}
	if err := c.valid(ctx); err != nil {
		return l, err
	}
	l.ValueBytes = min(l.ValueBytes, c.config.MaxValueBytes)
	return l, nil
}

// HashGetMany reads the requested fields atomically after checking every
// stored size against the value and reply bounds.
func (c *Client) HashGetMany(ctx context.Context, key data.Key, fields []string, l data.Limits) ([]value.Optional[string], error) {
	l, err := c.structureLimits(ctx, key, data.HashKind, l)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 || len(fields) > l.Entries {
		return nil, fault.New(fault.Invalid, "Redis hash field batch exceeds its bound")
	}
	for _, field := range fields {
		if err := l.ValidateField(field); err != nil {
			return nil, err
		}
	}
	reply, code, err := c.dataCall(ctx, dataHashScript, key, "hmget", l, fields...)
	if err != nil {
		return nil, err
	}
	if code != 1 {
		return nil, invalidDataReply()
	}
	result := make([]value.Optional[string], 0, len(fields))
	for i := 0; i < len(reply); i++ {
		flag, ok := reply[i].(int64)
		if !ok || flag != 0 && flag != 1 || len(result) == len(fields) {
			return nil, invalidDataReply()
		}
		if flag == 0 {
			result = append(result, value.Optional[string]{})
			continue
		}
		i++
		if i == len(reply) {
			return nil, invalidDataReply()
		}
		text, ok := reply[i].(string)
		if !ok || l.ValidateValue(text) != nil {
			return nil, invalidDataReply()
		}
		result = append(result, value.Set(text))
	}
	if len(result) != len(fields) {
		return nil, invalidDataReply()
	}
	return result, nil
}

// HashGetAll returns every field sorted by field bytes, after bounding
// cardinality, field sizes and the value reply budget on the server. The script
// returns fields unordered (Lua comparison follows the server locale); the
// adapter sorts them bytewise and rejects duplicates.
func (c *Client) HashGetAll(ctx context.Context, key data.Key, l data.Limits) ([]data.StoredField, error) {
	l, err := c.structureLimits(ctx, key, data.HashKind, l)
	if err != nil {
		return nil, err
	}
	reply, code, err := c.dataCall(ctx, dataHashScript, key, "hgetall", l)
	if err != nil {
		return nil, err
	}
	if code != 1 || len(reply)%2 != 0 || len(reply)/2 > l.Entries {
		return nil, invalidDataReply()
	}
	result := make([]data.StoredField, 0, len(reply)/2)
	for i := 0; i < len(reply); i += 2 {
		field, okField := reply[i].(string)
		text, okValue := reply[i+1].(string)
		if !okField || !okValue || l.ValidateField(field) != nil || l.ValidateValue(text) != nil {
			return nil, invalidDataReply()
		}
		result = append(result, data.StoredField{Field: field, Value: text})
	}
	slices.SortFunc(result, func(a, b data.StoredField) int { return strings.Compare(a.Field, b.Field) })
	for i := 1; i < len(result); i++ {
		if result[i].Field == result[i-1].Field {
			return nil, invalidDataReply()
		}
	}
	return result, nil
}

// HashIncrement uses Redis signed 64-bit arithmetic on a canonical integer field.
func (c *Client) HashIncrement(ctx context.Context, key data.Key, field string, delta int64, l data.Limits) (int64, error) {
	l, err := c.structureLimits(ctx, key, data.HashKind, l)
	if err != nil {
		return 0, err
	}
	if err := l.ValidateField(field); err != nil {
		return 0, err
	}
	if l.ValueBytes < cacheint.MaxBytes {
		return 0, fault.New(fault.Invalid, "Redis data value bound cannot hold every int64")
	}
	reply, code, err := c.dataCall(ctx, dataHashScript, key, "hincrby", l, field, strconv.FormatInt(delta, 10))
	if err != nil {
		return 0, err
	}
	if code != 1 || len(reply) != 1 {
		return 0, invalidDataReply()
	}
	text, ok := reply[0].(string)
	if !ok {
		return 0, invalidDataReply()
	}
	result, err := cacheint.Decode([]byte(text))
	if err != nil {
		return 0, invalidDataReply()
	}
	return result, nil
}

// formatScore renders a finite or infinite score in Redis's accepted syntax.
func formatScore(score float64) string {
	switch {
	case math.IsInf(score, 1):
		return "+inf"
	case math.IsInf(score, -1):
		return "-inf"
	}
	return strconv.FormatFloat(score, 'g', -1, 64)
}
func parseScore(reply any) (float64, error) {
	text, ok := reply.(string)
	if !ok {
		return 0, invalidDataReply()
	}
	score, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(score) {
		return 0, invalidDataReply()
	}
	return score, nil
}
func scoreBound(score float64, exclusive bool) string {
	if exclusive {
		return "(" + formatScore(score)
	}
	return formatScore(score)
}
func orderArgument(order data.Order) string {
	if order == data.Descending {
		return "desc"
	}
	return "asc"
}
func (c *Client) sortedSetMember(ctx context.Context, key data.Key, member string, l data.Limits) (data.Limits, error) {
	l, err := c.structureLimits(ctx, key, data.SortedSetKind, l)
	if err != nil {
		return l, err
	}
	return l, l.ValidateValue(member)
}

// SortedSetAdd inserts or re-scores one member, rejecting growth when full.
func (c *Client) SortedSetAdd(ctx context.Context, key data.Key, member string, score float64, l data.Limits) (bool, error) {
	if err := data.ValidateScore(score); err != nil {
		return false, err
	}
	l, err := c.sortedSetMember(ctx, key, member, l)
	if err != nil {
		return false, err
	}
	return dataBoolean(c.dataCall(ctx, dataSortedSetScript, key, "zadd", l, member, formatScore(score)))
}

// SortedSetIncrement adds delta to one member's score (missing starts at zero).
func (c *Client) SortedSetIncrement(ctx context.Context, key data.Key, member string, delta float64, l data.Limits) (float64, error) {
	if err := data.ValidateScore(delta); err != nil {
		return 0, err
	}
	l, err := c.sortedSetMember(ctx, key, member, l)
	if err != nil {
		return 0, err
	}
	reply, code, err := c.dataCall(ctx, dataSortedSetScript, key, "zincrby", l, member, formatScore(delta))
	if err != nil {
		return 0, err
	}
	if code != 1 || len(reply) != 1 {
		return 0, invalidDataReply()
	}
	return parseScore(reply[0])
}
func (c *Client) SortedSetRemove(ctx context.Context, key data.Key, member string, l data.Limits) (bool, error) {
	l, err := c.sortedSetMember(ctx, key, member, l)
	if err != nil {
		return false, err
	}
	return dataBoolean(c.dataCall(ctx, dataSortedSetScript, key, "zrem", l, member))
}
func (c *Client) SortedSetScore(ctx context.Context, key data.Key, member string, l data.Limits) (float64, bool, error) {
	l, err := c.sortedSetMember(ctx, key, member, l)
	if err != nil {
		return 0, false, err
	}
	reply, code, err := c.dataCall(ctx, dataSortedSetScript, key, "zscore", l, member)
	if err != nil || code == 0 {
		return 0, false, err
	}
	if len(reply) != 1 {
		return 0, false, invalidDataReply()
	}
	score, err := parseScore(reply[0])
	return score, err == nil, err
}
func (c *Client) SortedSetRank(ctx context.Context, key data.Key, member string, order data.Order, l data.Limits) (uint64, bool, error) {
	if err := order.Validate(); err != nil {
		return 0, false, err
	}
	l, err := c.sortedSetMember(ctx, key, member, l)
	if err != nil {
		return 0, false, err
	}
	reply, code, err := c.dataCall(ctx, dataSortedSetScript, key, "zrank", l, member, orderArgument(order))
	if err != nil || code == 0 {
		return 0, false, err
	}
	if len(reply) != 1 {
		return 0, false, invalidDataReply()
	}
	rank, ok := reply[0].(int64)
	if !ok || rank < 0 || rank >= int64(l.Entries) {
		return 0, false, invalidDataReply()
	}
	return uint64(rank), true, nil
}

// SortedSetRange returns a rank window with scores.
func (c *Client) SortedSetRange(ctx context.Context, key data.Key, window data.Window, l data.Limits) ([]data.StoredMember, error) {
	l, err := c.windowLimits(ctx, key, window, l)
	if err != nil {
		return nil, err
	}
	return scoredReply(c.dataCall(ctx, dataSortedSetScript, key, "zrange", l, strconv.Itoa(window.Offset), strconv.Itoa(window.Count), orderArgument(window.Order)))(window, l)
}

// SortedSetRangeByScore returns a score range, windowed with Redis LIMIT.
func (c *Client) SortedSetRangeByScore(ctx context.Context, key data.Key, scores data.ScoreRange, window data.Window, l data.Limits) ([]data.StoredMember, error) {
	if err := scores.Validate(); err != nil {
		return nil, err
	}
	l, err := c.windowLimits(ctx, key, window, l)
	if err != nil {
		return nil, err
	}
	return scoredReply(c.dataCall(ctx, dataSortedSetScript, key, "zrangebyscore", l, scoreBound(scores.Min, scores.ExcludeMin), scoreBound(scores.Max, scores.ExcludeMax), strconv.Itoa(window.Offset), strconv.Itoa(window.Count), orderArgument(window.Order)))(window, l)
}
func (c *Client) SortedSetCountByScore(ctx context.Context, key data.Key, scores data.ScoreRange, l data.Limits) (uint64, error) {
	if err := scores.Validate(); err != nil {
		return 0, err
	}
	l, err := c.structureLimits(ctx, key, data.SortedSetKind, l)
	if err != nil {
		return 0, err
	}
	fields, code, err := c.dataCall(ctx, dataSortedSetScript, key, "zcount", l, scoreBound(scores.Min, scores.ExcludeMin), scoreBound(scores.Max, scores.ExcludeMax))
	return dataCount(fields, code, err, l.Entries)
}
func (c *Client) windowLimits(ctx context.Context, key data.Key, window data.Window, l data.Limits) (data.Limits, error) {
	l, err := c.structureLimits(ctx, key, data.SortedSetKind, l)
	if err != nil {
		return l, err
	}
	if err := window.Order.Validate(); err != nil {
		return l, err
	}
	if window.Offset < 0 || window.Offset > data.MaxEntries || window.Count < 1 || window.Count > l.Entries {
		return l, fault.New(fault.Invalid, "Redis sorted set window exceeds its bound")
	}
	return l, nil
}

// scoredReply decodes flat member/score pairs within the window and bounds.
func scoredReply(reply []any, code int64, err error) func(data.Window, data.Limits) ([]data.StoredMember, error) {
	return func(window data.Window, l data.Limits) ([]data.StoredMember, error) {
		if err != nil {
			return nil, err
		}
		if code != 1 || len(reply)%2 != 0 || len(reply)/2 > window.Count {
			return nil, invalidDataReply()
		}
		result := make([]data.StoredMember, 0, len(reply)/2)
		remaining := l.ReplyBytes
		for i := 0; i < len(reply); i += 2 {
			member, ok := reply[i].(string)
			if !ok || l.ValidateValue(member) != nil || len(member) > remaining {
				return nil, invalidDataReply()
			}
			remaining -= len(member)
			score, err := parseScore(reply[i+1])
			if err != nil {
				return nil, err
			}
			result = append(result, data.StoredMember{Member: member, Score: score})
		}
		return result, nil
	}
}
func endArgument(end data.End) string {
	if end == data.Front {
		return "front"
	}
	return "back"
}

// ListPush inserts every value in one command after checking sizes and the
// resulting length, and returns the new length.
func (c *Client) ListPush(ctx context.Context, key data.Key, values []string, end data.End, l data.Limits) (uint64, error) {
	if err := end.Validate(); err != nil {
		return 0, err
	}
	l, err := c.structureLimits(ctx, key, data.ListKind, l)
	if err != nil {
		return 0, err
	}
	if len(values) == 0 || len(values) > l.Entries {
		return 0, fault.New(fault.Invalid, "Redis list push exceeds its bound")
	}
	for _, text := range values {
		if err := l.ValidateValue(text); err != nil {
			return 0, err
		}
	}
	args := append([]string{endArgument(end)}, values...)
	fields, code, err := c.dataCall(ctx, dataListScript, key, "push", l, args...)
	return dataCount(fields, code, err, l.Entries)
}

// ListPop removes one element after checking its size.
func (c *Client) ListPop(ctx context.Context, key data.Key, end data.End, l data.Limits) (string, bool, error) {
	if err := end.Validate(); err != nil {
		return "", false, err
	}
	l, err := c.structureLimits(ctx, key, data.ListKind, l)
	if err != nil {
		return "", false, err
	}
	reply, code, err := c.dataCall(ctx, dataListScript, key, "pop", l, endArgument(end))
	if err != nil || code == 0 {
		return "", false, err
	}
	if len(reply) != 1 {
		return "", false, invalidDataReply()
	}
	text, ok := reply[0].(string)
	if !ok || l.ValidateValue(text) != nil {
		return "", false, invalidDataReply()
	}
	return text, true, nil
}

// ListRange returns elements start..stop inclusive within the reply bounds.
func (c *Client) ListRange(ctx context.Context, key data.Key, start, stop int64, l data.Limits) ([]string, error) {
	if err := data.ValidateIndexRange(start, stop); err != nil {
		return nil, err
	}
	l, err := c.structureLimits(ctx, key, data.ListKind, l)
	if err != nil {
		return nil, err
	}
	reply, code, err := c.dataCall(ctx, dataListScript, key, "range", l, strconv.FormatInt(start, 10), strconv.FormatInt(stop, 10))
	if err != nil {
		return nil, err
	}
	if code != 1 || len(reply) > l.Entries {
		return nil, invalidDataReply()
	}
	result := make([]string, len(reply))
	remaining := l.ReplyBytes
	for i, item := range reply {
		text, ok := item.(string)
		if !ok || l.ValidateValue(text) != nil || len(text) > remaining {
			return nil, invalidDataReply()
		}
		remaining -= len(text)
		result[i] = text
	}
	return result, nil
}

// ListTrim keeps elements start..stop inclusive (Redis LTRIM semantics).
func (c *Client) ListTrim(ctx context.Context, key data.Key, start, stop int64, l data.Limits) error {
	if err := data.ValidateIndexRange(start, stop); err != nil {
		return err
	}
	l, err := c.structureLimits(ctx, key, data.ListKind, l)
	if err != nil {
		return err
	}
	fields, code, err := c.dataCall(ctx, dataListScript, key, "trim", l, strconv.FormatInt(start, 10), strconv.FormatInt(stop, 10))
	if err != nil {
		return err
	}
	if code != 1 || len(fields) != 0 {
		return invalidDataReply()
	}
	return nil
}
