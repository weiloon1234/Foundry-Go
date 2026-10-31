package jobs

import "time"

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Name identifies a declared job; Version identifies its payload contract.
type Name string

// Version identifies a job payload schema. Zero is invalid.
type Version uint32

// Queue is a declared routing destination, distinct from a job name.
type Queue string

// Validate checks that the queue has a semantic name.
func (q Queue) Validate() error {
	if !identifier.Semantic(string(q)) {
		return safeerror.NewFault(fault.Invalid, "job queue requires a semantic name")
	}
	return nil
}

// ID is a dispatch identity owned by its concrete payload type. It is stable
// across attempts and can be supplied again after an ambiguous dispatch result.
type ID[P any] = model.ID[ExecutionOf[P]]

// ExecutionOf marks the dispatch identity type owned by payload P.
type ExecutionOf[P any] struct{ _ [0]*P }

// NewID generates a dispatch identity owned by the concrete payload type using the current time.
func NewID[P any]() (ID[P], error) { return model.NewIDAt[ExecutionOf[P]](time.Now()) }

// ParseID parses a dispatch identity while retaining its concrete payload owner.
func ParseID[P any](text string) (ID[P], error) { return model.ParseID[ExecutionOf[P]](text) }

// ExecutionID is the explicit heterogeneous adapter/inspection identity.
// Converting an ID to bytes and back does not confer authorization.
type ExecutionID = model.ID[Execution]

// Execution marks the identity type of a heterogeneous job execution.
type Execution struct{}

// Key addresses one queue within one application's environment.
type Key struct {
	address keyaddress.Address
	queue   Queue
}

// NewKey constructs a validated queue address within namespace.
func NewKey(namespace keyspace.Namespace, queue Queue) (Key, error) {
	if err := queue.Validate(); err != nil {
		return Key{}, err
	}
	address, err := keyaddress.New(namespace, "queue", string(queue))
	return Key{address: address, queue: queue}, err
}

// Validate checks that the key contains a valid namespaced address.
func (k Key) Validate() error { return k.address.Validate() }

// Queue returns the queue selected by this address.
func (k Key) Queue() Queue { return k.queue }

// Namespace returns the queue address's application and environment scope.
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }

// String returns the physical queue address.
func (k Key) String() string { return k.address.String(KeyFeature) }

// KeyFeature is the physical address family owned by this adapter.
const KeyFeature keyspace.Feature = "jobs"
