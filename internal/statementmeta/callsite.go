package statementmeta

import (
	"context"
	"runtime"
	"strconv"
	"strings"
)

type siteKey struct{}

// CaptureSite preserves the caller across a framework-owned goroutine boundary.
// Immediate external frames still take precedence when statements execute.
func CaptureSite(ctx context.Context) context.Context {
	if !Active(ctx) {
		return ctx
	}
	return context.WithValue(ctx, siteKey{}, CallSite(ctx))
}
func fallbackSite(ctx context.Context) string {
	if site, _ := ctx.Value(siteKey{}).(string); site != "" {
		return site
	}
	return "unavailable:0"
}
func CallSite(ctx context.Context) string {
	var pcs [64]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		fn := frame.Function
		if fn != "" && !strings.HasPrefix(fn, "github.com/weiloon1234/Foundry-Go/") && !strings.HasPrefix(fn, "github.com/weiloon1234/Foundry-Go.") && !strings.HasPrefix(fn, "runtime.") && !strings.HasPrefix(fn, "testing.") {
			if len(frame.File) <= 1024 {
				return frame.File + ":" + strconv.Itoa(frame.Line)
			}
			return fallbackSite(ctx)
		}
		if !more {
			break
		}
	}
	return fallbackSite(ctx)
}
