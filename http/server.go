package http

import (
	"context"
	"errors"
	"log/slog"
	"net"
	stdhttp "net/http"
	"reflect"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"golang.org/x/net/netutil"
)

// Server owns one listener and the synchronous lifetime of each admitted
// handler. It must not be copied or reused after Run. Use Prepare or Module;
// the zero value is invalid. Work launched by a raw handler after it returns,
// including hijacked connections, needs its own application lifecycle owner.
type Server struct {
	config    ServerConfig
	handler   stdhttp.Handler
	logger    *slog.Logger
	managed   bool
	observers []RequestObserver

	mu        sync.Mutex
	attempted bool
	address   string
	startErr  error
	ready     chan struct{}
	done      chan struct{}
}

// Prepare validates a standalone server without opening sockets or starting
// goroutines. The supplied logger is local to this server; no global is changed.
func Prepare(handler stdhttp.Handler, config ServerConfig, logger *slog.Logger, options ...ServerOption) (*Server, error) {
	if logger == nil {
		return nil, fault.New(fault.Invalid, "HTTP server needs a logger")
	}
	server, err := prepare(handler, config, options...)
	if err != nil {
		return nil, err
	}
	server.logger = logger
	return server, nil
}

func prepare(handler stdhttp.Handler, config ServerConfig, options ...ServerOption) (*Server, error) {
	settings, err := configureServer(options)
	if err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, fault.New(fault.Invalid, "HTTP server needs a handler")
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, fault.New(fault.Invalid, "HTTP server needs a non-nil handler")
		}
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Server{observers: settings.observers, handler: handler, config: config.Snapshot(), ready: make(chan struct{}), done: make(chan struct{})}, nil
}

// Ready waits for the one startup attempt and returns the actual bound TCP
// address. It reports binding/cancellation failure instead of inventing an
// address. Readiness records successful binding, not ongoing service health.
func (s *Server) Ready(ctx context.Context) (string, error) {
	if s == nil || s.ready == nil || ctx == nil {
		return "", fault.New(fault.Invalid, "HTTP readiness requires a prepared server and context")
	}
	select {
	case <-s.ready:
		return s.address, s.startErr
	default:
	}
	select {
	case <-s.ready:
		return s.address, s.startErr
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Done closes after Run's listener and admitted handlers actually finish. A
// server that has never run is not done. A shutdown deadline cannot kill Go code.
func (s *Server) Done() <-chan struct{} { return s.done }

// Run binds and serves once until cancellation or a listener failure. Normal
// cancellation stops admission, then allows active requests the configured
// shutdown grace. Expiry cancels their contexts and closes ordinary connections,
// but Run still waits for actual handler exit before releasing ownership.
func (s *Server) Run(ctx context.Context) error { return s.run(ctx, nil, false) }

func (s *Server) run(ctx context.Context, logger *slog.Logger, managed bool) error {
	if s == nil || s.ready == nil || ctx == nil {
		return fault.New(fault.Invalid, "HTTP startup requires a prepared server and context")
	}
	s.mu.Lock()
	if s.managed != managed {
		s.mu.Unlock()
		return fault.New(fault.Invalid, "application module owns HTTP startup")
	}
	if s.attempted {
		s.mu.Unlock()
		return fault.New(fault.Conflict, "HTTP server has already run")
	}
	s.attempted = true
	s.mu.Unlock()
	defer close(s.done)
	if !managed {
		logger = s.logger
	}
	if err := ctx.Err(); err != nil {
		s.completeStart("", err)
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.config.Address)
	if err != nil {
		err = fault.Wrap(fault.Internal, "HTTP listener could not start", err)
		s.completeStart("", err)
		return err
	}
	connectionLimit := s.config.MaxConnections
	if connectionLimit == 0 {
		connectionLimit = DefaultServerConfig().MaxConnections
	}
	listener = netutil.LimitListener(listener, connectionLimit)

	// Preserve application values but defer request cancellation until the grace
	// expires. Cancelling the kernel should first let active work finish normally.
	requests, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	owners := newHandlerLifetime()
	server := &stdhttp.Server{
		Handler:           owners.wrap(s.handler, logger, s.config, s.observers...),
		ReadHeaderTimeout: s.config.ReadHeaderTimeout, ReadTimeout: s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout, IdleTimeout: s.config.IdleTimeout,
		MaxHeaderBytes: s.config.MaxHeaderBytes,
		ErrorLog:       slog.NewLogLogger(logger.Handler(), slog.LevelError),
		BaseContext:    func(net.Listener) context.Context { return requests },
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	s.completeStart(listener.Addr().String(), nil)

	var serveErr error
	var received bool
	select {
	case serveErr = <-served:
		received = true
	case <-ctx.Done():
	}
	owners.seal()
	grace, stopGrace := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer stopGrace()
	shutdownErr := server.Shutdown(grace)
	if shutdownErr == nil {
		// The same grace covers handler ownership after hijack, which the
		// standard server no longer sees as an active connection.
		select {
		case <-owners.done:
		case <-grace.Done():
			shutdownErr = grace.Err()
		}
	}
	if shutdownErr != nil {
		cancel()
		code := fault.Internal
		if errors.Is(shutdownErr, context.DeadlineExceeded) {
			code = fault.Timeout
		}
		shutdownErr = errors.Join(fault.Wrap(code, "HTTP shutdown grace did not complete", shutdownErr), server.Close())
	}
	// Shutdown ignores hijacked connections. The admitted handler itself may
	// still be running, so its ownership is tracked independently of net/http.
	<-owners.done
	if !received {
		serveErr = <-served
	}
	if errors.Is(serveErr, stdhttp.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(ctx.Err(), serveErr, shutdownErr)
}

func (s *Server) completeStart(address string, err error) {
	s.address, s.startErr = address, err
	close(s.ready)
}
