package infrastructure_test

import (
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/secret"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfiguredMailersKeepProviderCredentialsAndSenderSeparate(t *testing.T) {
	type submission struct{ authorization, from string }
	submissions := make(chan submission, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emails" || r.Method != "POST" {
			w.WriteHeader(400)
			return
		}
		var payload struct {
			From string `json:"from"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(400)
			return
		}
		submissions <- submission{r.Header.Get("Authorization"), payload.From}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"fixture-accepted"}`)
	}))
	defer server.Close()
	s := supportingSettings(t)
	for name, c := range s.Mail.Mailers {
		c.Driver = infrastructure.ResendMail
		c.API.Endpoint = server.URL
		c.API.Token = secret.New("credential-" + string(name))
		c.Config.From, _ = email.ParseAddress(string(name) + "@example.test")
		s.Mail.Mailers[name] = c
	}
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	app := built(t, plan)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	recipient, _ := email.ParseAddress("recipient@example.test")
	for _, name := range []email.MailerName{"default", "second"} {
		mailer, err := services(t, app).Mailers.Mailer(name)
		if err != nil {
			t.Fatal(err)
		}
		result, err := mailer.Send(t.Context(), mailer.Message("configured", recipient).Text("body"), email.SendOptions{})
		if err != nil || !result.Accepted {
			t.Fatal("mail submission failed", err)
		}
		got := <-submissions
		if got.authorization != "Bearer credential-"+string(name) || got.from != s.Mail.Mailers[name].Config.From.Header() {
			t.Fatal("provider settings crossed mailers")
		}
	}
}
