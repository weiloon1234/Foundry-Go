// Package application assembles configured Foundry services, domain declarations
// and runtime kernels without consumer-written infrastructure factories. Feature
// packages do not import it.
package application

import (
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/temporal"

	"time"
)

// Settings contains deployment values. Routes, models and callbacks stay Go declarations.
//
//foundry:config
type Settings struct {
	TimeZone        temporal.ZoneName
	Services        infrastructure.Settings
	HTTP            HTTPSettings
	Worker          WorkerSettings
	Scheduler       SchedulerSettings
	Realtime        RealtimeServerSettings
	Features        FeatureSettings
	Image           ImageSettings
	Log             LogSettings
	ShutdownTimeout time.Duration
}
type HTTPSettings struct {
	Enabled         bool
	Server          http.ServerConfig
	SecurityHeaders bool
}
type ImageSettings struct {
	Enabled bool
	Config  imaging.Config
}

func DefaultSettings() Settings {
	server := http.DefaultServerConfig()
	server.AccessLog = true
	return Settings{TimeZone: temporal.UTC, Services: infrastructure.DefaultSettings(), Worker: DefaultWorkerSettings(), Scheduler: DefaultSchedulerSettings(), Realtime: DefaultRealtimeServerSettings(), Features: DefaultFeatureSettings(), HTTP: HTTPSettings{Enabled: true, Server: server, SecurityHeaders: true}, Image: ImageSettings{Config: imaging.DefaultConfig()}, Log: DefaultLogSettings(), ShutdownTimeout: foundation.DefaultShutdownTimeout}
}
