package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/httpheader"
	"github.com/weiloon1234/Foundry-Go/internal/httptoken"
)

type requestHeaderField struct {
	name     string
	index    int
	required bool
}

type requestHeaderText struct {
	text    string
	count   int
	bytes   int
	tooLong bool
	invalid bool
}

// Framing is non-generic and runs before any input codec. Only the first value
// is retained: duplicate singles are rejected, never joined or silently chosen.
func readRequestHeaders(ctx context.Context, headers stdhttp.Header, indexes map[string]int, fields []requestHeaderField, limits RequestHeaderLimits) (map[int][]string, error) {
	if len(fields) == 0 {
		return nil, ctx.Err()
	}
	states := make(map[int]requestHeaderText, len(fields))
	remainingBytes, remainingValues := limits.Bytes, limits.Values
	for name, lines := range headers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(name) > httpheader.MaxNameBytes || !httptoken.Valid(name) {
			continue
		}
		index, declared := indexes[strings.ToLower(name)]
		if !declared {
			continue
		}
		if len(lines) > remainingValues {
			return nil, requestHeaderFailure([]contract.Issue{{Code: contract.LengthIssue}}, nil)
		}
		remainingValues -= len(lines)
		state := states[index]
		for _, raw := range lines {
			if len(name) > remainingBytes || len(raw) > remainingBytes-len(name) {
				return nil, requestHeaderFailure([]contract.Issue{{Code: contract.LengthIssue}}, nil)
			}
			remainingBytes -= len(name) + len(raw)
			state.count++
			if len(raw) > limits.FieldBytes-state.bytes {
				state.tooLong = true
			} else {
				state.bytes += len(raw)
			}
			if !state.tooLong && httpheader.ValidateRequestValue(raw) != nil {
				state.invalid = true
			}
			if state.count == 1 && !state.tooLong && !state.invalid {
				state.text = strings.Trim(raw, " \t")
			}
		}
		states[index] = state
	}
	var issues []contract.Issue
	values := make(map[int][]string, len(states))
	for _, field := range fields {
		state := states[field.index]
		var code contract.IssueCode
		switch {
		case state.count == 0 && field.required:
			code = contract.RequiredIssue
		case state.tooLong || state.count > 1:
			code = contract.LengthIssue
		case state.invalid:
			code = contract.ValueIssue
		}
		if code != "" && len(issues) < limits.Issues {
			issues = append(issues, contract.Issue{Path: queryIssuePath(field.name), Code: code})
		}
		if state.count == 1 {
			values[field.index] = []string{state.text}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(issues) != 0 {
		return nil, requestHeaderFailure(issues, nil)
	}
	return values, nil
}

type requestHeaderBudget struct {
	limits RequestHeaderLimits
	bytes  int
	values int
}

func (b *requestHeaderBudget) take(name, text string) error {
	if len(text) > b.limits.FieldBytes {
		return requestHeaderFailure([]contract.Issue{{Path: queryIssuePath(name), Code: contract.LengthIssue}}, nil)
	}
	if b.values == b.limits.Values || len(name) > b.limits.Bytes-b.bytes || len(text) > b.limits.Bytes-b.bytes-len(name) {
		return requestHeaderFailure([]contract.Issue{{Code: contract.LengthIssue}}, nil)
	}
	if err := httpheader.ValidateRequestValue(text); err != nil {
		return requestHeaderFailure([]contract.Issue{{Path: queryIssuePath(name), Code: contract.ValueIssue}}, err)
	}
	if strings.Trim(text, " \t") != text {
		return requestHeaderFailure([]contract.Issue{{Path: queryIssuePath(name), Code: contract.ValueIssue}}, nil)
	}
	b.values++
	b.bytes += len(name) + len(text)
	return nil
}
