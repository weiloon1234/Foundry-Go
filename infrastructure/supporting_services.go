package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func (s *Services) Mailer() (*email.Mailer, error) {
	if s == nil {
		return (*email.Mailers)(nil).Default()
	}
	return s.Mailers.Default()
}
func MailProvider(name email.MailerName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.mail." + string(name))
}
func MailKey(name email.MailerName) foundation.Key[*email.Mailer] {
	return foundation.NewKey[*email.Mailer](string(MailProvider(name)))
}
func (s *Services) JobConnection() (*jobs.Connection, error) {
	if s == nil {
		return (*jobs.Connections)(nil).Default()
	}
	return s.Jobs.Default()
}
func JobProvider(name jobs.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.job." + string(name))
}
func JobKey(name jobs.ConnectionName) foundation.Key[*jobs.Connection] {
	return foundation.NewKey[*jobs.Connection](string(JobProvider(name)))
}
func (s *Services) HTTPClient() (*httpclient.Client, error) {
	if s == nil {
		return (*httpclient.Clients)(nil).Default()
	}
	return s.HTTPClients.Default()
}
func HTTPClientProvider(name httpclient.Name) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.httpclient." + string(name))
}
func HTTPClientKey(name httpclient.Name) foundation.Key[*httpclient.Client] {
	return foundation.NewKey[*httpclient.Client](string(HTTPClientProvider(name)))
}
func (s *Services) Broker() (*pubsub.Broker, error) {
	if s == nil {
		return (*pubsub.Brokers)(nil).Default()
	}
	return s.Brokers.Default()
}
func BrokerProvider(name pubsub.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.broker." + string(name))
}
func BrokerKey(name pubsub.ConnectionName) foundation.Key[*pubsub.Broker] {
	return foundation.NewKey[*pubsub.Broker](string(BrokerProvider(name)))
}
func (s *Services) RealtimeConnection() (*websocket.BackendConnection, error) {
	if s == nil {
		return (*websocket.Connections)(nil).Default()
	}
	return s.Realtime.Default()
}
func RealtimeProvider(name websocket.ConnectionName) foundation.ProviderID {
	return foundation.ProviderID("foundry.infrastructure.realtime." + string(name))
}
func RealtimeKey(name websocket.ConnectionName) foundation.Key[*websocket.BackendConnection] {
	return foundation.NewKey[*websocket.BackendConnection](string(RealtimeProvider(name)))
}

const CoordinationProvider foundation.ProviderID = "foundry.infrastructure.coordination"

var CoordinationKey = foundation.NewKey[*lease.Manager](string(CoordinationProvider))

func (s *Services) Leases() (*lease.Manager, error) {
	if s == nil || s.Coordination == nil {
		return nil, fault.New(fault.Missing, "coordination is not configured")
	}
	return s.Coordination, nil
}
func JobDispatcherKey(name jobs.ConnectionName) foundation.Key[*jobs.Dispatcher] {
	return foundation.NewKey[*jobs.Dispatcher](string(JobProvider(name)) + ".dispatcher")
}
