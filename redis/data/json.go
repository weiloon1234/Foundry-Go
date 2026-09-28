package data

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func encode[V any](input V, l Limits) (string, error) {
	snapshot, err := value.NewJSON(input)
	if err != nil {
		return "", err
	}
	text, err := snapshot.Text()
	if err != nil {
		return "", err
	}
	return text, l.ValidateValue(text)
}
func decode[V any](text string, l Limits) (V, error) {
	if err := l.ValidateValue(text); err != nil {
		return *new(V), err
	}
	snapshot, err := value.ParseJSON[V](text)
	if err != nil {
		return *new(V), err
	}
	canonical, err := snapshot.Text()
	if err != nil {
		return *new(V), err
	}
	if canonical != text {
		return *new(V), fault.New(fault.Invalid, "stored Redis data is not canonical JSON")
	}
	return snapshot.Decode()
}
