package http

import (
	"context"
	stdhttp "net/http"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpdeadline"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
)

// UploadPoolConfig bounds one independently provisioned multipart capture pool.
// Directory must be private and writable. Use distinct quota-controlled
// volumes for large and small work; limits apply to this process only.
type UploadPoolConfig struct {
	Directory string
	MaxActive int
	MaxBytes  int64
}

// UploadPool reserves slots and complete encoded-body budgets before middleware
// or capture. It must not be copied. The framework server claims it before
// serving and closes it after HTTP drain.
type UploadPool struct{ pool *upload.Pool }

// NewUploadPool validates configuration and inspects the private directory without
// writing or claiming it. The framework server claims it before serving and
// reconciles captures left by a dead owner under an exclusive filesystem lock.
func NewUploadPool(config UploadPoolConfig) (*UploadPool, error) {
	pool, err := upload.NewPool(upload.PoolConfig{Directory: config.Directory, MaxActive: config.MaxActive, MaxBytes: config.MaxBytes})
	if err != nil {
		return nil, err
	}
	return &UploadPool{pool: pool}, nil
}

// Usage reports active requests and reserved bytes, including failed cleanup.
func (p *UploadPool) Usage() (active int, bytes int64) { return p.pool.Usage() }

// RetryCleanup retries only known owned files; failed cleanup stays charged.
func (p *UploadPool) RetryCleanup() error { return p.pool.RetryCleanup() }

// Claim takes this process's exclusive ownership of the pool directory and
// enables reservations; ownership ends at Close or process exit. The framework
// Server claims its router's pools before accepting connections, so call Claim
// only when hosting the Router on another server, before it accepts requests.
// Claim is idempotent for the same pool. A framework server and a manual Claim
// must not share a pool. A live kernel lock or a legacy unversioned marker
// refuses the claim; legacy markers require the documented one-time migration.
func (p *UploadPool) Claim() error { return p.pool.Claim() }

// Close seals admission and releases directory ownership only after requests
// drain and retained cleanup succeeds. It never cancels active work.
func (p *UploadPool) Close() error { return p.pool.Close() }

// MultipartAdmission binds a descriptor to a shared pool and its directory.
// Each endpoint reserves EndpointLimits.Multipart.Bytes, independent of the
// client's Content-Length. BodyLimit must declare the same encoded ceiling.
func MultipartAdmission[B any](d Multipart[B], pool *UploadPool) Multipart[B] {
	if pool == nil || pool.pool == nil {
		d.err = safeerror.NewFault(fault.Invalid, "multipart admission requires an upload pool")
		return d
	}
	d.pool = pool
	return MultipartDirectory(d, pool.pool.Directory())
}

type uploadRoute struct {
	pool  *UploadPool
	bytes int64
}
type uploadReservationKey struct{}
type requestUpload struct {
	route              uploadRoute
	reservation        *upload.Reservation
	readTimeout        *httpdeadline.ReadTimeout
	observeBodyTimeout bool
	timeoutResponse    atomic.Bool
}
type uploadSelector func(*stdhttp.Request) uploadRoute

func uploadPools(handler stdhttp.Handler) []*UploadPool {
	if source, ok := handler.(interface{ uploadPools() []*UploadPool }); ok {
		return source.uploadPools()
	}
	return nil
}

func (r *Router) uploadPools() []*UploadPool       { return r.pools }
func (h routedHandler) uploadPools() []*UploadPool { return h.pools }

func selectUpload(handler stdhttp.Handler) uploadSelector {
	if source, ok := handler.(interface {
		uploadRoute(*stdhttp.Request) uploadRoute
	}); ok {
		return source.uploadRoute
	}
	return nil
}

func beginUpload(r *stdhttp.Request, selectRoute uploadSelector) (*stdhttp.Request, func(), error) {
	if selectRoute == nil {
		return r, func() {}, nil
	}
	route := selectRoute(r)
	if existing, ok := r.Context().Value(uploadReservationKey{}).(*requestUpload); ok {
		if existing.route != route {
			return r, func() {}, safeerror.NewFault(fault.Invalid, "middleware changed an admitted upload route")
		}
		return r, func() {}, nil
	}
	if route.pool == nil {
		return r, func() {}, nil
	}
	if scope := requestScopeFrom(r.Context()); scope != nil && scope.budget != nil {
		return r, func() {}, safeerror.NewFault(fault.Invalid, "upload route metadata was hidden from kernel admission; use ApplyMiddleware")
	}
	reservation, err := route.pool.pool.Reserve(r.Context(), route.bytes)
	if err != nil {
		return r, func() {}, err
	}
	ctx := context.WithValue(r.Context(), uploadReservationKey{}, &requestUpload{route: route, reservation: reservation})
	return r.WithContext(ctx), reservation.Release, nil
}

func rejectUpload(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	rejectUnreadBody(w, r)
	writeRoutingError(w, r, capacityError(err))
}

func (r *Router) uploadRoute(request *stdhttp.Request) uploadRoute {
	if r == nil || r.mux == nil || len(r.uploads) == 0 {
		return uploadRoute{}
	}
	_, pattern := r.mux.Handler(request)
	return r.uploads[pattern]
}

func (h routedHandler) uploadRoute(request *stdhttp.Request) uploadRoute {
	if h.uploads == nil {
		return uploadRoute{}
	}
	return h.uploads(request)
}

func wrapUploadAdmission(handler stdhttp.Handler, selector uploadSelector) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		request, release, err := beginUpload(r, selector)
		if err != nil {
			rejectUpload(w, r, err)
			return
		}
		defer release()
		handler.ServeHTTP(w, request)
	})
}
