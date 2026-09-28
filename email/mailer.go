package email

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type Config struct {
	// From supplies Mailer.Message; explicit NewMessage senders are never replaced.
	From                                           Address
	MaxActive, MaxMessageBytes, MaxAttachmentBytes int
	Timeout                                        time.Duration
}

func DefaultConfig() Config {
	return Config{MaxActive: 16, MaxMessageBytes: 10 << 20, MaxAttachmentBytes: 5 << 20, Timeout: 30 * time.Second}
}
func (c Config) Validate() error {
	if c.From != (Address{}) && c.From.Validate() != nil {
		return Construction
	}
	if c.MaxActive < 1 || c.MaxActive > 1024 || c.MaxMessageBytes < 1024 || c.MaxMessageBytes > 32<<20 || c.MaxAttachmentBytes < 1 || c.MaxAttachmentBytes > c.MaxMessageBytes || c.Timeout <= 0 || c.Timeout > 10*time.Minute {
		return Construction
	}
	return nil
}

type Stage string

const (
	Prepared Stage = "prepared"
	Finished Stage = "finished"
)

// Notice deliberately excludes addresses, subject, body, attachment names,
// message IDs and keys. Finished observers cannot undo provider acceptance.
type Notice struct {
	Stage             Stage
	Recipients, Bytes int
	Accepted          bool
	Failure           Kind
	Duration          time.Duration
}
type Observer func(context.Context, Notice) error
type Diagnostics struct {
	Active                                    int
	Closing                                   bool
	Calls, Accepted, Failed, ObserverFailures uint64
}
type Mailer struct {
	driver      Driver
	storage     *storage.Registry
	config      Config
	observer    Observer
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	diagnostics Diagnostics
	done        chan struct{}
}
type sendFrame struct {
	mailer *Mailer
	parent *sendFrame
	active atomic.Bool
}
type sendKey struct{}

