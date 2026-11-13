// Package hostcapabilities demonstrates typed deployment dependency intake.
package hostcapabilities

import "github.com/weiloon1234/Foundry-Go/deployment"

// doc-example:begin host-capability-bindings
type RelayDependencies struct {
	Control  deployment.Endpoint
	Checksum deployment.Executable
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
	checksum, err := bindings.Executable("checksum")
	if err != nil {
		return RelayDependencies{}, true, err
	}
	return RelayDependencies{Control: control, Checksum: checksum}, true, nil
}

// doc-example:end host-capability-bindings
