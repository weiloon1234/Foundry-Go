package consumer

import v "github.com/weiloon1234/Foundry-Go/validation"

type Request struct{ Name string }
type Other struct{}

func (Other) Validate() error { return nil }

// Keep this consumer declaration and its comments.
func migrate(f v.Field[Request, string], r v.Rule[Request], input Request, rules []v.Rule[string]) {
	_ = v.Rules(f, rules...)
	_ = v.ValidateField(f)
	_ = v.FieldName(f)
	_ = v.Select(f, input)
	_ = v.WithLabel(f, "Name")
	_ = v.WithLabelKey(f, "request.name")
	_ = v.ValidateRule(r)
	_, _ = v.DescribeRule(r)
	_ = v.Check(r, 1, input, v.Limits{})
	_ = v.CheckProhibitions(r, 1, input, v.Limits{})
	_ = v.WithMessage(r, "required")
	_ = v.Rules(v.WithLabelKey(v.WithLabel(f, "Name"), "request.name"), rules...)
	_ = v.Rules[Request, string]
	_ = v.Check[Request]
	_ = v.ValidateRule(r)
	_ = Other{}.Validate() // unrelated method must stay
}
