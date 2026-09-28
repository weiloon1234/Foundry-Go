package application

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/websocket"
	"time"
)

type WorkerSettings struct {
	Enabled    bool
	Connection jobs.ConnectionName
	Config     jobs.WorkerConfig
}

func DefaultWorkerSettings() WorkerSettings {
	return WorkerSettings{Config: jobs.DefaultWorkerConfig(keyspace.Namespace{})}
}

type SchedulerSettings struct {
	Enabled                             bool
	Group                               schedule.Group
	Concurrency, MaxHistory, MaxPerTick int
	PollInterval, LeadershipTTL, Grace  time.Duration
}

func DefaultSchedulerSettings() SchedulerSettings {
	c := schedule.DefaultConfig("default")
	return SchedulerSettings{Group: c.Group, Concurrency: c.Concurrency, MaxHistory: c.MaxHistory, MaxPerTick: c.MaxPerTick, PollInterval: c.PollInterval, LeadershipTTL: c.LeadershipTTL, Grace: c.Grace}
}
func (s SchedulerSettings) runtime(source clock.Clock) schedule.Config {
	return schedule.Config{Group: s.Group, Clock: source, Concurrency: s.Concurrency, MaxHistory: s.MaxHistory, MaxPerTick: s.MaxPerTick, PollInterval: s.PollInterval, LeadershipTTL: s.LeadershipTTL, Grace: s.Grace}
}

type RealtimeServerSettings struct {
	Enabled    bool
	Connection websocket.ConnectionName
	Config     websocket.Config
	HTTP       http.ServerConfig
	Path       string
}

func DefaultRealtimeServerSettings() RealtimeServerSettings {
	server := websocket.DefaultServerConfig()
	return RealtimeServerSettings{Config: websocket.DefaultConfig(), HTTP: server.HTTP, Path: server.Path}
}
