package websocket

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
)

// BackendConnection owns immutable cluster selection, borrowing its authority adapter.
// It registers no kernel or socket. A local connection is explicitly process-local.
type BackendConnection struct {
	local   bool
	backend ClusterBackend
	cluster ClusterConfig
}

func NewLocalConnection() *BackendConnection { return &BackendConnection{local: true} }
func NewClusterConnection(backend ClusterBackend, cluster ClusterConfig) (*BackendConnection, error) {
	if credential.IsNil(backend) {
		return nil, fault.New(fault.Invalid, "realtime connection requires a cluster adapter")
	}
	if err := cluster.Validate(); err != nil {
		return nil, err
	}
	return &BackendConnection{backend: backend, cluster: cluster}, nil
}
func (c *BackendConnection) NewHub(registry *Registry, authentication *foundryhttp.Authentication, config Config) (*Hub, error) {
	if c == nil {
		return nil, fault.New(fault.Invalid, "realtime connection is missing")
	}
	if c.local {
		return New(registry, authentication, config)
	}
	return NewDistributed(registry, authentication, config, c.backend, c.cluster)
}
func (c *BackendConnection) NewPublisher(registry *Registry, config Config) (*Publisher, error) {
	if c == nil || c.local {
		return nil, fault.New(fault.Invalid, "standalone realtime publication requires a cluster connection")
	}
	return NewPublisher(registry, config, c.backend, c.cluster)
}
