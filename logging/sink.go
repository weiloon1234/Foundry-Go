package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type SinkDriver string

const (
	Stderr SinkDriver = "stderr"
	Stdout SinkDriver = "stdout"
	File   SinkDriver = "file"
	// Syslog writes each JSON record to a local or remote syslog daemon with a
	// severity derived from the record level. It is unavailable on Windows/Plan 9.
	Syslog SinkDriver = "syslog"
)

// MaxFallbackBytes bounds one record rewritten to stderr after its configured
// destination failed. Larger records are counted as dropped instead.
const MaxFallbackBytes = 256 << 10

// SinkConfig selects an application-owned JSON destination. File sinks append
// with automatic rotation/retention unless Rotation.Disabled is explicit.
type SinkConfig struct {
	Driver    SinkDriver
	Path      string
	Level     slog.Level
	AddSource bool
	Rotation  RotationConfig
	// TimeZone controls timestamps and daily rotation; empty means UTC for direct
	// sinks and inherits the application timezone during configured assembly.
	TimeZone temporal.ZoneName
	// Async optionally moves destination writes to one owned bounded writer.
	Async AsyncConfig
	// Syslog selects the daemon used by the syslog driver.
	Syslog SyslogConfig
}

// AsyncConfig enables one owned writer per sink. Records are copied into a
// bounded queue; a full queue drops the newest record and counts it rather than
// blocking the logging caller. Close drains every accepted record. Zero limits
// select 1,024 records and 4 MiB of pending record bytes.
type AsyncConfig struct {
	Enabled  bool
	Queue    int
	MaxBytes int64
}

func (c AsyncConfig) resolved() AsyncConfig {
	if c.Queue == 0 {
		c.Queue = 1024
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 4 << 20
	}
	return c
}

func (c AsyncConfig) Validate() error {
	if !c.Enabled {
		if c != (AsyncConfig{}) {
			return fault.New(fault.Invalid, "async logging options require async to be enabled")
		}
		return nil
	}
	c = c.resolved()
	if c.Queue < 1 || c.Queue > 65536 || c.MaxBytes < 1024 || c.MaxBytes > 256<<20 {
		return fault.New(fault.Invalid, "async logging queue limits must be bounded and positive")
	}
	return nil
}

func DefaultSinkConfig() SinkConfig { return SinkConfig{Driver: Stderr} }
func (c SinkConfig) Validate() error {
	if _, err := c.location(); err != nil {
		return err
	}
	if c.Level < slog.LevelDebug || c.Level > slog.LevelError {
		return fault.New(fault.Invalid, "invalid logging level")
	}
	if err := c.Async.Validate(); err != nil {
		return err
	}
	if c.Driver != Syslog && c.Syslog != (SyslogConfig{}) {
		return fault.New(fault.Invalid, "syslog options require the syslog driver")
	}
	switch c.Driver {
	case Stderr, Stdout:
		if c.Path != "" || c.Rotation != (RotationConfig{}) {
			return fault.New(fault.Invalid, "stream logging cannot select file options")
		}
	case File:
		if !filepath.IsAbs(c.Path) || strings.ContainsRune(c.Path, 0) {
			return fault.New(fault.Invalid, "log file requires an absolute path")
		}
		return c.Rotation.Validate()
	case Syslog:
		if c.Path != "" || c.Rotation != (RotationConfig{}) {
			return fault.New(fault.Invalid, "syslog logging cannot select file options")
		}
		if !syslogSupported {
			return fault.New(fault.Invalid, "syslog logging is unsupported on this platform")
		}
		return c.Syslog.Validate()
	default:
		return fault.New(fault.Invalid, "unsupported logging sink")
	}
	return nil
}

func (c SinkConfig) location() (*time.Location, error) {
	zone := c.TimeZone
	if zone == "" {
		zone = temporal.UTC
	}
	return zone.Location()
}

