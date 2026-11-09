// Package closeerror recognizes disconnected transports only during physical close.
package closeerror

import (
	"io"
	"net"
	"syscall"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// OnlyDisconnected requires every leaf to identify a disconnected connection.
// EOF and syscall failures require a net.OpError ancestor; TLS delegates can
// return the same errors without any socket failure. net.ErrClosed is explicit.
// protocol may recognize additional protocol-specific leaves. Unknown failures,
// classified faults and incomplete inspection remain failures. Never use this
// to classify queries, commands, transactions, or shutdown wait deadlines.
func OnlyDisconnected(err error, protocol func(error) bool) bool {
	if err == nil {
		return false
	}
	closed, other := false, false
	inspected := callback.Isolated("classify connection close", func() error {
		var transportPath []bool
		complete := errorgraph.WalkWithLeaf(err, func(current error, leaf bool, depth int) bool {
			_, transport := current.(*net.OpError)
			if depth > 0 {
				transport = transport || transportPath[depth-1]
			}
			transportPath = append(transportPath[:depth], transport)
			switch current {
			case net.ErrClosed:
				closed = true
				return true
			case io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, syscall.ECONNRESET, syscall.EPIPE:
				if transport {
					closed = true
					return true
				}
			}
			if protocol != nil && protocol(current) {
				closed = true
				return true
			}
			if _, classified := current.(*fault.Error); classified {
				other = true
			} else {
				other = leaf
			}
			return !other
		})
		other = other || !complete
		return nil
	})
	return inspected == nil && closed && !other
}
