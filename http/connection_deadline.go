package http

import (
	"context"
	"net"

	"github.com/weiloon1234/Foundry-Go/internal/httpdeadline"
)

type requestDeadlineConnKey struct{}

func requestDeadlineContext(ctx context.Context, conn net.Conn) context.Context {
	if owned, ok := conn.(*httpdeadline.Conn); ok {
		return context.WithValue(ctx, requestDeadlineConnKey{}, owned)
	}
	return ctx
}