// SinkStats is an owned snapshot of one destination's delivery counters.
// Records counts records the destination accepted. Failures counts rejected
// destination writes; each such record was either rewritten to stderr
// (Fallbacks) or lost (Dropped). Dropped also counts records refused by a full
// async queue. Queued is the current async backlog. Rotation and retention
// failures never drop a record; they are counted separately.
type SinkStats struct {
	Driver           SinkDriver `json:"driver"`
	Records          uint64     `json:"records"`
	Failures         uint64     `json:"failures"`
	Fallbacks        uint64     `json:"fallbacks"`
	Dropped          uint64     `json:"dropped"`
	Queued           int        `json:"queued"`
	RotationFailures uint64     `json:"rotation_failures"`
	PruneFailures    uint64     `json:"prune_failures"`
}

// Sink is a prepared writer; it acquires its destination only in Start. Copying
// the handle retains one owner. It never closes process stdout/stderr.
type Sink struct{ state *sinkState }
type sinkState struct {
	mu       sync.Mutex
	config   SinkConfig
	target   destination
	file     io.Closer
	closed   bool
	location *time.Location
	events   *sinkEvents
	// fallback receives a failed record once. Stderr sinks have no fallback.
	fallback io.Writer
	// Async state: queue is owned by Write/Close under mu and consumed by one
	// writer goroutine that never takes mu.
	queue       chan asyncRecord
	queuedBytes atomic.Int64
	writerDone  chan struct{}
}

type asyncRecord struct {
	data    []byte
	level   slog.Level
	barrier chan struct{}
}

// destination writes one complete record. Severity-aware destinations use the
// record level; byte streams ignore it.
type destination interface {
	writeRecord([]byte, slog.Level) (int, error)
}
type streamDestination struct{ writer io.Writer }

func (d streamDestination) writeRecord(data []byte, _ slog.Level) (int, error) {
	return d.writer.Write(data)
}

