package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis"
)

type JobDriver string

const (
	MemoryJobs JobDriver = "memory"
	RedisJobs  JobDriver = "redis"
)

//foundry:config
type JobConnectionSettings struct {
	Driver       JobDriver
	Redis        redis.ConnectionName
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
