package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// Mailer returns the default mailer or a registry error.
func (s *Services) Mailer() (*email.Mailer, error) {
	if s == nil {
		return (*email.Mailers)(nil).Default()
	}
	return s.Mailers.Default()
}

// MailProvider returns the provider identifier for a named mailer.
func MailProvider(name email.MailerName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.mail." + string(name))
}

// MailKey returns the typed service key for a named mailer.
func MailKey(name email.MailerName) foundation.Key[*email.Mailer] {
	return foundation.NewKey[*email.Mailer](string(MailProvider(name)))
}

// JobConnection returns the default job connection or a registry error.
func (s *Services) JobConnection() (*jobs.Connection, error) {
	if s == nil {
		return (*jobs.Connections)(nil).Default()
	}
	return s.Jobs.Default()
}

// JobProvider returns the provider identifier for a named job connection.
func JobProvider(name jobs.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.job." + string(name))
}

// JobKey returns the typed service key for a named job connection.
func JobKey(name jobs.ConnectionName) foundation.Key[*jobs.Connection] {
	return foundation.NewKey[*jobs.Connection](string(JobProvider(name)))
}

// HTTPClient returns the default HTTP client or a registry error.
func (s *Services) HTTPClient() (*httpclient.Client, error) {
	if s == nil {
		return (*httpclient.Clients)(nil).Default()
	}
	return s.HTTPClients.Default()
}

// HTTPClientProvider returns the provider identifier for a named HTTP client.
func HTTPClientProvider(name httpclient.Name) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.httpclient." + string(name))
}

// HTTPClientKey returns the typed service key for a named HTTP client.
func HTTPClientKey(name httpclient.Name) foundation.Key[*httpclient.Client] {
	return foundation.NewKey[*httpclient.Client](string(HTTPClientProvider(name)))
}

// Broker returns the default pub/sub broker or a registry error.
func (s *Services) Broker() (*pubsub.Broker, error) {
	if s == nil {
		return (*pubsub.Brokers)(nil).Default()
	}
	return s.Brokers.Default()
}

// BrokerProvider returns the provider identifier for a named pub/sub broker.
func BrokerProvider(name pubsub.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.broker." + string(name))
}

// BrokerKey returns the typed service key for a named pub/sub broker.
func BrokerKey(name pubsub.ConnectionName) foundation.Key[*pubsub.Broker] {
	return foundation.NewKey[*pubsub.Broker](string(BrokerProvider(name)))
}

// RealtimeConnection returns the default realtime connection or a registry error.
func (s *Services) RealtimeConnection() (*websocket.BackendConnection, error) {
	if s == nil {
		return (*websocket.Connections)(nil).Default()
	}
	return s.Realtime.Default()
}

// RealtimeProvider returns the provider identifier for a named realtime connection.
func RealtimeProvider(name websocket.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.realtime." + string(name))
}

// RealtimeKey returns the typed service key for a named realtime connection.
func RealtimeKey(name websocket.ConnectionName) foundation.Key[*websocket.BackendConnection] {
	return foundation.NewKey[*websocket.BackendConnection](string(RealtimeProvider(name)))
}

const CoordinationProvider foundation.ProviderID = "foundry.infrastructure.coordination"

var CoordinationKey = foundation.NewKey[*lease.Manager](string(CoordinationProvider))

// Leases returns the coordination manager, or a missing fault if unconfigured.
func (s *Services) Leases() (*lease.Manager, error) {
	if s == nil || s.Coordination == nil {
		return nil, safeerror.NewFault(fault.Missing, "coordination is not configured")
	}
	return s.Coordination, nil
}

// JobDispatcherKey returns the typed dispatcher key for a named job connection.
func JobDispatcherKey(name jobs.ConnectionName) foundation.Key[*jobs.Dispatcher] {
	return foundation.NewKey[*jobs.Dispatcher](string(JobProvider(name)) + ".dispatcher")
}
