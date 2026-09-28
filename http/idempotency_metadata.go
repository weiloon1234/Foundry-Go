package http

import (
	stdhttp "net/http"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

const IdempotencyKeyHeader HeaderName = "Idempotency-Key"

// IdempotencyInfo is the replay policy used by the handler and generated client.
// Retention is a minimum until explicit pruning, not automatic callback expiry.
type IdempotencyInfo struct {
	Operation                 idempotency.OperationID `json:"operation"`
	Version                   uint32                  `json:"version"`
	Header                    HeaderName              `json:"header"`
	MinKeyBytes               int                     `json:"min_key_bytes"`
	MaxKeyBytes               int                     `json:"max_key_bytes"`
	KeyPattern                string                  `json:"key_pattern"`
	Fingerprint               string                  `json:"fingerprint"`
	SuccessOnly               bool                    `json:"success_only"`
	DuplicateWaitMilliseconds int64                   `json:"duplicate_wait_ms"`
	RetentionSeconds          int64                   `json:"retention_seconds"`
	ApplicationHeaders        []HeaderName            `json:"application_headers"`
}

func idempotencyInfo(d idempotency.Definition, c idempotency.Config, headers bool) IdempotencyInfo {
	i := IdempotencyInfo{Operation: d.ID, Version: d.Version, Header: IdempotencyKeyHeader, MinKeyBytes: idempotency.MinKeyBytes, MaxKeyBytes: c.MaxKeyBytes, KeyPattern: idempotencyKeyPattern(c.MaxKeyBytes), Fingerprint: "prepared-path-query-body-v1", SuccessOnly: true, DuplicateWaitMilliseconds: c.DuplicateWait.Milliseconds(), RetentionSeconds: int64(c.Retention / time.Second), ApplicationHeaders: []HeaderName{}}
	if headers {
		i.ApplicationHeaders = slices.Clone(replayHeaderNames)
	}
	return i
}
func idempotencyKeyPattern(maximum int) string { return idempotency.KeyPattern(maximum) }
func (i IdempotencyInfo) Validate() error {
	if (idempotency.Definition{ID: i.Operation, Version: i.Version}).Validate() != nil || i.Header != IdempotencyKeyHeader || i.MinKeyBytes != idempotency.MinKeyBytes || i.MaxKeyBytes < i.MinKeyBytes || i.MaxKeyBytes > idempotency.MaxKeyBytes || i.KeyPattern != idempotencyKeyPattern(i.MaxKeyBytes) || i.Fingerprint != "prepared-path-query-body-v1" || !i.SuccessOnly || i.DuplicateWaitMilliseconds < 1 || i.DuplicateWaitMilliseconds > int64(5*time.Minute/time.Millisecond) || i.RetentionSeconds < 3600 || i.RetentionSeconds > int64(365*24*time.Hour/time.Second) {
		return fault.New(fault.Invalid, "invalid HTTP idempotency policy")
	}
	if len(i.ApplicationHeaders) != 0 && !slices.Equal(i.ApplicationHeaders, replayHeaderNames) {
		return fault.New(fault.Invalid, "invalid replay header policy")
	}
	return nil
}
func (i *IdempotencyInfo) clone() *IdempotencyInfo {
	if i == nil {
		return nil
	}
	c := *i
	c.ApplicationHeaders = slices.Clone(i.ApplicationHeaders)
	return &c
}

type idempotencyKeyContext struct{}

func requestIdempotencyKey(r *stdhttp.Request, maximum int) (idempotency.Key, error) {
	values := r.Header.Values(string(IdempotencyKeyHeader))
	if len(values) != 1 || len(values[0]) > maximum {
		return idempotency.Key{}, IdempotencyBadKey
	}
	key, err := idempotency.ParseKey(values[0])
	if err != nil {
		return idempotency.Key{}, IdempotencyBadKey.WithCause(err)
	}
	return key, nil
}
