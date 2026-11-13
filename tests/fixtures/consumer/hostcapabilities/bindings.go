// Package hostcapabilities demonstrates typed deployment dependency intake.
package hostcapabilities

import "github.com/weiloon1234/Foundry-Go/deployment"

// doc-example:begin host-capability-bindings
// RelayDependencies contains the private control endpoint and tool used by the app.
type RelayDependencies struct {
	// Control addresses the relay's private HTTP control listener.
	Control deployment.Endpoint
	// JSON is the package-owned jq executable for JSON processing.
	JSON deployment.Executable
}

// LoadRelayDependencies runs once during application assembly.
// The caller explicitly chooses its local-development path when present is false.
func LoadRelayDependencies() (dependencies RelayDependencies, present bool, err error) {
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return RelayDependencies{}, present, err
	}
	control, err := bindings.Endpoint("relay")
	if err != nil {
		return RelayDependencies{}, true, err
	}
	jsonTool, err := bindings.Executable("json")
	if err != nil {
		return RelayDependencies{}, true, err
	}
	return RelayDependencies{Control: control, JSON: jsonTool}, true, nil
}

// doc-example:end host-capability-bindings
