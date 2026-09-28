package http

import (
	"context"
	stdhttp "net/http"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type browserSessionKey struct{}
type browserSessionState struct {
	owner                              *browserSessionAdapter
	context                            context.Context
	cancel                             context.CancelFunc
	credential                         value.Optional[secret.String]
	method                             string
	mu                                 sync.Mutex
	work                               sync.WaitGroup
	typed, mutable, used, busy, sealed bool
	cookie                             string
	deadline                           time.Time
}

func browserState(ctx context.Context) *browserSessionState {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(browserSessionKey{}).(*browserSessionState)
	return s
}
func (s *browserSessionState) close() {
	s.mu.Lock()
	s.sealed = true
	s.cookie = ""
	s.cancel()
	s.mu.Unlock()
	s.work.Wait()
	s.mu.Lock()
	s.credential = value.Optional[secret.String]{}
	s.mu.Unlock()
}
func bindBrowserResponse(ctx context.Context, mutable bool) error {
	s := browserState(ctx)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.typed || s.sealed {
		return fault.New(fault.Invalid, "browser session response is already bound")
	}
	s.typed = true
	s.mutable = mutable && !safeBrowserMethod(s.method)
	return nil
}
func (s *browserSessionState) operation(ctx context.Context, fn func(context.Context) (string, time.Time, error)) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "browser session operation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.sealed || !s.typed || !s.mutable || s.used {
		s.mu.Unlock()
		return fault.New(fault.Conflict, "session mutation requires one uncommitted typed unsafe response")
	}
	s.used = true
	s.busy = true
	s.work.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock(); s.work.Done() }()
	op, cancel := contextlink.Link(ctx, s.context)
	defer cancel()
	var cookie string
	var deadline time.Time
	err := callback.Isolated("HTTP session mutation", func() error {
		if err := op.Err(); err != nil {
			return err
		}
		var err error
		cookie, deadline, err = fn(op)
		return err
	})
	if canceled := op.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return fault.Wrap(fault.Internal, "HTTP session mutation failed", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return fault.New(fault.Conflict, "browser session response is closed")
	}
	s.cookie = cookie
	s.deadline = deadline
	return nil
}

// Called only after a typed handler and response preparation both succeed. No
// native writer is wrapped or retained, and failed responses never see the secret.
func publishBrowserSession(ctx context.Context, w stdhttp.ResponseWriter, status int) error {
	s := browserState(ctx)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.sealed || s.busy {
		s.sealed = true
		s.cancel()
		s.mu.Unlock()
		return fault.New(fault.Conflict, "session mutation did not finish before response")
	}
	s.sealed = true
	cookie, deadline := s.cookie, s.deadline
	s.cookie = ""
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if cookie == "" || status < 200 || status >= 400 {
		return nil
	}
	if !deadline.IsZero() {
		err := callback.Isolated("HTTP session expiry", func() error {
			now, err := s.owner.now()
			if err != nil {
				return err
			}
			if !now.Before(deadline) {
				return Unauthenticated
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.Header().Add("Set-Cookie", cookie)
	return nil
}
