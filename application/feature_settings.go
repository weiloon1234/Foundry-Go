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
	"github.com/weiloon1234/Foundry-Go/telemetry"
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

// DefaultExtensionSettings returns default extension store settings.
func DefaultExtensionSettings() StoreSettings {
	c := extensions.DefaultConfig()
	return StoreSettings{Schema: c.Schema, MaxActive: c.MaxActive, Timeout: c.Timeout}
}

// DefaultNotificationSettings returns default notification store settings.
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

// AuditSettings configures the audit recorder and its database schema.
type AuditSettings struct {
	Enabled  bool
	Database database.ConnectionName
	Schema   string
	Config   audit.Config
}

// TelemetrySettings selects persisted process telemetry. Disabled telemetry
// creates no collector, reporter, observer, polling task or migration target.
// Empty Database selects the default connection; empty Schema selects its
// primary schema, or public for an unscoped connection. Empty Kernels selects
// HTTP, worker and scheduler. Alert thresholds remain explicit in Config.
type TelemetrySettings struct {
	// Enabled turns on persisted telemetry. It requires observability to be enabled.
	Enabled bool
	// Database names the database connection that stores telemetry. Empty selects the default connection.
	Database database.ConnectionName
	// Schema is the schema that holds the telemetry tables. Empty selects the connection's primary schema,
	// or public when that has none.
	Schema string
	// Kernels lists the kernels whose processes run the telemetry engine; empty selects HTTP, worker and
	// scheduler. Each must be a known kernel kind, listed once.
	Kernels []foundation.KernelKind `config:",json"`
	// Collector configures the in-memory collector; it is validated at assembly.
	Collector telemetry.CollectorConfig
	// Config configures the engine's timing, retention, alerts and read limits; it is validated at
	// assembly.
	Config telemetry.Config
}

// AttachmentSettings configures the attachment feature.
type AttachmentSettings struct {
	Enabled bool
	Config  attachments.Config
}

// ReportSettings configures the datatable report feature and its database connection.
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

// LocaleSettings configures supported locales and their default identifier.
type LocaleSettings struct {
	Enabled bool
	Default i18n.LocaleID
	Locales []i18n.LocaleID `config:",json"`
}

// OutboxSettings configures the shared publisher. Kernels selects which running
// kernels also run the publisher; empty runs it under every kernel. Prefer the
// worker and/or scheduler kernels so CLI commands and HTTP replicas do not poll.
type OutboxSettings struct {
	// Enabled registers the outbox publisher and its migrations; the publisher fields are validated only when it is true.
	Enabled bool
	// Database is the database connection that holds the outbox table.
	Database database.ConnectionName
	// Schema is the schema of the outbox table within Database; the default is public.
	Schema string
	// Jobs maps a job connection (the key, which must be configured under services.jobs.connections) to the outbox destination (the value) that carries that connection's jobs. At most 128 entries; each destination must be a semantic name.
	Jobs map[jobs.ConnectionName]outbox.Destination `config:",json"`
	// Events maps each declared EventOutbox destination (the key) to the durable job
	// connection (the value) that delivers it; the reverse direction of Jobs. Empty connection selects the
	// default job connection. Every connection used here, explicit or default, must be a Redis job connection. Each destination needs an
	// EventOutbox declaration and each declaration needs an entry here; at most 128 entries.
	// Each connection uses its default queue and jobs.DefaultPolicy for the framework event delivery job.
	Events  map[outbox.Destination]jobs.ConnectionName `config:",json"`
	Kernels []foundation.KernelKind                    `config:",json"`
	// MaxAttempts is the number of publication attempts per row before the publisher gives up on it; from 1 to 1,000,000, default 1000.
	MaxAttempts                                uint32
	RetryDelay, OperationTimeout, PollInterval time.Duration
	MaxRetryDelay, Jitter, MaxPollInterval     time.Duration
	// MaxInFlight bounds both the rows claimed per transaction and the concurrent publications; from 1 to 1024, default 16.
	MaxInFlight int
}

// DefaultOutboxSettings returns default outbox publisher settings in the public schema.
func DefaultOutboxSettings() OutboxSettings {
	c := publisher.DefaultConfig()
	return OutboxSettings{Schema: "public", MaxAttempts: c.MaxAttempts, RetryDelay: c.RetryDelay, OperationTimeout: c.OperationTimeout, PollInterval: c.PollInterval, MaxRetryDelay: c.MaxRetryDelay, Jitter: c.Jitter, MaxPollInterval: c.MaxPollInterval, MaxInFlight: c.MaxInFlight}
}
func (s OutboxSettings) runtime(source clock.Clock) publisher.Config {
	return publisher.Config{MaxAttempts: s.MaxAttempts, RetryDelay: s.RetryDelay, MaxRetryDelay: s.MaxRetryDelay, Jitter: s.Jitter, OperationTimeout: s.OperationTimeout, PollInterval: s.PollInterval, MaxPollInterval: s.MaxPollInterval, MaxInFlight: s.MaxInFlight, Clock: source}
}

// ExtensionCleanupSettings makes the cleanup of models with extension slots
// deleted through a database connection other than the extension store's
// crash-durable (features.extension_cleanup).
type ExtensionCleanupSettings struct {
	// Jobs names the job connection that runs the cleanup job. The deletion
	// enqueues it in its own transaction through the outbox, so it needs a
	// destination in features.outbox.jobs and a worker consuming the
	// connection's default queue. Empty keeps process-local after-commit cleanup.
	Jobs jobs.ConnectionName
}

// FeatureSettings groups configuration for application features.
type FeatureSettings struct {
	Idempotency      IdempotencySettings
	Auth             AuthSettings
	Events           EventsSettings
	Outbox           OutboxSettings
	Audit            AuditSettings
	Locales          LocaleSettings
	Extensions       StoreSettings
	ExtensionCleanup ExtensionCleanupSettings
	Notifications    StoreSettings
	Attachments      AttachmentSettings
	Reports          ReportSettings
	Health           HealthSettings
	Observability    ObservabilitySettings
	Telemetry        TelemetrySettings
	// Maintenance schedules framework housekeeping (features.maintenance).
	Maintenance MaintenanceScheduleSettings
	// WebhookCapture configures prefixes declared with Builder.CaptureWebhooks.
	WebhookCapture WebhookCaptureSettings
	// OutboundCapture configures capture of every httpclient.Module call.
	OutboundCapture OutboundCaptureSettings
}

// DefaultFeatureSettings returns default settings for application features.
func DefaultFeatureSettings() FeatureSettings {
	return FeatureSettings{Idempotency: DefaultIdempotencySettings(), Auth: DefaultAuthSettings(), Events: EventsSettings{Config: events.DefaultConfig()}, Outbox: DefaultOutboxSettings(), Audit: AuditSettings{Schema: "public", Config: audit.DefaultConfig()}, Locales: LocaleSettings{Default: "en", Locales: []i18n.LocaleID{"en"}}, Extensions: DefaultExtensionSettings(), Notifications: DefaultNotificationSettings(), Attachments: AttachmentSettings{Config: attachments.DefaultConfig()}, Reports: ReportSettings{Config: datatable.DefaultConfig()}, Health: HealthSettings{Config: health.DefaultConfig(), ConfiguredConnections: true}, Observability: DefaultObservabilitySettings(), Maintenance: DefaultMaintenanceScheduleSettings(), WebhookCapture: DefaultWebhookCaptureSettings(), OutboundCapture: DefaultOutboundCaptureSettings()}
}
