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

// WorkerConfig bounds one worker kernel. PollInterval is the first idle wait;
// consecutive empty reservations double it up to MaxPollInterval, and a local
// enqueue through the same backend wakes idle loops immediately. Zero
// MaxPollInterval keeps a fixed PollInterval.
//
// DrainTimeout is the graceful-shutdown budget: when Run's context ends or Drain
// is called, reservations stop while admitted handlers keep their heartbeats and
// run for up to DrainTimeout before cancellation. Zero cancels immediately. Keep
// DrainTimeout plus OperationTimeout within the application shutdown timeout.
//
// FailureBackoff caps the jittered exponential wait after backend failures;
// the worker logs them and keeps running. RetryUnregistered treats an unknown job
// name/version as transient (for example during a rolling deploy): it consumes
// attempts under the envelope's own retry policy before failing as unregistered.
type WorkerConfig struct {
	FailureLog        bool
	RetryUnregistered bool
	Namespace         keyspace.Namespace
	Queues            []Subscription `config:",json"`
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	OperationTimeout  time.Duration
	PollInterval      time.Duration
	MaxPollInterval   time.Duration
	DrainTimeout      time.Duration
	FailureBackoff    time.Duration
}

func DefaultWorkerConfig(namespace keyspace.Namespace, queues ...Queue) WorkerConfig {
	subscriptions := make([]Subscription, len(queues))
	for i, queue := range queues {
		subscriptions[i] = Subscription{Queue: queue, Weight: 1}
	}
	return WorkerConfig{FailureLog: true, RetryUnregistered: true, Namespace: namespace, Queues: subscriptions, Concurrency: 4, LeaseDuration: 30 * time.Second, HeartbeatInterval: 5 * time.Second, OperationTimeout: 5 * time.Second, PollInterval: 100 * time.Millisecond, MaxPollInterval: time.Second, DrainTimeout: 5 * time.Second, FailureBackoff: 30 * time.Second}
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
	if c.MaxPollInterval < 0 || c.MaxPollInterval > time.Minute || c.MaxPollInterval != 0 && c.MaxPollInterval < c.PollInterval || c.DrainTimeout < 0 || c.DrainTimeout > time.Hour || c.FailureBackoff < 0 || c.FailureBackoff > 10*time.Minute {
		return fault.New(fault.Invalid, "invalid worker idle, drain or failure backoff bounds")
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

// ValidateShutdown checks that a graceful drain plus its final acknowledgement
// fits within the owning application's shutdown timeout. Assembly calls this
// with the configured application timeout before any worker starts.
func (c WorkerConfig) ValidateShutdown(timeout time.Duration) error {
	if timeout <= 0 || c.DrainTimeout+c.OperationTimeout > timeout {
		return fault.New(fault.Invalid, "worker drain timeout plus operation timeout must fit within the application shutdown timeout")
	}
	return nil
}
func (c WorkerConfig) snapshot() WorkerConfig { c.Queues = slices.Clone(c.Queues); return c }