func PrepareSink(c SinkConfig) (*Sink, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	location, err := c.location()
	if err != nil {
		return nil, err
	}
	state := &sinkState{config: c, location: location, events: newSinkEvents(c.Driver, os.Stderr)}
	if c.Driver != Stderr {
		state.fallback = os.Stderr
	}
	return &Sink{state: state}, nil
}
func (s *Sink) Logger() *slog.Logger {
	if s == nil || s.state == nil {
		return nil
	}
	options := Options{Level: s.state.config.Level, AddSource: s.state.config.AddSource, Location: s.state.location}
	if s.state.config.Driver == Syslog {
		return slog.New(Correlate(newSeverityHandler(s, options)))
	}
	return JSON(s, options)
}
func (s *Sink) Start(ctx context.Context) error {
	if s == nil || s.state == nil || ctx == nil {
		return fault.New(fault.Invalid, "logging sink requires an owner and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return fault.New(fault.Closed, "logging sink is closed")
	}
	if state.target != nil {
		return nil
	}
	if err := state.open(); err != nil {
		return err
	}
	state.startAsync()
	return nil
}

// startAsync starts the owned writer for an opened async sink. The caller
// holds mu.
func (state *sinkState) startAsync() {
	if !state.config.Async.Enabled {
		return
	}
	async := state.config.Async.resolved()
	state.queue = make(chan asyncRecord, async.Queue)
	state.writerDone = make(chan struct{})
	go state.runAsync(state.target, state.queue)
}

// open acquires the configured destination. The caller holds mu.
func (state *sinkState) open() error {
	switch state.config.Driver {
	case Stderr:
		state.target = streamDestination{os.Stderr}
	case Stdout:
		state.target = streamDestination{os.Stdout}
	case Syslog:
		connection, err := openSyslog(state.config.Syslog)
		if err != nil {
			return err
		}
		state.target, state.file = connection, connection
	case File:
		if !state.config.Rotation.Disabled {
			file, err := openRotatingFile(state.config.Path, state.config.Rotation, time.Now(), state.location, state.events)
			if err != nil {
				return err
			}
			state.target, state.file = streamDestination{file}, file
			return nil
		}
		if info, err := os.Lstat(state.config.Path); err == nil {
			if !info.Mode().IsRegular() {
				return fault.New(fault.Invalid, "log destination must be a regular file")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fault.Wrap(fault.Invalid, "cannot inspect log destination", err)
		}
		file, err := os.OpenFile(state.config.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fault.Wrap(fault.Invalid, "cannot open log destination", err)
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return errors.Join(fault.New(fault.Invalid, "invalid opened log destination"), err, file.Close())
		}
		state.target = streamDestination{file}
		state.file = file
	}
	return nil
}

// Write accepts one complete record. Handlers created by Logger call it once
// per record; direct callers must also supply whole records.
func (s *Sink) Write(data []byte) (int, error) { return s.writeRecord(data, slog.LevelInfo) }

func (s *Sink) writeRecord(data []byte, level slog.Level) (int, error) {
	if s == nil || s.state == nil {
		return 0, io.ErrClosedPipe
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed || state.target == nil {
		return 0, io.ErrClosedPipe
	}
	if len(data) == 0 {
		return 0, nil
	}
	if state.queue == nil {
		return state.deliver(state.target, data, level)
	}
	// Only Write sends, under mu, so a length check makes the send non-blocking.
	async := state.config.Async.resolved()
	if len(state.queue) == cap(state.queue) || int64(len(data)) > async.MaxBytes-state.queuedBytes.Load() {
		state.events.dropped.Add(1)
		state.events.notice(noticeDropped, nil)
		return 0, fault.New(fault.Overloaded, "log queue is full; record dropped")
	}
	state.queuedBytes.Add(int64(len(data)))
	state.queue <- asyncRecord{data: slices.Clone(data), level: level}
	return len(data), nil
}

// deliver writes one record and applies the bounded stderr fallback. It never
// takes mu, so the async writer can use it while Write callers keep admitting.
func (state *sinkState) deliver(target destination, data []byte, level slog.Level) (int, error) {
	n, err := target.writeRecord(data, level)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		state.events.records.Add(1)
		return n, nil
	}
	state.events.failures.Add(1)
	state.events.notice(noticeWrite, err)
	if state.fallback != nil && len(data) <= MaxFallbackBytes {
		if written, fallbackErr := state.fallback.Write(data); fallbackErr == nil && written == len(data) {
			state.events.fallbacks.Add(1)
			return n, err
		}
	}
	state.events.dropped.Add(1)
	return n, err
}

func (state *sinkState) runAsync(target destination, queue <-chan asyncRecord) {
	defer close(state.writerDone)
	for record := range queue {
		if record.barrier != nil {
			close(record.barrier)
			continue
		}
		_, _ = state.deliver(target, record.data, record.level)
		state.queuedBytes.Add(-int64(len(record.data)))
	}
}

// Stats returns the sink's delivery counters. It is safe during concurrent
// writes and after Close; a prepared, never-started sink reports zeros.
func (s *Sink) Stats() SinkStats {
	if s == nil || s.state == nil {
		return SinkStats{}
	}
	state := s.state
	stats := state.events.snapshot()
	state.mu.Lock()
	if state.queue != nil {
		stats.Queued = len(state.queue)
	}
	state.mu.Unlock()
	return stats
}

// Close drains accepted async records, stops owned cleanup work and releases
// the destination. Stream sinks never close process stdout/stderr.
func (s *Sink) Close() error {
	if s == nil || s.state == nil {
		return nil
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return nil
	}
	state.closed = true
	if state.queue != nil {
		close(state.queue)
		<-state.writerDone
		state.queue = nil
	}
	state.target = nil
	if state.file != nil {
		if err := state.file.Close(); err != nil {
			return fault.Wrap(fault.Internal, "cannot close log destination", err)
		}
	}
	return nil
}
