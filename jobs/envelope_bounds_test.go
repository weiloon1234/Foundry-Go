package jobs_test

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

type largePayload struct {
	Text string `json:"text"`
}

func TestWorkflowTransportSupportsMultipleLargeMembers(t *testing.T) {
	d := jobs.Define[largePayload]("large", 1, jobs.DefaultPolicy("default"))
	var steps []jobs.Step
	for range 3 {
		p, err := d.Capture(t.Context(), largePayload{Text: strings.Repeat("x", 400<<10)}, jobs.Options[largePayload]{})
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, p.Step())
	}
	workflow, err := jobs.NewChain(steps...)
	if err != nil {
		t.Fatal(err)
	}
	data, err := workflow.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := jobs.DecodeWorkflow(data)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID() != workflow.ID() || len(restored.Steps()) != 3 {
		t.Fatal("large workflow changed")
	}
}
