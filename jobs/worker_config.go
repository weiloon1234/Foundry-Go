package jobs

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"slices"
	"time"
)

// Subscription gives a queue a positive share of reservation opportunities.
// Weights provide bounded service for lower-priority queues under steady load;
// they do not preempt running work or promise globally strict priority ordering.
type Subscription struct {
	Queue  Queue
	Weight uint16
}
type WorkerConfig struct {
	Namespace         keyspace.Namespace
	Queues            []Subscription `config:",json"`
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	OperationTimeout  time.Duration
	PollInterval      time.Duration
}

func DefaultWorkerConfig(namespace keyspace.Namespace, queues ...Queue) WorkerConfig {
	subscriptions := make([]Subscription, len(queues))
	for i, queue := range queues {
		subscriptions[i] = Subscription{Queue: queue, Weight: 1}
	}
	return WorkerConfig{Namespace: namespace, Queues: subscriptions, Concurrency: 4, LeaseDuration: 30 * time.Second, HeartbeatInterval: 5 * time.Second, OperationTimeout: 5 * time.Second, PollInterval: 100 * time.Millisecond}
}
func (c WorkerConfig) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if err := lease.ValidateDuration(c.LeaseDuration); err != nil {
		return err
	}
	if c.Concurrency <= 0 || c.Concurrency > 4096 || len(c.Queues) == 0 || len(c.Queues) > 64 || c.PollInterval <= 0 || c.PollInterval > time.Minute || c.HeartbeatInterval <= 0 || c.HeartbeatInterval > c.LeaseDuration/3 || c.OperationTimeout <= 0 || c.OperationTimeout > c.LeaseDuration/3 {
		return fault.New(fault.Invalid, "invalid worker concurrency, queues or timing bounds")
	}
	seen := make(map[Queue]bool)
	total := 0
	for _, queue := range c.Queues {
		if err := queue.Queue.Validate(); err != nil {
			return err
		}
		if queue.Weight == 0 || seen[queue.Queue] {
			return fault.New(fault.Invalid, "worker queues require unique names and positive weights")
		}
		seen[queue.Queue] = true
		total += int(queue.Weight)
	}
	if total > 1024 {
		return fault.New(fault.Invalid, "worker queue weight capacity exceeded")
	}
	return nil
}
func (c WorkerConfig) snapshot() WorkerConfig { c.Queues = slices.Clone(c.Queues); return c }
