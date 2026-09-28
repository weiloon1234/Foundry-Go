package jobs

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Name identifies a declared job; Version identifies its payload contract.
type Name string
type Version uint32

// Queue is a declared routing destination, distinct from a job name.
type Queue string

func (q Queue) Validate() error {
	if !identifier.Semantic(string(q)) {
		return fault.New(fault.Invalid, "job queue requires a semantic name")
	}
	return nil
}

// ID is a dispatch identity owned by its concrete payload type. It is stable
// across attempts and can be supplied again after an ambiguous dispatch result.
type ID[P any] = model.ID[ExecutionOf[P]]
type ExecutionOf[P any] struct{ _ [0]*P }

func NewID[P any]() (ID[P], error)              { return model.NewID[ExecutionOf[P]]() }
func ParseID[P any](text string) (ID[P], error) { return model.ParseID[ExecutionOf[P]](text) }

// ExecutionID is the explicit heterogeneous adapter/inspection identity.
// Converting an ID to bytes and back does not confer authorization.
type ExecutionID = model.ID[Execution]
type Execution struct{}

// Key addresses one queue within one application's environment.
type Key struct {
	address keyaddress.Address
	queue   Queue
}

func NewKey(namespace keyspace.Namespace, queue Queue) (Key, error) {
	if err := queue.Validate(); err != nil {
		return Key{}, err
	}
	address, err := keyaddress.New(namespace, "queue", string(queue))
	return Key{address: address, queue: queue}, err
}
func (k Key) Validate() error               { return k.address.Validate() }
func (k Key) Queue() Queue                  { return k.queue }
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }
func (k Key) String() string                { return k.address.String("jobs") }
