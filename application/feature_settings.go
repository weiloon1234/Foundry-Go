package application

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"time"
)

// StoreSettings addresses one existing PostgreSQL authority. Runtime callbacks
// and application time are supplied by the owning application, not deployment files.
type StoreSettings struct {
	Enabled   bool
	Database  database.ConnectionName
	Schema    string
	MaxActive int
	Timeout   time.Duration
}

func DefaultExtensionSettings() StoreSettings {
	c := extensions.DefaultConfig()
	return StoreSettings{Schema: c.Schema, MaxActive: c.MaxActive, Timeout: c.Timeout}
}
func DefaultNotificationSettings() StoreSettings {
	c := notifications.DefaultConfig()
	return StoreSettings{Schema: c.Schema, MaxActive: c.MaxActive, Timeout: c.Timeout}
}

// EventsSettings configure the event bus. QueuedListeners delivers listeners
// declared with ListenQueued (or events.Listener.Queued) as jobs on
// ListenerConnection (empty selects the default job connection) and
// ListenerQueue (empty uses "default"); a worker must consume that queue.
type EventsSettings struct {
	Enabled            bool
	Config             events.Config
	QueuedListeners    bool
	ListenerConnection jobs.ConnectionName
	ListenerQueue      jobs.Queue
}
type AuditSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
	Config   audit.Config
}
type AttachmentSettings struct {
	Enabled bool
	Config  attachments.Config
}
type ReportSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Config   datatable.Config
}

// HealthSettings select readiness probes. ConfiguredConnections probes every
// database and Redis connection, plus the realtime hub when realtime is enabled.
// ConfiguredStorage stats a probe key on every disk; ConfiguredMail checks that
// every mailer still admits submissions (no transport round trip).
type HealthSettings struct {
	Enabled               bool
	Config                health.Config
	ConfiguredConnections bool
	ConfiguredStorage     bool
	ConfiguredMail        bool
}
type LocaleSettings struct {
	Enabled  bool
	Default  i18n.LocaleID
	Locales  []i18n.LocaleID `config:",json"`
	Fallback i18n.LocaleID
}

// OutboxSettings configures the shared publisher. Kernels selects which running
// kernels also run the publisher; empty runs it under every kernel. Prefer the
// worker and/or scheduler kernels so CLI commands and HTTP replicas do not poll.
type OutboxSettings struct {
	Enabled                                    bool
	Database                                   database.ConnectionName
	Schema                                     string
	Jobs                                       map[jobs.ConnectionName]outbox.Destination `config:",json"`
	Kernels                                    []foundation.KernelKind                    `config:",json"`
	MaxAttempts                                uint32
	RetryDelay, OperationTimeout, PollInterval time.Duration
	MaxRetryDelay, Jitter, MaxPollInterval     time.Duration
	MaxInFlight                                int
}

func DefaultOutboxSettings() OutboxSettings {
	c := publisher.DefaultConfig()
	return OutboxSettings{Schema: "public", MaxAttempts: c.MaxAttempts, RetryDelay: c.RetryDelay, OperationTimeout: c.OperationTimeout, PollInterval: c.PollInterval, MaxRetryDelay: c.MaxRetryDelay, Jitter: c.Jitter, MaxPollInterval: c.MaxPollInterval, MaxInFlight: c.MaxInFlight}
}
func (s OutboxSettings) runtime(source clock.Clock) publisher.Config {
	return publisher.Config{MaxAttempts: s.MaxAttempts, RetryDelay: s.RetryDelay, MaxRetryDelay: s.MaxRetryDelay, Jitter: s.Jitter, OperationTimeout: s.OperationTimeout, PollInterval: s.PollInterval, MaxPollInterval: s.MaxPollInterval, MaxInFlight: s.MaxInFlight, Clock: source}
}

type FeatureSettings struct {
	Idempotency   IdempotencySettings
	Auth          AuthSettings
	Events        EventsSettings
	Outbox        OutboxSettings
	Audit         AuditSettings
	Locales       LocaleSettings
	Extensions    StoreSettings
	Notifications StoreSettings
	Attachments   AttachmentSettings
	Reports       ReportSettings
	Health        HealthSettings
	Observability ObservabilitySettings
	// Maintenance schedules framework housekeeping (features.maintenance).
	Maintenance MaintenanceScheduleSettings
}

func DefaultFeatureSettings() FeatureSettings {
	return FeatureSettings{Idempotency: DefaultIdempotencySettings(), Auth: DefaultAuthSettings(), Events: EventsSettings{Config: events.DefaultConfig()}, Outbox: DefaultOutboxSettings(), Audit: AuditSettings{Schema: "public", Config: audit.DefaultConfig()}, Locales: LocaleSettings{Default: "en", Locales: []i18n.LocaleID{"en"}}, Extensions: DefaultExtensionSettings(), Notifications: DefaultNotificationSettings(), Attachments: AttachmentSettings{Config: attachments.DefaultConfig()}, Reports: ReportSettings{Config: datatable.DefaultConfig()}, Health: HealthSettings{Config: health.DefaultConfig(), ConfiguredConnections: true}, Observability: DefaultObservabilitySettings(), Maintenance: DefaultMaintenanceScheduleSettings()}
}
