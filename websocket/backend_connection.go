package websocket

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// BackendConnection owns immutable cluster selection, borrowing its authority adapter.
// It registers no kernel or socket. A local connection is explicitly process-local.
type BackendConnection struct {
	local   bool
	backend ClusterBackend
	cluster ClusterConfig
}

// NewLocalConnection creates a connection that builds process-local hubs.
func NewLocalConnection() *BackendConnection { return &BackendConnection{local: true} }

// NewClusterConnection validates and borrows a cluster adapter with its configuration.
func NewClusterConnection(backend ClusterBackend, cluster ClusterConfig) (*BackendConnection, error) {
	if credential.IsNil(backend) {
		return nil, safeerror.NewFault(fault.Invalid, "realtime connection requires a cluster adapter")
	}
	if err := cluster.Validate(); err != nil {
		return nil, err
	}
	return &BackendConnection{backend: backend, cluster: cluster}, nil
}

// NewHub creates a local or distributed hub according to the connection's
// configuration.
func (c *BackendConnection) NewHub(registry *Registry, authentication *foundryhttp.Authentication, config Config, opts ...Option) (*Hub, error) {
	if c == nil {
		return nil, safeerror.NewFault(fault.Invalid, "realtime connection is missing")
	}
	if c.local {
		return New(registry, authentication, config, opts...)
	}
	return NewDistributed(registry, authentication, config, c.backend, c.cluster, opts...)
}

// NewPublisher creates a standalone publisher; local connections and nil receivers are
// invalid.
func (c *BackendConnection) NewPublisher(registry *Registry, config Config, opts ...Option) (*Publisher, error) {
	if c == nil || c.local {
		return nil, safeerror.NewFault(fault.Invalid, "standalone realtime publication requires a cluster connection")
	}
	return NewPublisher(registry, config, c.backend, c.cluster, opts...)
}

// Local reports a process-local connection that cannot publish across processes.
func (c *BackendConnection) Local() bool { return c != nil && c.local }
