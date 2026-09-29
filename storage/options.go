package storage

import (
	"math"
	"time"

	"github.com/weiloon1234/Foundry-Go/value"
)

// ByteRange requests up to Length bytes from Offset. The returned range is
// clipped at EOF. An empty object or starting beyond EOF is unsatisfiable.
type ByteRange struct{ Offset, Length int64 }

func (r ByteRange) Validate() error {
	if r.Offset < 0 || r.Length <= 0 || r.Offset > math.MaxInt64-r.Length {
		return Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	return nil
}
func (r ByteRange) Resolve(size int64) (offset, length int64, err error) {
	if err = r.Validate(); err != nil {
		return
	}
	if size <= 0 || r.Offset >= size {
		return 0, 0, Failure(RangeNotSatisfiable, OpenOperation, NotApplicable, nil)
	}
	return r.Offset, min(r.Length, size-r.Offset), nil
}

type WriteCondition struct {
	absent bool
	match  ETag
}

func IfAbsent() WriteCondition { return WriteCondition{absent: true} }
func IfMatch(tag ETag) (WriteCondition, error) {
	if tag == "" {
		return WriteCondition{}, Failure(Invalid, PutOperation, Unchanged, nil)
	}
	if err := tag.Validate(); err != nil {
		return WriteCondition{}, err
	}
	return WriteCondition{match: tag}, nil
}
func (c WriteCondition) RequiresAbsence() bool { return c.absent }
func (c WriteCondition) Match() ETag           { return c.match }

type PutOptions struct {
	ContentType MediaType
	Size        value.Optional[int64]
	Checksum    value.Optional[SHA256]
	Condition   WriteCondition
	// Metadata is optional provider object metadata. Adapters without
	// Capabilities.ObjectMetadata reject a nonzero value before consuming input.
	Metadata ObjectMetadata
}

func (o PutOptions) Validate(maximum int64) error {
	if o.ContentType != "" {
		if err := o.ContentType.Validate(); err != nil {
			return err
		}
	}
	if size, present := o.Size.Get(); present && (size < 0 || size > maximum) {
		return Failure(LimitExceeded, PutOperation, Unchanged, nil)
	}
	if err := o.Metadata.Validate(); err != nil {
		return err
	}
	return o.Condition.match.Validate()
}

type ReadOptions struct {
	Range   value.Optional[ByteRange]
	IfMatch ETag
	Version VersionID
}

func (o ReadOptions) Validate() error {
	if r, ok := o.Range.Get(); ok {
		if err := r.Validate(); err != nil {
			return err
		}
	}
	if err := o.IfMatch.Validate(); err != nil {
		return err
	}
	return o.Version.Validate()
}

type DeleteOptions struct {
	IfMatch ETag
	Version VersionID
}

func (o DeleteOptions) Validate() error {
	if err := o.IfMatch.Validate(); err != nil {
		return err
	}
	return o.Version.Validate()
}

// Cursor is an opaque provider continuation. Copy it only from the returned
// page; it is tied to that disk/prefix, and never grants object authorization.
type Cursor struct{ text string }

func NewCursor(text string) Cursor { return Cursor{text: text} }
func (c Cursor) Token() string     { return c.text }
func (c Cursor) IsZero() bool      { return c.text == "" }

const MaxPageSize = 1000
const MaxCursorBytes = 8192

// ListOptions selects one bounded page. Delimited lists a single level below
// Prefix: objects without a further "/" after Prefix and each distinct child
// prefix ending in "/" (reported once in Page.Directories). It requires
// Capabilities.DelimitedList. Limit bounds objects and directories together.
type ListOptions struct {
	Prefix    Prefix
	Cursor    Cursor
	Limit     int
	Delimited bool
}

func (o ListOptions) Validate() error {
	if err := o.Prefix.Validate(); err != nil {
		return err
	}
	if o.Limit < 1 || o.Limit > MaxPageSize || len(o.Cursor.text) > MaxCursorBytes {
		return Failure(Invalid, ListOperation, NotApplicable, nil)
	}
	return nil
}

// Page is one listing page. Listed objects carry key, size, modification time,
// ETag and version from the provider listing; ContentType and Checksum are
// zero when the listing does not report them (S3). Use Stat for complete
// metadata. Skipped counts provider entries under the prefix that are not
// representable framework objects (foreign or unparsable keys, entries beyond
// the object size limit, entries without validators); they never fail a page.
type Page struct {
	Objects     []ObjectInfo
	Directories []Prefix
	Skipped     int
	Next        Cursor
}

// Config bounds one disk. MaxActive and Timeout apply to metadata and write
// operations (Put, Stat, Delete, List, signing) and to opening a read stream.
// An open read stream uses a separate MaxStreams pool and remains open while it
// makes progress: StreamIdleTimeout cancels it only after no read progress for
// that long, so a slow client download is neither cut off by Timeout nor able to
// exhaust write capacity. Capacity waits are queued (at most min(Timeout, 5s))
// and then fail as retryable overload (fault.Overloaded). Zero MaxStreams or
// StreamIdleTimeout selects its default.
type Config struct {
	Visibility        Visibility
	MaxObjectBytes    int64
	MaxActive         int
	Timeout           time.Duration
	MaxStreams        int
	StreamIdleTimeout time.Duration
}

const (
	DefaultMaxStreams        = 256
	DefaultStreamIdleTimeout = 2 * time.Minute
)

func DefaultConfig() Config {
	return Config{MaxObjectBytes: 1 << 30, MaxActive: 32, Timeout: 5 * time.Minute, MaxStreams: DefaultMaxStreams, StreamIdleTimeout: DefaultStreamIdleTimeout}
}
func (c Config) Validate() error {
	if c.Visibility > Public || c.MaxObjectBytes <= 0 || c.MaxObjectBytes > 1<<50 || c.MaxActive < 1 || c.MaxActive > 4096 || c.Timeout <= 0 || c.Timeout > 24*time.Hour || c.MaxStreams < 0 || c.MaxStreams > 16384 || c.StreamIdleTimeout < 0 || c.StreamIdleTimeout > 24*time.Hour {
		return Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	return nil
}
func (c Config) normalized() Config {
	if c.MaxStreams == 0 {
		c.MaxStreams = DefaultMaxStreams
	}
	if c.StreamIdleTimeout == 0 {
		c.StreamIdleTimeout = DefaultStreamIdleTimeout
	}
	return c
}
