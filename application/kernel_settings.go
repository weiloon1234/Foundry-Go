package application

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
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
	Archive    JobArchiveSettings
}

// JobArchiveSettings enable the durable failed-job archive (jobs/archive):
// the worker writes each confirmed terminal failure to Database/Schema, which
// must have archive.Migrations applied.
type JobArchiveSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
}

// prepareJobArchive selects the archive's default database and schema and
// checks them before any resource is acquired, so registration and
// App.Migrations agree on one target.
func prepareJobArchive(s Settings) (JobArchiveSettings, error) {
	a := s.Worker.Archive
	if !a.Enabled {
		return a, nil
	}
	if a.Database == "" {
		a.Database = s.Services.Database.Default
	}
	if a.Schema == "" {
		a.Schema = "public"
	}
	if _, ok := s.Services.Database.Connections[a.Database]; !ok {
		return a, fault.New(fault.Missing, "job archive database connection is not configured")
	}
	if !sqlname.Valid(a.Schema) {
		return a, fault.New(fault.Invalid, "job archive requires a valid PostgreSQL schema")
	}
	return a, nil
}

func DefaultWorkerSettings() WorkerSettings {
	return WorkerSettings{Config: jobs.DefaultWorkerConfig(keyspace.Namespace{}), Archive: JobArchiveSettings{Schema: "public"}}
}

type SchedulerSettings struct {
	Enabled                             bool
	Group                               schedule.Group
	Concurrency, MaxHistory, MaxPerTick int
	PollInterval, LeadershipTTL, Grace  time.Duration
	CapacityWait, DrainTimeout          time.Duration
}

func DefaultSchedulerSettings() SchedulerSettings {
	c := schedule.DefaultConfig("default")
	return SchedulerSettings{Group: c.Group, Concurrency: c.Concurrency, MaxHistory: c.MaxHistory, MaxPerTick: c.MaxPerTick, PollInterval: c.PollInterval, LeadershipTTL: c.LeadershipTTL, Grace: c.Grace, CapacityWait: c.CapacityWait, DrainTimeout: c.DrainTimeout}
}
func (s SchedulerSettings) runtime(source clock.Clock) schedule.Config {
	return schedule.Config{Group: s.Group, Clock: source, Concurrency: s.Concurrency, MaxHistory: s.MaxHistory, MaxPerTick: s.MaxPerTick, PollInterval: s.PollInterval, LeadershipTTL: s.LeadershipTTL, Grace: s.Grace, CapacityWait: s.CapacityWait, DrainTimeout: s.DrainTimeout}
}

type RealtimeServerSettings struct {
	Enabled bool
	// Shared serves upgrades at Path on the application HTTP listener instead
	// of a dedicated listener and WebSocket kernel; running foundation.HTTP then
	// serves both. HTTP request/connection limits must accommodate sockets.
	Shared bool
	// Publisher constructs a managed cross-process publisher for processes
	// that run no hub, such as workers. It requires a cluster connection and
	// the same channel declarations as the socket servers.
	Publisher  bool
	Connection websocket.ConnectionName
	Config     websocket.Config
	HTTP       http.ServerConfig
	Path       string
}

func DefaultRealtimeServerSettings() RealtimeServerSettings {
	server := websocket.DefaultServerConfig()
	return RealtimeServerSettings{Config: websocket.DefaultConfig(), HTTP: server.HTTP, Path: server.Path}
}
