package http

import (
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Only framework-owned operation names reach this helper. Request and response
// resources share ownership and diagnostics, including panic and Goexit paths.
func cleanupEndpointResource(r *stdhttp.Request, operation string, cleanup func() error) {
	if cleanup == nil {
		return
	}
	message := "HTTP endpoint " + operation + " cleanup"
	var returned error
	owned := callback.Isolated(message, func() error { returned = cleanup(); return nil })
	if owned != nil {
		logRouteFailure(r, message+" failed", owned)
	} else if returned != nil {
		logRouteFailure(r, message+" failed", fault.Wrap(fault.Internal, message+" failed", returned))
	}
}

type preparedResponse struct {
	data        []byte
	headers     []ResponseHeader
	operational error
	file        *preparedFile
	// status is the handler-selected success status; zero is the declared one.
	status int
	// location is a validated redirect target.
	location string
	// events streams a prepared server-sent event response.
	events func(stdhttp.ResponseWriter, *stdhttp.Request) error
	// release ends a detached file-source context after the transfer.
	release func()
}

func (p preparedResponse) statusOr(declared int) int {
	if p.status != 0 {
		return p.status
	}
	return declared
}

func (p preparedResponse) cleanup() func() error {
	if p.file == nil || p.file.reader == nil {
		if p.release == nil {
			return nil
		}
		return func() error { p.release(); return nil }
	}
	return func() error {
		err := p.file.reader.Close()
		if p.release != nil {
			p.release()
		}
		return err
	}
}
