// Package webhookcapturestore owns the append-only webhook capture table.
// Rows arrive already redacted; transport capture and disclosure policy belong
// to webhook/capture. Only that package imports this one.
package webhookcapturestore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Capture is one row of foundry_webhook_captures. The writer inserts it once
// and retention deletes it; nothing updates a row. Text columns BodyMode,
// BodyState, BodyOmission, ResponseState and the Omissions elements hold the
// closed webhook/capture enum values that the migration's CHECKs enforce.
type Capture struct {
	// ID is the kernel's UUIDv7 request ID for the captured request.
	ID                  model.ID[Capture]
	ReceivedAt          temporal.DateTime
	Method              string
	Path                string
	Route               value.Nullable[string]
	ClientIP            value.Nullable[string]
	Headers             value.JSON[[]httpcapture.Field]
	Query               value.JSON[[]httpcapture.Field]
	ContentType         string
	ContentEncoding     string
	DeclaredBytes       value.Nullable[int64]
	MeasuredBytes       value.Nullable[int64]
	ObservedBytes       int64
	BodyMode            string
	BodyState           string
	BodyOmission        value.Nullable[string]
	Body                []byte
	ResponseStatus      value.Nullable[int32]
	Outcome             observability.Outcome
	ResponseState       string
	ResponseContentType string
	ResponseEncoding    string
	ResponseBytes       int64
	ResponseHead        []byte
	ResponseHeaders     value.JSON[[]httpcapture.Field]
	DurationMS          int64
	Omissions           value.JSON[[]string]
	DroppedBefore       int64
}

// Format prints a fixed label so captured data never reaches diagnostics.
func (Capture) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook capture [redacted]")) }

// Size is the row's retained payload estimate used for write batching.
func (c Capture) Size() int {
	headers, _ := c.Headers.Text()
	query, _ := c.Query.Text()
	responseHeaders, _ := c.ResponseHeaders.Text()
	return len(c.Body) + len(c.ResponseHead) + len(headers) + len(query) + len(responseHeaders) + len(c.Path) + 1024
}

// Summary is the narrow list projection. It never selects bodies, headers or
// query values.
type Summary struct {
	ID             model.ID[Capture]
	ReceivedAt     temporal.DateTime
	Method         string
	Path           string
	Route          value.Nullable[string]
	ClientIP       value.Nullable[string]
	ResponseStatus value.Nullable[int32]
	Outcome        observability.Outcome
	ResponseState  string
	DurationMS     int64
	BodyMode       string
	BodyState      string
	BodyOmission   value.Nullable[string]
	DeclaredBytes  value.Nullable[int64]
	MeasuredBytes  value.Nullable[int64]
	ObservedBytes  int64
	DroppedBefore  int64
}

// Format prints a fixed label so captured data never reaches diagnostics.
func (Summary) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("webhook capture summary [redacted]"))
}
