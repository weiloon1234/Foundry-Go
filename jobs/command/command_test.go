package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	jobcommand "github.com/weiloon1234/Foundry-Go/jobs/command"
	"github.com/weiloon1234/Foundry-Go/lease"
	jobtest "github.com/weiloon1234/Foundry-Go/testkit/jobs"
)

type payload struct {
	Secret string `json:"secret"`
}

func TestCommandValidatesBeforeServiceStartup(t *testing.T) {
	for _, args := range [][]string{
		nil, {"jobs"}, {"jobs", "unknown"}, {"jobs", "retry"}, {"jobs", "inspect"},
		{"jobs", "failed", "--limit", "0"}, {"jobs", "failed", "--limit", "101"},
		{"jobs", "failed", "--queue", ""}, {"jobs", "failed", "--format", "xml"},
		{"jobs", "failed", "unexpected"}, {"jobs", "failed", "--connection", "not a name"},
		{"jobs", "inspect", "--id", "bad"}, {"jobs", "retry", "--token", "bad"},
	} {
		if _, err := jobcommand.Parse(args, io.Discard); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	if _, err := jobcommand.Parse([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if err := (jobcommand.Command{}).Run(context.Background(), nil, io.Discard); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCommandRetryReconcilesOutputFailureAndNeverEmitsPayload(t *testing.T) {
	d := jobs.Define[payload]("commands.work", 1, jobs.DefaultPolicy("work"))
	declaration, err := d.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := jobtest.New(t, clock.System{}, declaration)
	connection, err := jobs.NewConnection(h.Dispatcher, "work")
	if err != nil {
		t.Fatal(err)
	}
	connections, err := jobs.NewConnections("primary", jobs.NamedConnection{Name: "primary", Value: connection})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := d.On(connection)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := bound.Dispatch(t.Context(), payload{Secret: "do-not-emit"}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	key, err := jobs.NewKey(h.Namespace, "work")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	found, err := h.Backend.JobReserve(t.Context(), key, owner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok := found.Get()
	if !ok {
		t.Fatal("job not found")
	}
	if _, err := h.Backend.JobStart(t.Context(), key, claim.Ownership); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Backend.JobFinish(t.Context(), key, claim.Ownership, jobs.Result{State: jobs.Failed, Reason: jobs.HandlerFailed}); err != nil {
		t.Fatal(err)
	}
	parse := func(args ...string) jobcommand.Command {
		t.Helper()
		c, err := jobcommand.Parse(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	var output bytes.Buffer
	c := parse("jobs", "failed", "--format", "json")
	if err := c.Run(t.Context(), connections, &output); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Jobs []jobs.Summary `json:"jobs"`
	}
	if err := json.Unmarshal(output.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Jobs) != 1 || page.Jobs[0].RetryToken == "" || strings.Contains(output.String(), "do-not-emit") {
		t.Fatal("unsafe or incomplete inspection")
	}
	token := page.Jobs[0].RetryToken
	c = parse("jobs", "retry", "--id", receipt.ID.String(), "--token", string(token), "--format", "json")
	if err := c.Run(t.Context(), connections, failedWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("lost output error", err)
	}
	output.Reset()
	if err := c.Run(t.Context(), connections, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"changed":false`) || !strings.Contains(output.String(), `"acceptance":"confirmed"`) {
		t.Fatal("ambiguous operation was not reconciled")
	}
	foundRecord, err := bound.Inspect(t.Context(), receipt.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	record, ok := foundRecord.Get()
	if !ok || record.Retries != 1 || record.State != jobs.Waiting {
		t.Fatal("command replayed work")
	}
	c = parse("jobs", "failed", "--connection", "absent")
	if err := c.Run(t.Context(), connections, io.Discard); err == nil {
		t.Fatal("unknown connection fell back")
	}
	if _, err := jobcommand.Parse([]string{"jobs", "retry", "--id", receipt.ID.String()}, io.Discard); err == nil {
		t.Fatal("missing retry token accepted")
	}
}
