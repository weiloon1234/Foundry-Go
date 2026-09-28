package raw

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type replyKind uint8

const (
	nullKind replyKind = iota
	integerKind
	textKind
	arrayKind
)

// Reply is an immutable, owned RESP2 value for explicit decoders and adapters.
// Its zero value is Redis nil. Array returns a new slice of immutable child values.
type Reply struct {
	kind    replyKind
	integer int64
	text    string
	array   []Reply
}

func (r Reply) IsNull() bool { return r.kind == nullKind }
func (r Reply) Integer() (int64, error) {
	if r.kind != integerKind {
		return 0, replyError()
	}
	return r.integer, nil
}
func (r Reply) Text() (string, error) {
	if r.kind != textKind {
		return "", replyError()
	}
	return r.text, nil
}
func (r Reply) Array() ([]Reply, error) {
	if r.kind != arrayKind {
		return nil, replyError()
	}
	return append(make([]Reply, 0, len(r.array)), r.array...), nil
}
func replyError() error {
	return fault.New(fault.Invalid, "Redis reply does not match its declared result")
}

type replyBudget struct {
	ctx          context.Context
	limits       ReplyLimits
	nodes, bytes int
}

func (b *replyBudget) enter(depth int, text string) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if depth > b.limits.Depth || b.nodes >= b.limits.Nodes || len(text) > b.limits.Bytes-b.bytes {
		return fault.New(fault.Invalid, "Redis reply exceeds its bound")
	}
	b.nodes++
	b.bytes += len(text)
	return nil
}
func newBudget(ctx context.Context, l ReplyLimits) (replyBudget, error) {
	if ctx == nil {
		return replyBudget{}, fault.New(fault.Invalid, "Redis reply capture requires a context")
	}
	if err := l.Validate(); err != nil {
		return replyBudget{}, err
	}
	return replyBudget{ctx: ctx, limits: l}, nil
}

// CaptureReply is the adapter boundary for driver-decoded RESP2 (nil, int64,
// string, or []any). No driver types escape. Bounds apply after RESP parsing.
func CaptureReply(ctx context.Context, input any, l ReplyLimits) (Reply, error) {
	b, err := newBudget(ctx, l)
	if err != nil {
		return Reply{}, err
	}
	return b.capture(input, 0)
}

// CaptureReplies shares one budget across a complete pipeline, including its
// synthetic array root. Errors return no partial batch and do not expose payloads.
func CaptureReplies(ctx context.Context, input []any, l ReplyLimits) ([]Reply, error) {
	r, err := CaptureReply(ctx, input, l)
	if err != nil {
		return nil, err
	}
	return r.array, nil
}
func (b *replyBudget) capture(input any, depth int) (Reply, error) {
	text, _ := input.(string)
	if err := b.enter(depth, text); err != nil {
		return Reply{}, err
	}
	switch v := input.(type) {
	case nil:
		return Reply{}, nil
	case int64:
		return Reply{kind: integerKind, integer: v}, nil
	case string:
		return Reply{kind: textKind, text: strings.Clone(v)}, nil
	case []any:
		if len(v) > b.limits.Nodes-b.nodes {
			return Reply{}, fault.New(fault.Invalid, "Redis reply exceeds its node bound")
		}
		result := Reply{kind: arrayKind, array: make([]Reply, len(v))}
		for i, x := range v {
			child, err := b.capture(x, depth+1)
			if err != nil {
				return Reply{}, err
			}
			result.array[i] = child
		}
		return result, nil
	case error:
		return Reply{}, fault.Wrap(fault.Internal, "Redis command returned an error", v)
	default:
		return Reply{}, replyError()
	}
}
func (r Reply) validate(b *replyBudget, depth int) error {
	if err := b.enter(depth, r.text); err != nil {
		return err
	}
	for _, child := range r.array {
		if err := child.validate(b, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func validateReplies(ctx context.Context, replies []Reply, l ReplyLimits, batch bool) error {
	b, err := newBudget(ctx, l)
	if err != nil {
		return err
	}
	depth := 0
	if batch {
		if err := b.enter(0, ""); err != nil {
			return err
		}
		depth = 1
	}
	for _, reply := range replies {
		if err := reply.validate(&b, depth); err != nil {
			return err
		}
	}
	return nil
}
