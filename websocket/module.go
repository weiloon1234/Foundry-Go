package websocket

import (
	"context"
	"errors"
	stdhttp "net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

type ServerConfig struct {
	HTTP       foundryhttp.ServerConfig
	Path       string
	Middleware []foundryhttp.Middleware
}

func DefaultServerConfig() ServerConfig {
	return ServerConfig{HTTP: foundryhttp.DefaultServerConfig(), Path: "/ws"}
}
func (c ServerConfig) Validate() error {
	if err := c.HTTP.Validate(); err != nil {
		return err
	}
	if _, err := foundryhttp.StaticPath(c.Path).URL(foundryhttp.NoPath{}); err != nil {
		return err
	}
	for _, middleware := range c.Middleware {
		if err := middleware.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Module registers the WebSocket kernel and an injectable Hub. HTTP middleware
// wraps the exact upgrade route; trusted proxy/public-origin policy can therefore
// run before origin validation. List providers owning borrowed services in requires.
func Module(name foundation.ProviderID, key foundation.Key[*Hub], config ServerConfig, requires []foundation.ProviderID, construct func(foundation.Resolver) (*Hub, error)) foundation.Module {
	return DeclaredModule(name, key, config, requires, func(r foundation.Resolver) (*Hub, []foundryhttp.Middleware, error) {
		if construct == nil {
			return nil, nil, fault.New(fault.Invalid, "WebSocket module requires a hub factory")
		}
		hub, err := construct(r)
		return hub, nil, err
	})
}

// DeclaredModule allows domain declarations to provide upgrade middleware during
// pure construction while preserving the same single-server kernel ownership.
func DeclaredModule(name foundation.ProviderID, key foundation.Key[*Hub], config ServerConfig, requires []foundation.ProviderID, construct func(foundation.Resolver) (*Hub, []foundryhttp.Middleware, error)) foundation.Module {
	type declaredHub struct {
		hub     *Hub
		handler stdhttp.Handler
	}
	declarations := foundation.NewKey[declaredHub](string(name) + ".declared")

	config.HTTP = config.HTTP.Snapshot()
	config.Middleware = slices.Clone(config.Middleware)
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "WebSocket module requires a hub factory")
		}
		if err := config.Validate(); err != nil {
			return err
		}
		if err := foundation.Factory(r, declarations, func(resolver foundation.Resolver) (declaredHub, error) {
			hub, middleware, err := construct(resolver)
			if err != nil {
				return declaredHub{}, err
			}
			if hub == nil || hub.done == nil {
				return declaredHub{}, fault.New(fault.Invalid, "WebSocket factory returned no hub")
			}
			route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "foundry.websocket", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath(config.Path))
			router, err := foundryhttp.NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, request *stdhttp.Request, _ foundryhttp.NoPath) {
				hub.ServeHTTP(w, request)
			}))
			if err != nil {
				return declaredHub{}, err
			}
			handler, err := foundryhttp.ApplyMiddleware(router, append(slices.Clone(config.Middleware), middleware...)...)
			if err != nil {
				return declaredHub{}, err
			}
			return declaredHub{hub, handler}, nil
		}); err != nil {
			return err
		}
		if err := foundation.Factory(r, key, func(resolver foundation.Resolver) (*Hub, error) {
			d, err := foundation.Resolve(resolver, declarations)
			return d.hub, err
		}); err != nil {
			return err
		}
		return r.Kernel(foundation.WebSocket, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
			hub, err := foundation.Resolve(runtime.Services(), key)
			if err != nil {
				return nil, err
			}
			if hub == nil || hub.done == nil {
				return nil, fault.New(fault.Invalid, "WebSocket factory returned no hub")
			}
			d, err := foundation.Resolve(runtime.Services(), declarations)
			if err != nil {
				return nil, err
			}

			server, err := foundryhttp.Prepare(d.handler, config.HTTP, runtime.Logger())
			if err != nil {
				return nil, err
			}
			hub.mu.Lock()
			if hub.server != nil || hub.closing {
				hub.mu.Unlock()
				return nil, fault.New(fault.Conflict, "WebSocket hub already belongs to a server or is stopping")
			}
			hub.server = server
			close(hub.serverSelected)
			hub.mu.Unlock()
			return foundation.KernelFunc(func(ctx context.Context) error {
				ctx, unlink := contextlink.Link(ctx, hub.ctx)
				defer unlink()
				stop := context.AfterFunc(ctx, hub.beginStop)
				defer stop()
				if err := hub.Start(ctx); err != nil {
					return errors.Join(err, hub.Stop(context.Background()))
				}
				err := server.Run(ctx)
				return errors.Join(err, hub.Stop(context.Background()))
			}), nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		hub, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("hub", func(ctx context.Context) error {
			err := hub.Stop(ctx)
			<-hub.Done()
			return errors.Join(err, hub.Stop(context.Background()))
		}); err != nil {
			return errors.Join(err, hub.Stop(ctx))
		}
		return nil
	}}
}

// Ready waits for this Hub's Module-selected listener and returns its bound
// address. Manually embedded HTTP handlers use their host's readiness API.
func (h *Hub) Ready(ctx context.Context) (string, error) {
	if h == nil || h.done == nil || ctx == nil {
		return "", fault.New(fault.Invalid, "WebSocket readiness requires a hub and context")
	}
	ctx, unlink := contextlink.Link(ctx, h.ctx)
	defer unlink()
	select {
	case <-h.serverSelected:
		return h.server.Ready(ctx)
	default:
	}
	select {
	case <-h.serverSelected:
	case <-h.done:
		return "", Stopping
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return h.server.Ready(ctx)
}
