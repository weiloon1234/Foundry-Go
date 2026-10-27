package querycore

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func (e Comparison) hasBinding() bool { return e.Bind != nil || e.codecBind != nil }

func (e Comparison) bindValue(value any) (driver.Value, error) {
	if e.Bind == nil && e.codecBind != nil {
		return e.codecBind(value)
	}
	var result driver.Value
	err := callback.Isolated("query comparison binding", func() error {
		var failure error
		result, failure = e.Bind(value)
		return failure
	})
	return result, err
}

func (e Comparison) bindValues() ([]driver.Value, error) {
	values := ComparisonValues(e.Values)
	if len(values) > MaxMembershipValues {
		return nil, safeerror.NewFault(fault.Invalid, "membership list exceeds its value bound")
	}
	bound := make([]driver.Value, len(values))
	if e.Bind == nil && e.codecBind != nil {
		if err := e.bindInto(bound, e.codecBind); err != nil {
			return nil, err
		}
		return bound, nil
	}
	// A single owned invocation covers the bounded list. Native validation
	// stays in order with binding so an earlier invalid value wins. Return
	// its diagnostic outside Invoke without adding callback error context.
	var failure error
	err := callback.Isolated("query comparison binding", func() error {
		failure = e.bindInto(bound, e.Bind)
		return nil
	})
	if err != nil {
		return nil, safeerror.Wrap("query comparison binding failed", err)
	}
	if failure != nil {
		return nil, failure
	}
	return bound, nil
}

func (e Comparison) bindInto(bound []driver.Value, bind func(any) (driver.Value, error)) error {
	for i, value := range ComparisonValues(e.Values) {
		v, err := bind(value)
		if err != nil {
			return safeerror.Wrap("query comparison binding failed", callback.Returned("query comparison binding", err))
		}
		if v == nil {
			return safeerror.NewFault(fault.Invalid, "NULL comparison requires IsNull or IsNotNull")
		}
		if EscapedPattern(e.Operator) {
			text, ok := v.(string)
			if !ok {
				return safeerror.NewFault(fault.Invalid, "text pattern operations require a text database representation")
			}
			v = LikePattern(e.Operator, text)
		}
		bound[i] = v
	}
	return nil
}
