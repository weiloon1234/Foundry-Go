package database

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"sort"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
	"github.com/weiloon1234/Foundry-Go/internal/statementshape"
)

// StatementDetectorConfig bounds one test/request scope. Threshold is per shape.
// Example: c := database.DefaultStatementDetectorConfig().
type StatementDetectorConfig struct{ Threshold, MaxShapes int }

// DefaultStatementDetectorConfig permits ten executions per shape and 128 shapes
// per scope. Example: limits := database.DefaultStatementDetectorConfig().
func DefaultStatementDetectorConfig() StatementDetectorConfig {
	return StatementDetectorConfig{10, 128}
}

// StatementDetector is an immutable, injected observer; scopes own all counters.
// Raw SQL is identified only by a private per-instance keyed fingerprint.
type StatementDetector struct {
	config StatementDetectorConfig
	key    [32]byte
}

// NewStatementDetector validates bounds and creates a private fingerprint key.
// Example: detector, err := database.NewStatementDetector(limits).
func NewStatementDetector(c StatementDetectorConfig) (*StatementDetector, error) {
	if c.Threshold < 1 || c.Threshold > 1000000 || c.MaxShapes < 1 || c.MaxShapes > 1024 {
		return nil, safeerror.NewFault(fault.Invalid, "invalid statement detector limits")
	}
	d := &StatementDetector{config: c}
	if _, err := rand.Read(d.key[:]); err != nil {
		return nil, safeerror.NewFault(fault.Invalid, "statement detector entropy unavailable")
	}
	return d, nil
}

const maxStatementSites = 4

type statementCount struct {
	count        int
	sites        []string
	siteOverflow bool
}

// StatementScope retains trusted compiler shapes or keyed fingerprints, never raw SQL.
type StatementScope struct {
	mu       sync.Mutex
	config   StatementDetectorConfig
	counts   map[string]*statementCount
	overflow bool
}

// StatementViolation contains only compiler-owned text or an opaque fingerprint,
// counts and up to four source locations. CallSiteOverflow makes truncation explicit.
type StatementViolation struct {
	Shape            string
	Count, Threshold int
	CallSites        []string
	CallSiteOverflow bool
}

// StatementReport is an owned inspection snapshot. Either overflow flag means
// observation is incomplete, even when no retained shape exceeds its threshold.
type StatementReport struct {
	Violations       []StatementViolation
	Overflow         bool
	CallSiteOverflow bool
}

// Begin starts an isolated scope. Nested scopes replace their parent for this
// detector. Example: ctx, scope := detector.Begin(t.Context()).
func (d *StatementDetector) Begin(ctx context.Context) (context.Context, *StatementScope) {
	s := &StatementScope{config: d.config, counts: make(map[string]*statementCount)}
	return context.WithValue(statementmeta.Enable(ctx), d, s), s
}
func (d *StatementDetector) active(ctx context.Context) bool {
	s, _ := ctx.Value(d).(*StatementScope)
	return s != nil
}

// Observe is the raw observer integration. Without compiler-owned internal
// metadata, statement text can only become an opaque keyed fingerprint.
func (d *StatementDetector) Observe(ctx context.Context, event QueryEvent) { d.observe(ctx, event, "") }
func (d *StatementDetector) observe(ctx context.Context, event QueryEvent, site string) {
	s, _ := ctx.Value(d).(*StatementScope)
	if s == nil {
		return
	}
	if site == "" {
		site = statementmeta.CallSite(ctx)
	}
	shape := statementmeta.Shape(ctx, event.Statement)
	if shape == "" || len(shape) > statementshape.MaxBytes {
		shape = "untrusted statement " + d.fingerprint(event.Statement)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	counter, exists := s.counts[shape]
	if !exists {
		if len(s.counts) >= s.config.MaxShapes {
			s.overflow = true
			return
		}
		counter = &statementCount{}
		s.counts[shape] = counter
	}
	if counter.count < int(^uint(0)>>1) {
		counter.count++
	}
	for _, seen := range counter.sites {
		if site == seen {
			return
		}
	}
	if len(counter.sites) == maxStatementSites {
		counter.siteOverflow = true
		return
	}
	counter.sites = append(counter.sites, site)
}
func (d *StatementDetector) fingerprint(sql string) string {
	text, ok := statementshape.Normalize(sql)
	domain := "normalized\x00"
	if !ok {
		text = sql
		domain = "raw\x00"
	}
	h := hmac.New(sha256.New, d.key[:])
	h.Write([]byte(domain))
	// Even the fallback for oversized SQL has bounded temporary memory.
	var block [4096]byte
	for len(text) > 0 {
		n := copy(block[:], text)
		h.Write(block[:n])
		text = text[n:]
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// Report snapshots completed statements; close query rows before inspecting it.
// Example: report := scope.Report().
func (s *StatementScope) Report() StatementReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := StatementReport{Overflow: s.overflow}
	for shape, c := range s.counts {
		r.CallSiteOverflow = r.CallSiteOverflow || c.siteOverflow
		if c.count > s.config.Threshold {
			sites := append([]string(nil), c.sites...)
			sort.Strings(sites)
			r.Violations = append(r.Violations, StatementViolation{Shape: shape, Count: c.count, Threshold: s.config.Threshold, CallSites: sites, CallSiteOverflow: c.siteOverflow})
		}
	}
	sort.Slice(r.Violations, func(i, j int) bool { return r.Violations[i].Shape < r.Violations[j].Shape })
	return r
}

// WithStatementDetector composes detection with the existing query observer.
// Install the same instance used to Begin scopes; omitted means no instrumentation.
// Example: database.WithStatementDetector(detector).
func WithStatementDetector(d *StatementDetector) Option {
	return func(s *poolSettings) error {
		if d == nil || d.config.Threshold < 1 {
			return safeerror.NewFault(fault.Invalid, "statement detector is uninitialized")
		}
		if s.statementDetector != nil {
			return safeerror.NewFault(fault.Invalid, "statement detector already configured")
		}
		s.statementDetector = d
		return nil
	}
}

func (*StatementDetector) String() string   { return "database statement detector" }
func (*StatementDetector) GoString() string { return "database statement detector" }
