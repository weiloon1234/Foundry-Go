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
	TimeZone    temporal.ZoneName
	Services    infrastructure.Settings
	HTTP        HTTPSettings
	Worker      WorkerSettings
	Scheduler   SchedulerSettings
	Realtime    RealtimeServerSettings
	Features    FeatureSettings
	Image       ImageSettings
	Log         LogSettings
	Maintenance MaintenanceSettings
	// Encryption is the application key ring (active key plus retained
	// previous keys) used for MFA factors and encrypted cookies.
	Encryption EncryptionSettings
	// ShutdownTimeout is the whole shutdown budget: StopDelay, kernel drain
	// (such as the HTTP shutdown grace) and every cleanup share it.
	ShutdownTimeout time.Duration
	// StopDelay is a lame-duck period after a shutdown request: readiness
	// fails while listeners keep serving, then admission closes and kernels
	// drain. Zero disables it. It must leave room for kernel drain.
	StopDelay time.Duration
	// StartupTimeout bounds provider boot; zero leaves it unbounded.
	StartupTimeout time.Duration
}
type HTTPSettings struct {
	Enabled         bool
	Server          http.ServerConfig
	SecurityHeaders bool
	Probes          ProbeSettings
}
type ImageSettings struct {
	Enabled bool
	Config  imaging.Config
}

func DefaultSettings() Settings {
	server := http.DefaultServerConfig()
	server.AccessLog = true
	return Settings{TimeZone: temporal.UTC, Services: infrastructure.DefaultSettings(), Worker: DefaultWorkerSettings(), Scheduler: DefaultSchedulerSettings(), Realtime: DefaultRealtimeServerSettings(), Features: DefaultFeatureSettings(), HTTP: HTTPSettings{Enabled: true, Server: server, SecurityHeaders: true, Probes: DefaultProbeSettings()}, Image: ImageSettings{Config: imaging.DefaultConfig()}, Log: DefaultLogSettings(), Maintenance: DefaultMaintenanceSettings(), ShutdownTimeout: foundation.DefaultShutdownTimeout}
}
