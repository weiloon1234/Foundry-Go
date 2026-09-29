package diagnostics

import (
	stdhttp "net/http"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Profile endpoint query parameters. name selects heap (default), goroutine,
// allocs, block, mutex, threadcreate, cpu or trace. seconds (1-20, default 5)
// bounds cpu and trace capture. debug=1 selects the text form of named
// profiles. One profile runs at a time per Runtime; others receive 503.
const (
	MaxProfileSeconds     = 20
	DefaultProfileSeconds = 5
)

var namedProfiles = map[string]bool{"heap": true, "goroutine": true, "allocs": true, "block": true, "mutex": true, "threadcreate": true}

// serveProfile uses runtime/pprof directly. Importing net/http/pprof would
// register handlers on http.DefaultServeMux, so it is deliberately not used.
func (r *Runtime) serveProfile(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
	query := request.URL.Query()
	name, seconds, debug := "heap", DefaultProfileSeconds, 0
	for key, values := range query {
		if len(values) != 1 {
			_ = foundryhttp.WriteError(writer, request, foundryhttp.BadRequest)
			return
		}
		var err error
		switch key {
		case "name":
			name = values[0]
		case "seconds":
			seconds, err = strconv.Atoi(values[0])
			if err == nil && (seconds < 1 || seconds > MaxProfileSeconds) {
				err = fault.New(fault.Invalid, "profile duration is out of range")
			}
		case "debug":
			debug, err = strconv.Atoi(values[0])
			if err == nil && debug != 0 && debug != 1 {
				err = fault.New(fault.Invalid, "profile debug level is out of range")
			}
		default:
			err = fault.New(fault.Invalid, "unknown profile parameter")
		}
		if err != nil {
			_ = foundryhttp.WriteError(writer, request, foundryhttp.BadRequest)
			return
		}
	}
	if name != "cpu" && name != "trace" && !namedProfiles[name] {
		_ = foundryhttp.WriteError(writer, request, foundryhttp.BadRequest)
		return
	}
	select {
	case r.profile <- struct{}{}:
		defer func() { <-r.profile }()
	default:
		_ = foundryhttp.WriteError(writer, request, fault.New(fault.Overloaded, "a diagnostics profile is already running"))
		return
	}
	header := writer.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", `attachment; filename="`+name+`.pprof"`)
	if debug == 1 && namedProfiles[name] {
		header.Set("Content-Type", "text/plain; charset=utf-8")
		header.Del("Content-Disposition")
	}
	if request.Method == stdhttp.MethodHead {
		writer.WriteHeader(stdhttp.StatusOK)
		return
	}
	switch name {
	case "cpu", "trace":
		start, stop := pprof.StartCPUProfile, pprof.StopCPUProfile
		if name == "trace" {
			start, stop = trace.Start, trace.Stop
		}
		if err := start(writer); err != nil {
			// Another in-process owner holds the global profiler.
			_ = foundryhttp.WriteError(writer, request, fault.New(fault.Overloaded, "the runtime profiler is busy"))
			return
		}
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		select {
		case <-timer.C:
		case <-request.Context().Done():
			timer.Stop()
		}
		stop()
	default:
		_ = pprof.Lookup(name).WriteTo(writer, debug)
	}
}
