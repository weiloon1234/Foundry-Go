package query

import (
	"context"
	"encoding/json"
	"math"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/value"
)

func decodePlan(ctx context.Context, data []byte, analyze bool) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	wire, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: MaxPlanBytes, Depth: jsonwire.MaxDepth, Nodes: MaxPlanNodes * 64})
	if err != nil {
		return Plan{}, invalidPlan()
	}
	envelopes, ok := wire.([]any)
	if !ok || len(envelopes) != 1 {
		return Plan{}, invalidPlan()
	}
	envelope, ok := envelopes[0].(map[string]any)
	if !ok {
		return Plan{}, invalidPlan()
	}
	root, ok := envelope["Plan"].(map[string]any)
	if !ok {
		return Plan{}, invalidPlan()
	}
	var rows []planEnvelope
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 1 {
		return Plan{}, invalidPlan()
	}
	document := rows[0]
	if document.ExecutionTime.IsSet() != analyze || !validPlanMeasurement(document.PlanningTime) || !validPlanMeasurement(document.ExecutionTime) {
		return Plan{}, invalidPlan()
	}
	type frame struct {
		node  *PlanNode
		wire  map[string]any
		depth int
	}
	pending := []frame{{&document.Plan, root, 0}}
	count := 0
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		count++
		node := current.node
		// COSTS TRUE always supplies these fields. Missing/null costs are malformed,
		// not zero estimates. Inspect the already bounded wire tree without decoding again.
		for _, key := range []string{"Startup Cost", "Total Cost", "Plan Rows", "Plan Width"} {
			if _, ok := current.wire[key].(json.Number); !ok {
				return Plan{}, invalidPlan()
			}
		}
		var children []any
		if raw, present := current.wire["Plans"]; present {
			var ok bool
			children, ok = raw.([]any)
			if !ok {
				return Plan{}, invalidPlan()
			}
		}
		if len(children) != len(node.Plans) {
			return Plan{}, invalidPlan()
		}
		if count > MaxPlanNodes || current.depth > MaxPlanDepth || node.NodeType == "" || !utf8.ValidString(node.NodeType) || len(node.NodeType) > 256 || !validPlanNumber(node.StartupCost) || !validPlanNumber(node.TotalCost) || !validPlanNumber(node.PlanRows) || node.PlanWidth < 0 {
			return Plan{}, invalidPlan()
		}
		if node.ActualRows.IsSet() != analyze || node.ActualLoops.IsSet() != analyze || node.ActualStartupTime.IsSet() != analyze || node.ActualTotalTime.IsSet() != analyze {
			return Plan{}, invalidPlan()
		}
		for _, measurement := range []value.Optional[float64]{node.ActualRows, node.ActualLoops, node.ActualStartupTime, node.ActualTotalTime} {
			if !validPlanMeasurement(measurement) {
				return Plan{}, invalidPlan()
			}
		}
		if len(node.Plans) > MaxPlanNodes-count-len(pending) {
			return Plan{}, invalidPlan()
		}
		for i := range node.Plans {
			child, ok := children[i].(map[string]any)
			if !ok {
				return Plan{}, invalidPlan()
			}
			pending = append(pending, frame{&node.Plans[i], child, current.depth + 1})
		}
	}
	return Plan{document: document, json: append([]byte(nil), data...), analyzed: analyze, nodes: count}, nil
}
func validPlanNumber(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 }
func validPlanMeasurement(n value.Optional[float64]) bool {
	v, ok := n.Get()
	return !ok || validPlanNumber(v)
}
