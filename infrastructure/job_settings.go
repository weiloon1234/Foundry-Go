package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis"
)

type JobDriver string

const (
	MemoryJobs JobDriver = "memory"
	RedisJobs  JobDriver = "redis"
	// SyncJobs runs each dispatched job inline in the dispatching caller
	// (jobs/inline). Like memory it is process-local and non-durable.
	SyncJobs JobDriver = "sync"
)

// JobConnectionSettings configures one named job connection. The memory
// driver is process-local: a job dispatched by a process that runs no worker
// kernel (for example an HTTP replica) is never executed and is lost when that
// process exits. Outside local, development and test environments it therefore
// requires AllowMemory; production deployments should use the redis driver.
//
//foundry:config
type JobConnectionSettings struct {
	Driver       JobDriver
	Redis        redis.ConnectionName
	AllowMemory  bool
	DefaultQueue jobs.Queue
	Dispatch     jobs.DispatchConfig
	Queue        jobs.QueueConfig
}

func DefaultJobConnectionSettings() JobConnectionSettings {
	return JobConnectionSettings{Driver: MemoryJobs, DefaultQueue: "default", Dispatch: jobs.DefaultDispatchConfig(keyspace.Namespace{}), Queue: jobs.DefaultQueueConfig()}
}

type JobConnections map[jobs.ConnectionName]JobConnectionSettings

func (m *JobConnections) UnmarshalText(data []byte) error {
	schema, err := JobConnectionSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(jobs.ConnectionName) JobConnectionSettings { return DefaultJobConnectionSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

type JobsSettings struct {
	Default     jobs.ConnectionName
	Connections JobConnections
}

func DefaultJobsSettings() JobsSettings { return JobsSettings{Default: "default"} }

// localEnvironment reports whether a namespace environment is a developer or
// test environment where process-local job storage is an intentional choice.
func localEnvironment(environment string) bool {
	switch environment {
	case "local", "development", "dev", "test", "testing":
		return true
	}
	return false
}

// validateMemory rejects the process-local memory driver outside local/test
// environments unless the connection explicitly opts in.
func (s JobConnectionSettings) validateMemory(name jobs.ConnectionName) error {
	if s.Driver != MemoryJobs && s.Driver != SyncJobs || s.AllowMemory || localEnvironment(s.Dispatch.Namespace.Environment) {
		return nil
	}
	return fault.New(fault.Invalid, "job connection "+string(name)+" uses a process-local "+string(s.Driver)+" driver outside a local or test environment; its jobs are not durable and can be lost. Configure the redis driver, or set allow_memory = true on this connection to opt in")
}
