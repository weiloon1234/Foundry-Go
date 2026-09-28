package infrastructure_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func supportingSettings(t *testing.T) infrastructure.Settings {
	t.Helper()
	s := memorySettings()
	sender, err := email.ParseAddress("Fixture <fixture@example.test>")
	if err != nil {
		t.Fatal(err)
	}
	mail := infrastructure.DefaultMailerSettings()
	mail.Driver = infrastructure.MemoryMail
	mail.Config.From = sender
	s.Mail.Mailers = infrastructure.Mailers{"default": mail, "second": mail}
	s.Jobs.Connections = infrastructure.JobConnections{"default": infrastructure.DefaultJobConnectionSettings(), "second": infrastructure.DefaultJobConnectionSettings()}
	s.HTTPClients.Clients = infrastructure.HTTPClients{"default": infrastructure.DefaultHTTPClientSettings("default"), "second": infrastructure.DefaultHTTPClientSettings("second")}
	s.PubSub.Connections = infrastructure.Brokers{"default": infrastructure.DefaultBrokerSettings(), "second": infrastructure.DefaultBrokerSettings()}
	s.Realtime.Connections = infrastructure.RealtimeConnections{"default": infrastructure.DefaultRealtimeConnectionSettings(), "second": infrastructure.DefaultRealtimeConnectionSettings()}
	return s
}
func TestSupportingNamedDefaultsOwnIndependentResources(t *testing.T) {
	s := supportingSettings(t)
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	a, b := built(t, plan), built(t, plan)
	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	x, y := services(t, a), services(t, b)
	m, _ := x.Mailer()
	ma, _ := x.Mailers.Mailer("default")
	mo, _ := x.Mailers.Mailer("second")
	mb, _ := y.Mailer()
	if m != ma || m == mo || m == mb {
		t.Fatal("mailer ownership")
	}
	j, _ := x.JobConnection()
	ja, _ := x.Jobs.Connection("default")
	jo, _ := x.Jobs.Connection("second")
	jb, _ := y.JobConnection()
	if j != ja || j == jo || j == jb {
		t.Fatal("job ownership")
	}
	h, _ := x.HTTPClient()
	ha, _ := x.HTTPClients.Client("default")
	ho, _ := x.HTTPClients.Client("second")
	hb, _ := y.HTTPClient()
	if h != ha || h == ho || h == hb {
		t.Fatal("HTTP ownership")
	}
	p, _ := x.Broker()
	pa, _ := x.Brokers.Broker("default")
	po, _ := x.Brokers.Broker("second")
	pb, _ := y.Broker()
	if p != pa || p == po || p == pb {
		t.Fatal("broker ownership")
	}
	w, _ := x.RealtimeConnection()
	wa, _ := x.Realtime.Connection("default")
	wo, _ := x.Realtime.Connection("second")
	wb, _ := y.RealtimeConnection()
	if w != wa || w == wo || w == wb {
		t.Fatal("realtime ownership")
	}
	if _, err := x.Mailers.Mailer("absent"); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
	topic := pubsub.Define[string, string]("configured", 1, keyspace.StringKeys[string]())
	first, err := topic.Bind(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := topic.Bind(po)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := first.Subscribe(t.Context(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close(context.Background())
	if count, err := second.Publish(t.Context(), "x", "elsewhere"); err != nil || count != 0 {
		t.Fatal("broker namespace leaked", err)
	}
	if count, err := first.Publish(t.Context(), "x", "here"); err != nil || count != 1 {
		t.Fatal("broker publication failed", err)
	}
	if _, err := sub.Receive(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	recipient, _ := email.ParseAddress("recipient@example.test")
	if _, err := mb.Send(t.Context(), mb.Message("still alive", recipient).Text("hello"), email.SendOptions{}); err != nil {
		t.Fatal("other application closed", err)
	}
	if _, err := m.Send(t.Context(), m.Message("closed", recipient).Text("hello"), email.SendOptions{}); err == nil {
		t.Fatal("mailer not drained")
	}
}
func TestConfiguredHTTPClientsKeepSeparateOriginsAndHeaders(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { _, _ = io.WriteString(w, r.Header.Get("X-Client")) }))
	defer server.Close()
	s := supportingSettings(t)
	for name, c := range s.HTTPClients.Clients {
		c.Config.BaseURL = server.URL
		c.Config.Headers = stdhttp.Header{"X-Client": {string(name)}}
		s.HTTPClients.Clients[name] = c
	}
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	c := s.HTTPClients.Clients["default"]
	c.Config.Headers.Set("X-Client", "mutated")
	app := built(t, plan)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []httpclient.Name{"default", "second"} {
		client, err := services(t, app).HTTPClients.Client(name)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(t.Context(), client.Get("/"))
		if err != nil {
			t.Fatal(err)
		}
		body, err := response.Text()
		if err != nil || body != string(name) {
			t.Fatal("HTTP configuration leaked", err)
		}
	}
}
func TestSupportingRejectsUnknownDriversNamesAndReferences(t *testing.T) {
	for name, change := range map[string]func(*infrastructure.Settings){
		"mail-default": func(s *infrastructure.Settings) { s.Mail.Default = "absent" },
		"mail-driver": func(s *infrastructure.Settings) {
			c := s.Mail.Mailers["default"]
			c.Driver = "absent"
			s.Mail.Mailers["default"] = c
		},
		"job-driver": func(s *infrastructure.Settings) {
			c := s.Jobs.Connections["default"]
			c.Driver = "absent"
			s.Jobs.Connections["default"] = c
		},
		"job-redis": func(s *infrastructure.Settings) {
			c := s.Jobs.Connections["default"]
			c.Driver = infrastructure.RedisJobs
			s.Jobs.Connections["default"] = c
		},
		"http-name": func(s *infrastructure.Settings) {
			c := s.HTTPClients.Clients["default"]
			c.Config.Name = "second"
			s.HTTPClients.Clients["default"] = c
		},
		"broker-driver": func(s *infrastructure.Settings) {
			c := s.PubSub.Connections["default"]
			c.Driver = "absent"
			s.PubSub.Connections["default"] = c
		},
		"realtime-driver": func(s *infrastructure.Settings) {
			c := s.Realtime.Connections["default"]
			c.Driver = "absent"
			s.Realtime.Connections["default"] = c
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := supportingSettings(t)
			change(&s)
			if _, err := infrastructure.Configure(s); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

type borrowedMailer struct {
	calls  int
	closed bool
}

func (d *borrowedMailer) Send(_ context.Context, o email.Outbound) (email.Receipt, error) {
	if d.closed {
		return email.Receipt{}, email.Transient
	}
	d.calls++
	return email.Receipt{MessageID: "fixture"}, o.Validate()
}
func (d *borrowedMailer) Close() { d.closed = true }
func TestCustomMailDriverIsBorrowedAndUsesConfiguredSender(t *testing.T) {
	s := supportingSettings(t)
	c := s.Mail.Mailers["default"]
	c.Driver = "custom"
	s.Mail.Mailers["default"] = c
	driver := &borrowedMailer{}
	plan, err := infrastructure.Configure(s, infrastructure.WithMailDriver("custom", driver))
	if err != nil {
		t.Fatal(err)
	}
	app := built(t, plan)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	mailer, _ := services(t, app).Mailer()
	recipient, _ := email.ParseAddress("to@example.test")
	message := mailer.Message("subject", recipient).Text("body")
	if message.From() != c.Config.From {
		t.Fatal("configured sender lost")
	}
	if _, err := mailer.Send(t.Context(), message, email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if driver.closed || driver.calls != 1 {
		t.Fatal("custom borrowed driver ownership changed")
	}
}
