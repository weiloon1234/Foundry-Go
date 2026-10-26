package querycore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func planDocument(analyze bool) []byte {
	node := PlanNode{NodeType: "Result", PlanRows: 1}
	envelope := PlanEnvelope{Plan: node, PlanningTime: value.Set(0.1)}
	if analyze {
		envelope.Plan.ActualRows = value.Set(1.0)
		envelope.Plan.ActualLoops = value.Set(1.0)
		envelope.Plan.ActualStartupTime = value.Set(0.0)
		envelope.Plan.ActualTotalTime = value.Set(0.1)
		envelope.ExecutionTime = value.Set(0.2)
	}
	data, _ := json.Marshal([]PlanEnvelope{envelope})
	return data
}

func TestPlanDecoderOwnsTypedResultAndExplicitJSON(t *testing.T) {
	for _, analyze := range []bool{false, true} {
		data := planDocument(analyze)
		plan, err := DecodePlan(t.Context(), data, analyze)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Analyzed() != analyze || plan.NodeCount() != 1 || plan.Root().NodeType != "Result" || plan.Root().ActualRows.IsSet() != analyze {
			t.Fatal("query plan lost metadata")
		}
		data[0] = '!'
		snapshot := plan.JSON()
		snapshot[0] = '!'
		if plan.JSON()[0] != '[' {
			t.Fatal("plan retained mutable JSON")
		}
		if text := fmt.Sprintf("%#v", plan); strings.Contains(text, "Result") {
			t.Fatal("routine formatting disclosed plan contents")
		}
	}
	document := PlanEnvelope{Plan: PlanNode{NodeType: "Append", Plans: []PlanNode{{NodeType: "Result"}}}}
	data, _ := json.Marshal([]PlanEnvelope{document})
	plan, err := DecodePlan(t.Context(), data, false)
	if err != nil {
		t.Fatal(err)
	}
	root := plan.Root()
	root.Plans[0].NodeType = "changed"
	if plan.Root().Plans[0].NodeType != "Result" {
		t.Fatal("plan children are not owned")
	}
}

func TestPlanDecoderRejectsMalformedUnboundedAndWrongMode(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`[]`), []byte(`[{},{}]`), []byte(`[{}]`), []byte(`[{"Plan":{"Node Type":"Result","Node Type":"Other"}}]`),
		[]byte(`[{"Plan":{"Node Type":"Result","Plan Rows":-1}}]`), []byte(`[{"Plan":{"Node Type":"Result","Total Cost":1e999}}]`),
		[]byte(`[{"Plan":{"Node Type":"Result"},"Execution Time":null}]`), []byte(strings.Repeat(" ", MaxPlanBytes+1)), planDocument(true),
	} {
		if _, err := DecodePlan(t.Context(), data, false); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid plan accepted", err)
		}
	}
	if _, err := DecodePlan(t.Context(), planDocument(false), true); !errors.Is(err, fault.Invalid) {
		t.Fatal("analysis accepted a nonexecuted plan")
	}
	root := PlanNode{NodeType: "Result"}
	for range MaxPlanDepth + 1 {
		root = PlanNode{NodeType: "Append", Plans: []PlanNode{root}}
	}
	data, _ := json.Marshal([]PlanEnvelope{{Plan: root}})
	if _, err := DecodePlan(t.Context(), data, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("deep plan accepted")
	}
	root = PlanNode{NodeType: "Append", Plans: make([]PlanNode, MaxPlanNodes)}
	for i := range root.Plans {
		root.Plans[i].NodeType = "Result"
	}
	data, _ = json.Marshal([]PlanEnvelope{{Plan: root}})
	if _, err := DecodePlan(t.Context(), data, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("large plan tree accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := DecodePlan(ctx, planDocument(false), false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var scanner PlanBytes
	for _, input := range []any{nil, 42, []byte{}, strings.Repeat("x", MaxPlanBytes+1)} {
		if err := scanner.Scan(input); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid plan scan accepted")
		}
	}
}

func FuzzPlanDocument(f *testing.F) {
	f.Add(planDocument(false), false)
	f.Add(planDocument(true), true)
	f.Add([]byte(`[{"Plan":null}]`), false)
	f.Fuzz(func(t *testing.T, data []byte, analyze bool) {
		if len(data) > MaxPlanBytes+1 {
			return
		}
		plan, err := DecodePlan(t.Context(), data, analyze)
		if err == nil && (plan.NodeCount() < 1 || plan.Analyzed() != analyze || len(plan.JSON()) > MaxPlanBytes) {
			t.Fatal("invalid plan success")
		}
	})
}

func TestPlanRejectsMissingOrNullCostFields(t *testing.T) {
	for _, field := range []string{"Startup Cost", "Total Cost", "Plan Rows", "Plan Width"} {
		for _, remove := range []bool{false, true} {
			var document []map[string]any
			if err := json.Unmarshal(planDocument(false), &document); err != nil {
				t.Fatal(err)
			}
			node := document[0]["Plan"].(map[string]any)
			if remove {
				delete(node, field)
			} else {
				node[field] = nil
			}
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodePlan(t.Context(), data, false); !errors.Is(err, fault.Invalid) {
				t.Fatalf("malformed %s accepted", field)
			}
		}
	}
}
