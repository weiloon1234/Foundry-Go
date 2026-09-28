package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/websocket"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

func resolveSupporting(r foundation.Resolver, s Settings, services *Services) error {
	if len(s.Mail.Mailers) > 0 {
		entries := make([]email.NamedMailer, 0, len(s.Mail.Mailers))
		for _, name := range keys(s.Mail.Mailers) {
			value, err := foundation.Resolve(r, MailKey(name))
			if err != nil {
				return err
			}
			entries = append(entries, email.NamedMailer{Name: name, Value: value})
		}
		var err error
		services.Mailers, err = email.NewMailers(s.Mail.Default, entries...)
		if err != nil {
			return err
		}
	}
	if len(s.Jobs.Connections) > 0 {
		entries := make([]jobs.NamedConnection, 0, len(s.Jobs.Connections))
		for _, name := range keys(s.Jobs.Connections) {
			value, err := foundation.Resolve(r, JobKey(name))
			if err != nil {
				return err
			}
			entries = append(entries, jobs.NamedConnection{Name: name, Value: value})
		}
		var err error
		services.Jobs, err = jobs.NewConnections(s.Jobs.Default, entries...)
		if err != nil {
			return err
		}
	}
	if len(s.HTTPClients.Clients) > 0 {
		entries := make([]httpclient.NamedClient, 0, len(s.HTTPClients.Clients))
		for _, name := range keys(s.HTTPClients.Clients) {
			value, err := foundation.Resolve(r, HTTPClientKey(name))
			if err != nil {
				return err
			}
			entries = append(entries, httpclient.NamedClient{Name: name, Value: value})
		}
		var err error
		services.HTTPClients, err = httpclient.NewClients(s.HTTPClients.Default, entries...)
		if err != nil {
			return err
		}
	}
	if len(s.PubSub.Connections) > 0 {
		entries := make([]pubsub.NamedBroker, 0, len(s.PubSub.Connections))
		for _, name := range keys(s.PubSub.Connections) {
			value, err := foundation.Resolve(r, BrokerKey(name))
			if err != nil {
				return err
			}
			entries = append(entries, pubsub.NamedBroker{Name: name, Value: value})
		}
		var err error
		services.Brokers, err = pubsub.NewBrokers(s.PubSub.Default, entries...)
		if err != nil {
			return err
		}
	}
	if len(s.Realtime.Connections) > 0 {
		entries := make([]websocket.NamedConnection, 0, len(s.Realtime.Connections))
		for _, name := range keys(s.Realtime.Connections) {
			value, err := foundation.Resolve(r, RealtimeKey(name))
			if err != nil {
				return err
			}
			entries = append(entries, websocket.NamedConnection{Name: name, Value: value})
		}
		var err error
		services.Realtime, err = websocket.NewConnections(s.Realtime.Default, entries...)
		if err != nil {
			return err
		}
	}
	if s.Coordination.Enabled {
		var err error
		services.Coordination, err = foundation.Resolve(r, CoordinationKey)
		if err != nil {
			return err
		}
	}
	return nil
}
func (p *Plan) supporting() {
	for _, name := range keys(p.settings.Mail.Mailers) {
		p.mailer(name, p.settings.Mail.Mailers[name])
	}
	for _, name := range keys(p.settings.Jobs.Connections) {
		p.jobConnection(name, p.settings.Jobs.Connections[name])
	}
	for _, name := range keys(p.settings.HTTPClients.Clients) {
		p.providers = append(p.providers, httpclient.Module(HTTPClientProvider(name), HTTPClientKey(name), p.settings.HTTPClients.Clients[name].Config, nil, nil))
	}
	for _, name := range keys(p.settings.PubSub.Connections) {
		p.broker(name, p.settings.PubSub.Connections[name])
	}
	for _, name := range keys(p.settings.Realtime.Connections) {
		p.realtime(name, p.settings.Realtime.Connections[name])
	}
	if p.settings.Coordination.Enabled {
		p.coordination(p.settings.Coordination)
	}
}
