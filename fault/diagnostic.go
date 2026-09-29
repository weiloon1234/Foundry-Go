package fault

import (
	"log/slog"
	"slices"
	"strconv"
)

// Frame is one source location of a contained panic. It never carries argument
// values, locals or the recovered panic value.
type Frame struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

func (f Frame) String() string { return f.Function + " " + f.File + ":" + strconv.Itoa(f.Line) }

// Note is one framework fault found in a failure's error graph. Messages come
// from framework-owned fault constructors, which never format causes.
type Note struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

// Attribute is a framework-owned, redaction-safe classification such as a
// PostgreSQL SQLSTATE or transport error code.
type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Diagnostic is a bounded, redacted failure summary that is safe to log and to
// export to error reporters. It contains dynamic Go type names, framework fault
// notes, framework attributes and contained-panic frames. It never contains
// arbitrary Error() text, payloads, credentials or recovered panic values.
type Diagnostic struct {
	Types      []string    `json:"types,omitempty"`
	Faults     []Note      `json:"faults,omitempty"`
	Attributes []Attribute `json:"attributes,omitempty"`
	Frames     []Frame     `json:"frames,omitempty"`
	// Truncated reports that bounds or a failing error method ended inspection.
	Truncated bool `json:"truncated,omitempty"`
}

// IsZero reports whether no failure information was captured.
func (d Diagnostic) IsZero() bool {
	return len(d.Types) == 0 && len(d.Faults) == 0 && len(d.Attributes) == 0 && len(d.Frames) == 0 && !d.Truncated
}

// Clone returns an owned copy whose slices do not alias d.
func (d Diagnostic) Clone() Diagnostic {
	return Diagnostic{Types: slices.Clone(d.Types), Faults: slices.Clone(d.Faults), Attributes: slices.Clone(d.Attributes), Frames: slices.Clone(d.Frames), Truncated: d.Truncated}
}

// LogValue renders the diagnostic as a structured slog group.
func (d Diagnostic) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, 5)
	if len(d.Types) != 0 {
		attrs = append(attrs, slog.Any("types", slices.Clone(d.Types)))
	}
	if len(d.Faults) != 0 {
		notes := make([]string, len(d.Faults))
		for i, note := range d.Faults {
			notes[i] = string(note.Code) + ": " + note.Message
		}
		attrs = append(attrs, slog.Any("faults", notes))
	}
	for _, attribute := range d.Attributes {
		attrs = append(attrs, slog.String(attribute.Key, attribute.Value))
	}
	if len(d.Frames) != 0 {
		attrs = append(attrs, slog.Any("frames", framesValue(d.Frames)))
	}
	if d.Truncated {
		attrs = append(attrs, slog.Bool("truncated", true))
	}
	return slog.GroupValue(attrs...)
}

func framesValue(frames []Frame) []string {
	rendered := make([]string, len(frames))
	for i, frame := range frames {
		rendered[i] = frame.String()
	}
	return rendered
}
