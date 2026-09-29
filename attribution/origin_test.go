package attribution_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

type member struct {
	ID       int64
	Password string
}

func (m member) FoundryIdentity() (model.Identity, error) {
	return model.NewReference[member]("members", m.ID, codec.Signed[int64]()).Identity()
}

func TestOriginSnapshotsStoredIdentityAndRequestWithoutCredentials(t *testing.T) {
	user := member{ID: 7, Password: "must-not-copy"}
	origin, err := (attribution.Origin{}).WithModel(user)
	if err != nil {
		t.Fatal(err)
	}
	origin, err = origin.WithGuard("session")
	if err != nil {
		t.Fatal(err)
	}
	request := attribution.Request{ID: "request-7", IP: netip.MustParseAddr("203.0.113.7"), UserAgent: "Foundry consumer"}
	origin, err = origin.WithRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, err := attribution.WithContext(parent, origin)
	if err != nil {
		t.Fatal(err)
	}
	user.ID = 8
	request.UserAgent = "changed"
	captured := attribution.FromContext(ctx)
	identity, present := captured.Model()
	key, err := identity.KeyJSON()
	if !present || err != nil || key != `{"k":"int","t":"7"}` || captured.Request().UserAgent != "Foundry consumer" || captured.Guard() != "session" {
		t.Fatal("origin was not an immutable stored-value snapshot", err)
	}
	data, err := json.Marshal(captured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), user.Password) {
		t.Fatal("attribution copied a credential")
	}
	var decoded attribution.Origin
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != captured {
		t.Fatal("origin serialization changed metadata", err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if fmt.Sprintf(format, captured) != "attribution origin" {
			t.Fatal("origin formatting exposed metadata")
		}
	}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("attribution detached cancellation")
	}
	system, err := origin.WithSystem("scheduler.cleanup")
	if err != nil {
		t.Fatal(err)
	}
	if _, present := system.Model(); present || system.Guard() != "" || system.System() != "scheduler.cleanup" || origin.System() != "" {
		t.Fatal("system transition retained a human subject or changed original")
	}
}

func TestOriginValidatesBoundariesAndPreservesExistingValueOnDecodeFailure(t *testing.T) {
	if err := (attribution.Origin{}).Validate(); err != nil {
		t.Fatal("anonymous origin invalid", err)
	}
	valid, err := (attribution.Origin{}).WithSystem("test.runner")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"subject":{"model":"members","key":{"k":"int","t":"7"}},"system":"test.runner","request":{}}`,
		`{"system":"test.runner","guard":"session","request":{}}`,
		`{"system":"bad system","request":{}}`,
		`{"request":{"id":"line\nbreak"}}`,
		`{"request":{},"credentials":"secret"}`, `{"request":{},"request":{}}`,
	} {
		destination := valid
		if err := json.Unmarshal([]byte(raw), &destination); err == nil || destination != valid {
			t.Fatal("invalid origin changed existing metadata", raw, err)
		}
	}
	if _, err := (attribution.Origin{}).WithRequest(attribution.Request{UserAgent: strings.Repeat("x", attribution.MaxUserAgentBytes+1)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded request metadata accepted", err)
	}
	for _, raw := range []string{strings.Repeat("é", attribution.MaxUserAgentBytes), "a\tb\r\nc\x00\u0085d", "bad\xff\xfeutf8", "plain agent"} {
		sanitized := attribution.SanitizeUserAgent(raw)
		if _, err := (attribution.Origin{}).WithRequest(attribution.Request{UserAgent: sanitized}); err != nil || len(sanitized) > attribution.MaxUserAgentBytes {
			t.Fatalf("sanitized agent %q remains invalid: %v", sanitized, err)
		}
	}
	if attribution.SanitizeUserAgent("a\tb\u0085c") != "abc" || attribution.SanitizeUserAgent("x\xffy") != "x\uFFFDy" || attribution.SanitizeUserAgent("plain agent") != "plain agent" {
		t.Fatal("sanitization changed meaning")
	}
	var nilMember *member
	if _, err := (attribution.Origin{}).WithModel(nilMember); !errors.Is(err, fault.Invalid) {
		t.Fatal("typed nil subject accepted", err)
	}
	if _, err := attribution.WithContext(nil, valid); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted", err)
	}
}
