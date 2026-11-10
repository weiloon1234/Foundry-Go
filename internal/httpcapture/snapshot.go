package httpcapture

import (
	"net/netip"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Field struct {
	Name   string
	Values []string
}

type Metadata struct {
	RequestID                                                       attribution.RequestID
	ReceivedAt                                                      time.Time
	Prefix                                                          string
	Full                                                            bool
	Method                                                          string
	EscapedPath                                                     string
	RawQuery                                                        string
	ClientIP                                                        netip.Addr
	Headers                                                         []Field
	ContentType                                                     string
	ContentEncoding                                                 string
	DeclaredBytes                                                   value.Nullable[int64]
	PathTruncated, QueryTruncated, HeadersTruncated                 bool
	MethodTruncated, ContentTypeTruncated, ContentEncodingTruncated bool
}

type BodyEnd uint8

const (
	NotRead BodyEnd = iota
	EOF
	OverCaptureLimit
	SourceFailure
)

type ReadFailure string

const (
	ReadFailed       ReadFailure = "read_failed"
	ReadTimeout      ReadFailure = "read_timeout"
	ClientDisconnect ReadFailure = "client_disconnect"
	BodyLimit        ReadFailure = "body_limit"
)

type Body struct {
	Prefix        []byte // raw, at most cap+1; a later owner applies disclosure policy
	End           BodyEnd
	ObservedBytes int64                 // counts source reads, never replay reads
	MeasuredBytes value.Nullable[int64] // known only after observed EOF
	ReadFailure   ReadFailure
}

type Completion struct {
	Route                                 string
	Result                                observability.Result
	Status                                value.Nullable[uint16] // actual committed status; absent on precommit abort/hijack
	ResponseHead                          []byte
	ResponseHeaders                       []Field // the handler's header map at commit, before net/http adds transport headers
	ResponseContentType, ResponseEncoding string
	ResponseMetadataTruncated             bool
	ResponseTruncated                     bool
	ResponseBytes                         int64 // accepted by the HTTP writer, not proof of delivery
	Hijacked                              bool
	Aborted                               bool
	WriteFailed                           bool
	Duration                              time.Duration
}

type Snapshot struct {
	Metadata      Metadata
	Body          Body
	Completion    Completion
	DroppedBefore uint64
}
