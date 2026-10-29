package consumer

import v "github.com/weiloon1234/Foundry-Go/validation"

type Request struct{ Name string }
type Other struct{}

func (Other) Validate() error { return nil }

// Keep this consumer declaration and its comments.
func migrate(f v.Field[Request, string], r v.Rule[Request], input Request, rules []v.Rule[string]) {
	_ = f.Rules(rules...)
	_ = f.Validate()
	_ = f.Name()
	_ = f.Select(input)
	_ = f.WithLabel("Name")
	_ = f.WithLabelKey("request.name")
	_ = r.Validate()
	_, _ = r.Description()
	_ = r.Check(1, input, v.Limits{})
	_ = r.CheckProhibitions(1, input, v.Limits{})
	_ = r.WithMessage("required")
	_ = f.WithLabel("Name").WithLabelKey("request.name").Rules(rules...)
	_ = v.Field[Request, string].Rules
	_ = v.Rule[Request].Check
	_ = (r.Validate)()
	_ = Other{}.Validate() // unrelated method must stay
}
