package http

import "github.com/weiloon1234/Foundry-Go/contract"

// TableOperation declares an endpoint's table association. Manifest construction
// validates its registered table and payload contracts. Repeated declarations fail.
func TableOperation(binding contract.TableOperationBinding) EndpointOption {
	return endpointOptionValue{kind: optionTableOperation, tableOperation: binding}
}
