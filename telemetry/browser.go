package telemetry

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// BrowserCode is an allowlisted exception class; exception messages are absent.
//
//foundry:enum
type BrowserCode string

const (
	BrowserCodeError              BrowserCode = "Error"
	BrowserCodeTypeError          BrowserCode = "TypeError"
	BrowserCodeRangeError         BrowserCode = "RangeError"
	BrowserCodeReferenceError     BrowserCode = "ReferenceError"
	BrowserCodeSyntaxError        BrowserCode = "SyntaxError"
	BrowserCodeURIError           BrowserCode = "URIError"
	BrowserCodeEvalError          BrowserCode = "EvalError"
	BrowserCodeUnhandledRejection BrowserCode = "UnhandledRejection"
	BrowserCodeReactBoundary      BrowserCode = "ReactBoundary"
)

// BrowserFrame is a location in a portal's deployed JavaScript asset. File must
// be exactly portal/_assets/basename.js, with no URL, encoding or traversal.
type BrowserFrame struct {
	// File is the asset path, exactly portal/_assets/basename.js, where portal is the report's portal and
	// basename is a bounded identity ending in .js.
	File string
	// Line is the line in File. It must be 1..1,000,000.
	Line int
	// Column is the column in File. It must be 0..1,000,000.
	Column int
}

// BrowserError supplies only an allowlisted portal, current release, class and
// at most twenty frames. It carries no browser messages, URLs or payloads.
type BrowserError struct {
	// Portal names the reporting portal. It must be one of the collector's configured browser portals.
	Portal Portal
	// Release names the build that reported. It must equal the collector's configured release.
	Release Release
	// Code is the exception class. It must be a known BrowserCode.
	Code BrowserCode
	// Frames locates the error in the portal's assets. At most twenty are accepted.
	Frames []BrowserFrame
}

// RecordBrowser validates and snapshots an untrusted browser observation into
// memory. Disabled/invalid intake returns a safe validation error; capacity loss
// returns nil and increments Dropped. Success is not a persistence receipt.
func (c *Collector) RecordBrowser(_ context.Context, report BrowserError) error {
	if !c.beginObservation() {
		return nil
	}
	if !c.config.Browser.Enabled || !slices.Contains(c.config.Browser.Portals, report.Portal) || report.Release != c.config.Release || len(report.Frames) > maxFrames {
		return invalid("invalid telemetry browser report")
	}
	if !report.Code.IsValid() {
		return invalid("invalid telemetry browser class")
	}
	frames := make([]Frame, 0, len(report.Frames))
	prefix := string(report.Portal) + "/_assets/"
	for _, frame := range report.Frames {
		name, ok := strings.CutPrefix(frame.File, prefix)
		if !ok || !validIdentity(name) || !strings.HasSuffix(name, ".js") || frame.Line < 1 || frame.Line > maxFramePosition || frame.Column < 0 || frame.Column > maxFramePosition {
			return invalid("invalid telemetry browser frame")
		}
		frames = append(frames, Frame{File: strings.Clone(frame.File), Line: frame.Line, Column: frame.Column})
	}
	now, err := temporal.Now(c.clock)
	if err != nil {
		c.drop(1)
		return err
	}
	c.add(observedIssue{Source: SourceBrowser, Code: string(report.Code), Route: http.RouteID("browser." + string(report.Portal)), Release: c.config.Release, At: now, Detail: Detail{Frames: frames, Untrusted: true}})
	return nil
}