// New borrows the transport and storage registry. It performs no I/O.
func New(driver Driver, disks *storage.Registry, config Config, observer Observer) (*Mailer, error) {
	if config.Validate() != nil || driver == nil {
		return nil, Construction
	}
	v := reflect.ValueOf(driver)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan:
		if v.IsNil() {
			return nil, Construction
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Mailer{driver: driver, storage: disks, config: config, observer: observer, ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
}
func (m *Mailer) begin(ctx context.Context) (context.Context, func(), error) {
	if m == nil || m.done == nil || ctx == nil {
		return nil, nil, Construction
	}
	if ctx.Err() != nil {
		return nil, nil, Transient
	}
	m.mu.Lock()
	if m.diagnostics.Closing || m.diagnostics.Active >= m.config.MaxActive {
		m.mu.Unlock()
		return nil, nil, Transient
	}
	m.diagnostics.Active++
	m.diagnostics.Calls++
	m.mu.Unlock()
	ctx, unlink := contextlink.Link(ctx, m.ctx)
	ctx, cancel := context.WithTimeout(ctx, m.config.Timeout)
	parent, _ := ctx.Value(sendKey{}).(*sendFrame)
	frame := &sendFrame{mailer: m, parent: parent}
	frame.active.Store(true)
	ctx = context.WithValue(ctx, sendKey{}, frame)
	return ctx, func() {
		frame.active.Store(false)
		cancel()
		unlink()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.diagnostics.Active--
		if m.diagnostics.Closing && m.diagnostics.Active == 0 {
			close(m.done)
		}
	}, nil
}

// Send waits for actual completion even when an extension ignores cancellation.
// A non-nil error never claims known acceptance; an accepted result remains
// accepted if a Finished observer fails or the context expires afterward.
func (m *Mailer) Send(ctx context.Context, message Message, options SendOptions) (result Result, err error) {
	ctx, release, err := m.begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	started := time.Now()
	notice := Notice{Recipients: message.RecipientCount()}
	defer func() {
		notice.Stage = Finished
		notice.Accepted = result.Accepted
		notice.Failure = Classification(err)
		notice.Duration = time.Since(started)
		if !m.observe(ctx, notice) {
			result.ObserverFailed = true
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if result.Accepted {
			m.diagnostics.Accepted++
		} else {
			m.diagnostics.Failed++
		}
		if result.ObserverFailed {
			m.diagnostics.ObserverFailures++
		}
	}()
	var outbound Outbound
	failed := callback.Isolated("prepare email", func() error { var e error; outbound, e = m.prepare(ctx, message, options); return e })
	if failed != nil {
		kind := Classification(failed)
		if kind == Ambiguous {
			kind = Construction
		}
		return Result{}, kind
	}
	notice.Stage = Prepared
	notice.Bytes = outbound.Size()
	if !m.observe(ctx, notice) {
		return Result{ObserverFailed: true}, Construction
	}
	if ctx.Err() != nil {
		return Result{}, Transient
	}
	var receipt Receipt
	var sendErr error
	failed = callback.Isolated("submit email", func() error { receipt, sendErr = m.driver.Send(ctx, outbound); return nil })
	if failed != nil {
		return Result{}, Ambiguous
	}
	if sendErr != nil {
		return Result{}, Classification(sendErr)
	}
	if receipt.Validate() != nil {
		return Result{}, Ambiguous
	}
	return Result{Receipt: receipt, Accepted: true}, nil
}
func (m *Mailer) observe(ctx context.Context, n Notice) bool {
	if m.observer == nil {
		return true
	}
	return callback.Isolated("observe email", func() error { return m.observer(ctx, n) }) == nil
}
func (m *Mailer) prepare(ctx context.Context, message Message, options SendOptions) (Outbound, error) {
	if message.Validate() != nil || options.IdempotencyKey.Validate() != nil || len(message.text)+len(message.html) > m.config.MaxMessageBytes {
		return Outbound{}, Construction
	}
	if ctx.Err() != nil {
		return Outbound{}, Transient
	}
	attachments := make([]ResolvedAttachment, 0, len(message.attachments))
	remaining := m.config.MaxMessageBytes
	for _, reference := range message.attachments {
		if m.storage == nil {
			return Outbound{}, Construction
		}
		disk, err := m.storage.Disk(reference.Disk)
		if err != nil {
			return Outbound{}, Construction
		}
		attachment, err := loadAttachment(ctx, disk, reference, min(m.config.MaxAttachmentBytes, remaining))
		if err != nil {
			return Outbound{}, err
		}
		remaining -= len(attachment.data)
		attachments = append(attachments, attachment)
	}
	wire, err := buildMIME(message, attachments, m.config.MaxMessageBytes)
	if err != nil {
		return Outbound{}, Construction
	}
	return Outbound{message: message, attachments: attachments, mime: wire, key: options.IdempotencyKey}, nil
}
func loadAttachment(ctx context.Context, disk *storage.Disk, reference Attachment, limit int) (result ResolvedAttachment, err error) {
	body, info, err := disk.Open(ctx, reference.Key, storage.ReadOptions{Version: reference.Version, IfMatch: reference.IfMatch})
	if body != nil {
		defer func() {
			if closeErr := body.Close(); closeErr != nil && err == nil {
				result = ResolvedAttachment{}
				err = Transient
			}
		}()
	}
	if err != nil {
		return result, attachmentFailure(err)
	}
	if body == nil || info.Length < 0 || info.Length > int64(limit) {
		return result, Construction
	}
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return result, attachmentFailure(err)
	}
	if len(data) > limit || int64(len(data)) != info.Length {
		return result, Construction
	}
	if ctx.Err() != nil {
		return result, Transient
	}
	return ResolvedAttachment{reference: reference, data: data}, nil
}

// Called inside the owned preparation callback; custom error methods cannot
// escape its panic/Goexit boundary or release capacity before they return.
func attachmentFailure(err error) error {
	result := Transient
	complete := errorgraph.Walk(err, func(current error) bool {
		for _, code := range []storage.Code{storage.NotFound, storage.Forbidden, storage.Unsupported, storage.PreconditionFailed, storage.Invalid, storage.IntegrityFailed, storage.LimitExceeded} {
			if errorgraph.Matches(current, code) {
				result = Construction
				return false
			}
		}
		return true
	})
	if !complete {
		return Construction
	}
	return result
}
func (m *Mailer) Snapshot() Diagnostics {
	if m == nil {
		return Diagnostics{Closing: true}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.diagnostics
}
func (m *Mailer) Done() <-chan struct{} {
	if m == nil || m.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return m.done
}

// Close cancels admission and active I/O. Its context bounds only this caller's
// wait; Done remains open until every callback really exits. Borrowed resources
// are not closed. Calling Close from an active send context returns fault.Cycle.
func (m *Mailer) Close(ctx context.Context) error {
	if m == nil || m.done == nil || ctx == nil {
		return Construction
	}
	for frame, _ := ctx.Value(sendKey{}).(*sendFrame); frame != nil; frame = frame.parent {
		if frame.mailer == m && frame.active.Load() {
			return fault.New(fault.Cycle, "email callback cannot wait for itself")
		}
	}
	m.mu.Lock()
	if !m.diagnostics.Closing {
		m.diagnostics.Closing = true
		m.cancel()
		if m.diagnostics.Active == 0 {
			close(m.done)
		}
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		return nil
	default:
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (*Mailer) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email mailer")) }
func (*Mailer) LogValue() slog.Value       { return slog.StringValue("email mailer") }

// Message starts an immutable message using this mailer's configured sender.
// An unconfigured sender produces a message rejected by Send's normal validation.
func (m *Mailer) Message(subject string, to ...Address) Message {
	if m == nil {
		return NewMessage(Address{}, subject, to...)
	}
	return NewMessage(m.config.From, subject, to...)
}
