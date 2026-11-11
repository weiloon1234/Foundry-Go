package httpcapture

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func outboundConfig() OutboundConfig {
	limits := testConfig()
	limits.LaneShare = OutboundLaneShare
	return OutboundConfig{Limits: limits, Inspect: func(string, string) Skip { return Kept }}
}

func facts(t *testing.T, method, raw string) RequestFacts {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return RequestFacts{Method: method, URL: u, Headers: http.Header{}}
}

func TestDenyListRulesMatchEverySetField(t *testing.T) {
	c := outboundConfig()
	c.Rules = []Rule{
		{Client: "crawler"},
		{Host: "*.ingest.sentry.io"},
		{Client: "visibility", Method: "GET", PathPrefix: "/api/v1/models"},
		{Host: "hooks.slack.com", PathPrefix: "/services"},
	}
	o, err := NewOutbound(c)
	if err != nil {
		t.Fatal(err)
	}
	if o.Lane("crawler") != nil || o.Lane("crawler") != nil || !o.Claimed("crawler") || o.Claimed("absent") {
		t.Fatal("client-only rule did not exclude the whole client")
	}
	visibility, stripe := o.Lane("visibility"), o.Lane("stripe")
	if visibility == nil || o.Lane("visibility") != visibility || len(visibility.rules) != 3 || len(stripe.rules) != 2 {
		t.Fatal("lanes are not shared per name or kept another client's rules")
	}
	for _, test := range []struct {
		lane     *ClientLane
		method   string
		url      string
		excluded bool
	}{
		{stripe, "POST", "https://o1.ingest.sentry.io/api/1/envelope", true},
		{stripe, "POST", "https://ingest.sentry.io/api", false},
		{stripe, "POST", "https://hooks.slack.com/services/T/B/X", true},
		{stripe, "POST", "https://hooks.slack.com/servicesX", false},
		{stripe, "POST", "https://HOOKS.slack.com/services", true},
		{visibility, "GET", "https://api.test/api/v1/models", true},
		{visibility, "GET", "https://api.test/api/v1/models/gpt", true},
		{visibility, "GET", "https://api.test/api/v1/modelsx", false},
		{visibility, "POST", "https://api.test/api/v1/models", false},
		{stripe, "GET", "https://api.test/api/v1/models", false},
	} {
		if excluded := test.lane.Begin(context.Background(), facts(t, test.method, test.url)) == nil; excluded != test.excluded {
			t.Errorf("%s %s excluded=%v, want %v", test.method, test.url, excluded, test.excluded)
		}
	}
	if stripe.Begin(context.Background(), RequestFacts{Method: "GET"}) != nil {
		t.Fatal("a request without a URL was captured")
	}
	var nilLane *ClientLane
	var nilOperation *Operation
	if nilLane.Begin(context.Background(), facts(t, "GET", "https://x.test")) != nil || nilOperation.Attempt(1) != nil {
		t.Fatal("an uncaptured client produced capture state")
	}
	nilOperation.NotSent(ResultOverloaded)
}

func TestTapKeepsBoundedPrefixAndRefusedGrowthTurnsCountOnly(t *testing.T) {
	c := outboundConfig()
	o, _ := NewOutbound(c)
	operation := o.Lane("api").Begin(context.Background(), facts(t, "GET", "https://api.test/x"))
	a := operation.Attempt(1)
	tap := &a.response
	tap.start(a.reservation, 8, Kept, -1)
	tap.Observe([]byte("0123"))
	tap.Observe([]byte("456789"))
	tap.End(TapEOF)
	tap.End(TapFailed)
	body := tap.settle(BodyCapture{}, 10)
	if string(body.Bytes) != "01234567" || !body.Over || !body.Complete || body.Skip != Kept {
		t.Fatal("tap did not keep a bounded prefix", string(body.Bytes), body.Over, body.Complete)
	}

	c.Limits.RetainedBytes = int64(OutboundLaneShare * (c.Limits.BodyBytes + 1 + MetadataBytes))
	tight, _ := NewOutbound(c)
	lane := tight.Lane("api")
	a = lane.Begin(context.Background(), facts(t, "GET", "https://api.test/x")).Attempt(1)
	share := tight.recorder.laneBytes
	if !a.reservation.Grow(share - OutboundMetadataBytes - 16) {
		t.Fatal("setup growth refused")
	}
	tap = &a.response
	tap.start(a.reservation, c.Limits.BodyBytes, Kept, 1000)
	tap.Observe(make([]byte, 100))
	body = tap.settle(BodyCapture{}, 100)
	if body.Skip != SkipMemory || body.Bytes != nil {
		t.Fatal("refused growth did not turn the tap count-only", body.Skip)
	}
	if tight.recorder.Drops() != ([DropReasons]uint64{}) {
		t.Fatal("refused growth lost a record")
	}
}
