package hostcapabilities

import (
	"context"
	"net"

	"github.com/weiloon1234/Foundry-Go/deployment"
)

// ConnectCompositor dials the declared Unix endpoint using the caller's deadline.
// The caller owns the returned connection and closes it when finished.
func ConnectCompositor(ctx context.Context, bindings deployment.Bindings) (net.Conn, error) {
	endpoint, err := bindings.Endpoint("compositor")
	if err != nil {
		return nil, err
	}
	return (&net.Dialer{}).DialContext(ctx, endpoint.Network(), endpoint.Path)
}

// CompositorEnvironment supplies explicit Wayland settings to a child process.
func CompositorEnvironment(bindings deployment.Bindings) ([]string, error) {
	endpoint, err := bindings.Endpoint("compositor")
	if err != nil {
		return nil, err
	}
	return []string{"QT_QPA_PLATFORM=wayland", "XDG_RUNTIME_DIR=" + endpoint.Directory, "WAYLAND_DISPLAY=" + endpoint.Name}, nil
}
