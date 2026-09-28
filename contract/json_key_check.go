package contract

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (c *shapeCheck) checkMapKeys(typ Type, keys []string, path string) bool {
	if typ.jsonKey == nil {
		return true
	}
	runtime := typ.jsonKey
	bytes := 0
	for _, key := range keys {
		if !c.take() {
			return false
		}
		if len(key) > int(^uint(0)>>1)-bytes {
			c.err = fault.New(fault.Invalid, "JSON key work bound exceeded")
			return false
		}
		bytes += len(key)
		var node any = key
		if runtime.info.Value.Kind == IntegerKind {
			node = json.Number(key)
		}
		issues, err := runtime.scalar.check(c.ctx, node, shapeLimits{steps: 1, issues: 1})
		if err != nil {
			c.err = err
			return false
		}
		if len(issues) != 0 {
			c.issue(path, KeyIssue)
			return false
		}
		if runtime.info.NonZero {
			id, err := model.ParseID[struct{}](key)
			if err != nil || id.IsZero() {
				c.issue(path, KeyIssue)
				return false
			}
		}
	}
	if len(keys) == 0 {
		return true
	}
	err := runtime.check(c.ctx, keys, value.JSONKeyLimits{Bytes: max(1, bytes), Keys: len(keys)})
	if err != nil {
		c.issue(path, KeyIssue)
		c.err = err
		return false
	}
	return true
}

// Native callback failures retain internal-failure classification even if the
// request was cancelled during that callback. No arbitrary error methods run.
func jsonKeyInternalFailure(err error) error {
	if failure, ok := err.(*fault.Error); ok && failure.Code() == fault.Internal {
		return fault.Wrap(fault.Internal, "JSON key validation failed", err)
	}
	return nil
}
