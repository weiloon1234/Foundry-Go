// Package outboundcapturestore owns the append-only outbound HTTP capture
// table. Rows arrive already redacted; capture and disclosure policy belong
// to httpclient/capture. Only that package and application assembly import
// this one.
package outboundcapturestore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Operation owns operation identifiers: one Do, Stream or Download call.
type Operation struct{}

// Capture is one row of foundry_outbound_captures: one wire attempt. The
// writer inserts it once and retention deletes it; nothing updates a row.
// Result, BodyMode, the body state and omission columns and the Omissions
// elements hold the closed httpclient/capture enum values the migration's
// CHECKs enforce.
type Capture struct {
	ID                    model.ID[Capture]
	OperationID           model.ID[Operation]
	Attempt               int16
	Retried               bool
	Client                string
	StartedAt             temporal.DateTime
	HeadersMS             value.Nullable[int64]
	DurationMS            int64
	Result                string
	RequestID             value.Nullable[string]
	TraceID               value.Nullable[string]
	Route                 value.Nullable[string]
	Method                string
	Scheme                string
	Host                  string
	Path                  string
	Query                 value.JSON[[]httpcapture.Field]
	RequestHeaders        value.JSON[[]httpcapture.Field]
	BodyMode              string
	RequestBodyState      string
	RequestBodyOmission   value.Nullable[string]
	RequestBody           []byte
	RequestDeclaredBytes  value.Nullable[int64]
	RequestSentBytes      int64
	RequestBodyComplete   bool
	Status                value.Nullable[int16]
	Protocol              value.Nullable[string]
	RemoteIP              value.Nullable[string]
	ConnectionReused      value.Nullable[bool]
	ResponseHeaders       value.JSON[[]httpcapture.Field]
	ResponseBodyState     string
	ResponseBodyOmission  value.Nullable[string]
	ResponseBody          []byte
	ResponseDeclaredBytes value.Nullable[int64]
	ResponseReadBytes     int64
	ResponseBodyComplete  bool
	Omissions             value.JSON[[]string]
	DroppedBefore         int64
}

// Format prints a fixed label so captured data never reaches diagnostics.
func (Capture) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("outbound capture [redacted]")) }

// Size is the row's retained payload estimate used for write batching.
func (c Capture) Size() int {
	query, _ := c.Query.Text()
	requestHeaders, _ := c.RequestHeaders.Text()
	responseHeaders, _ := c.ResponseHeaders.Text()
	return len(c.RequestBody) + len(c.ResponseBody) + len(query) + len(requestHeaders) + len(responseHeaders) + len(c.Path) + 1024
}

// Summary is the narrow list projection. It never selects bodies, headers or
// query values.
type Summary struct {
	ID                model.ID[Capture]
	OperationID       model.ID[Operation]
	Attempt           int16
	Retried           bool
	StartedAt         temporal.DateTime
	Client            string
	Method            string
	Host              string
	Path              string
	Status            value.Nullable[int16]
	Result            string
	HeadersMS         value.Nullable[int64]
	DurationMS        int64
	RequestID         value.Nullable[string]
	BodyMode          string
	RequestSentBytes  int64
	ResponseReadBytes int64
	DroppedBefore     int64
}

// Format prints a fixed label so captured data never reaches diagnostics.
func (Summary) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("outbound capture summary [redacted]"))
}
