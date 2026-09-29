package validation

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonpointer"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type definition struct{ id RuleID }

type ruleNode struct {
	info                Description
	definitions         []*definition
	slots               []*slotKey
	nodes, depth, bytes int
	err                 error
	prohibitions        bool
	// callbacks marks application-owned callbacks that may block or call
	// runtime.Goexit (custom rules, lookups, hooks and slot providers).
	callbacks bool
}

// Rule retains its concrete input type. It is immutable after construction;
// Check may run concurrently on independent inputs. The zero value is invalid.
type Rule[T any] struct {
	ruleNode
	apply func(*execution, T, int)
	leaf  func(*execution, T) (bool, error)
	// report is a leaf that records zero or more issues itself, for example at
	// element paths. It receives the current spec/message so overrides apply.
	report  func(*execution, T, Spec, *i18n.PreparedMessage) error
	message *i18n.PreparedMessage
}

// Validate checks the declaration without executing selectors or callbacks.
// A rule reading a Slot is complete only inside a Provide for that slot.
func (r Rule[T]) Validate() error {
	if err := r.validateStructure(); err != nil {
		return err
	}
	if len(r.slots) != 0 {
		return invalid("validation slot requires an enclosing Provide")
	}
	return nil
}

func (r Rule[T]) validateStructure() error {
	if r.err != nil {
		return r.err
	}
	if r.apply == nil && r.leaf == nil && r.report == nil || r.nodes == 0 {
		return invalid("validation rule is not defined")
	}
	return nil
}

// Description returns an independent snapshot of the same rule tree Check uses.
// A subtree awaiting its enclosing Provide can still be described.
func (r Rule[T]) Description() (Description, error) {
	if err := r.validateStructure(); err != nil {
		return Description{}, err
	}
	return cloneDescription(r.info), nil
}

// Errors reports rejected input. Truncated reports that the issue cap stopped
// further validation; it does not claim the omitted checks would have failed.
type Errors struct {
	issues    []contract.Issue
	messages  []issueMessage
	truncated bool
}

func (*Errors) Error() string              { return "validation failed" }
func (*Errors) Is(target error) bool       { return target == fault.Invalid }
func (e *Errors) Issues() []contract.Issue { return slices.Clone(e.issues) }
func (e *Errors) Truncated() bool          { return e.truncated }

// Check executes owned callbacks and returns nil only after all applicable
// checks pass. Input and referenced data must remain unchanged until return.
// Panics and infrastructure errors remain internal faults. A tree containing
// application callbacks (Custom, Dynamic, Hook, lookups, Provide, image
// measurement) runs in one owned goroutine so runtime.Goexit is also contained;
// other trees run inline without a goroutine, where Goexit in a selector or value
// method ends the caller like any Go function. Cancellation never abandons a
// selector or custom rule that still owns work. Valid input allocates no issue
// storage or JSON Pointer paths.
func (r Rule[T]) Check(ctx context.Context, input T, limits Limits) error {
	return r.check(ctx, input, limits, false)
}

// CheckProhibitions checks only built-in Prohibited/Absent rules on original
// decoded input before preparation. Conditions retain their original-input
// semantics. It does not substitute for Check on the prepared value.
func (r Rule[T]) CheckProhibitions(ctx context.Context, input T, limits Limits) error {
	return r.check(ctx, input, limits, true)
}

func (r Rule[T]) check(ctx context.Context, input T, limits Limits, prohibitionsOnly bool) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return invalid("validation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if prohibitionsOnly && !r.prohibitions {
		return nil
	}
	state := execution{ctx: ctx, limits: limits, prohibitionsOnly: prohibitionsOnly}
	state.work = &state.budget
	state.segments = state.segmentBuffer[:0]
	var err error
	if r.callbacks {
		err = r.runIsolated(&state, input)
	} else {
		err = callback.Invoke("validation", func() error { r.run(&state, input, 0); return nil })
	}
	if err != nil {
		return fault.Wrap(fault.Internal, "validation callback failed", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.err != nil {
		return state.err
	}
	if len(state.issues) != 0 {
		return &Errors{issues: state.issues, messages: state.messages, truncated: state.truncated}
	}
	return nil
}

// runIsolated keeps the goroutine-capturing closure out of check's frame, so
// inline checks do not move the rule to the heap.
func (r Rule[T]) runIsolated(s *execution, input T) error {
	return callback.Isolated("validation", func() error { r.run(s, input, 0); return nil })
}

type execution struct {
	// budget stays first so the root state's atomic counter is 64-bit aligned.
	// Only the root owns it; branches and probes share it through work.
	budget           int64
	ctx              context.Context
	limits           Limits
	work             *int64
	parallel         bool
	issues           []contract.Issue
	messages         []issueMessage
	segments         []segment
	segmentBuffer    [8]segment
	field            string
	otherField       string
	otherLabel       string
	otherLabelKey    i18n.MessageKey
	label            string
	labelKey         i18n.MessageKey
	truncated        bool
	err              error
	prohibitionsOnly bool
}

func (s *execution) take(depth int) bool {
	if s.err != nil || s.truncated {
		return false
	}
	if err := s.ctx.Err(); err != nil {
		s.err = err
		return false
	}
	if len(s.issues) >= s.limits.Issues {
		s.truncated = true
		return false
	}
	if depth > s.limits.Depth {
		s.err = &LimitError{}
		return false
	}
	for {
		used := atomic.LoadInt64(s.work)
		if used >= int64(s.limits.Checks) {
			s.err = &LimitError{}
			return false
		}
		if atomic.CompareAndSwapInt64(s.work, used, used+1) {
			return true
		}
	}
}

func (s *execution) remainingChecks() int { return s.limits.Checks - int(atomic.LoadInt64(s.work)) }

// segment is one JSON Pointer step. Paths are materialized only for issues, so
// valid input does not allocate path strings.
// A named step uses index -1; an array step has a nonnegative index.
type segment struct {
	name  string
	index int
}

func (s *execution) enter(name string) {
	s.segments = append(s.segments, segment{name: name, index: -1})
}
func (s *execution) enterIndex(index int) {
	s.segments = append(s.segments, segment{index: index})
}
func (s *execution) leave() { s.segments = s.segments[:len(s.segments)-1] }
func (s *execution) path() string {
	path := ""
	for _, step := range s.segments {
		if step.index < 0 {
			path = jsonpointer.Append(path, step.name)
		} else {
			path = jsonpointer.Append(path, strconv.Itoa(step.index))
		}
	}
	return path
}

// branch returns independent state for a concurrently executed child. Shared
// work accounting and inherited labels are retained; path steps are copied.
func (s *execution) branch() execution {
	child := execution{ctx: s.ctx, limits: s.limits, work: s.work, parallel: true,
		label: s.label, labelKey: s.labelKey, field: s.field, otherField: s.otherField, otherLabel: s.otherLabel, otherLabelKey: s.otherLabelKey, prohibitionsOnly: s.prohibitionsOnly}
	child.limits.Issues -= len(s.issues)
	child.segments = slices.Clone(s.segments)
	return child
}

func (r Rule[T]) run(s *execution, input T, depth int) {
	if s.prohibitionsOnly && !r.prohibitions {
		return
	}
	if !s.take(depth) {
		return
	}
	if r.leaf == nil && r.report == nil {
		r.apply(s, input, depth)
		return
	}
	valid, err := true, error(nil)
	if r.report != nil {
		err = r.report(s, input, *r.info.Spec, r.message)
	} else {
		valid, err = r.leaf(s, input)
	}
	if canceled := s.ctx.Err(); canceled != nil {
		s.err = canceled
		return
	}
	if s.err != nil {
		return
	}
	if err != nil {
		s.err = fault.Wrap(fault.Internal, "validation rule execution failed", err)
		return
	}
	if !valid {
		s.issue(*r.info.Spec, r.message)
	}
}
func (s *execution) issue(spec Spec, message *i18n.PreparedMessage) {
	item := issueMessage{message: message, field: s.field, label: s.label, labelKey: s.labelKey, otherField: s.otherField, otherLabel: s.otherLabel, otherLabelKey: s.otherLabelKey}
	path := s.path()
	text := spec.Message
	if message != nil {
		rendered, err := item.render(s.ctx, nil, "")
		if err != nil {
			s.err = fault.Wrap(fault.Internal, "validation message failed", err)
			return
		}
		text = rendered
	}
	s.issues = append(s.issues, contract.Issue{Path: path, Code: contract.IssueCode(spec.ID), Message: text, Label: s.label, LabelKey: s.labelKey})
	s.messages = append(s.messages, item)
}

// Custom defines a typed server rule. Returning false rejects the input with
// Spec.Message; returning an error means execution failed, not invalid input.
// Reuse the returned rule value when sharing one definition across fields.
func Custom[T any](spec Spec, check func(context.Context, T) (bool, error)) Rule[T] {
	if strings.HasPrefix(string(spec.ID), "foundry.") {
		return failed[T](invalid("foundry validation IDs are reserved"))
	}
	rule := leaf(spec, true, check)
	if rule.err == nil {
		rule.definitions = []*definition{{id: spec.ID}}
		rule.callbacks = true
	}
	return rule
}

func leaf[T any](spec Spec, server bool, check func(context.Context, T) (bool, error)) Rule[T] {
	if check == nil {
		return failed[T](invalid("validation callback is missing"))
	}
	return valueRule(spec, server, func(s *execution, input T) (bool, error) { return check(s.ctx, input) })
}

func valueRule[T any](spec Spec, server bool, check func(*execution, T) (bool, error)) Rule[T] {
	var prepared *i18n.PreparedMessage
	if spec.Message == "" && strings.HasPrefix(string(spec.ID), "foundry.") && spec.Translation == nil {
		message, err := builtinPrepared(spec)
		if err != nil {
			return failed[T](err)
		}
		result, err := message.Format(context.Background(), nil, "")
		if err != nil {
			return failed[T](err)
		}
		recipe := message.Description()
		spec.Translation = &recipe
		spec.Message = result.Text
	}
	owned, err := copySpec(spec)
	if err != nil {
		return failed[T](err)
	}
	if owned.Translation != nil {
		message, err := owned.Translation.Prepare()
		if err != nil {
			return failed[T](err)
		}
		prepared = &message
	}
	info := Description{Kind: LeafKind, Spec: &owned, ServerOnly: server || scalarWireTransform[T]()}
	size := infoBytes(info)
	if size > maxDescriptionBytes {
		return failed[T](invalid("validation metadata exceeds its byte bound"))
	}
	return Rule[T]{ruleNode: ruleNode{info: info, nodes: 1, bytes: size, prohibitions: spec.ID == "foundry.prohibited" || spec.ID == "foundry.absent"}, leaf: check, message: prepared}
}

func failed[T any](err error) Rule[T] { return Rule[T]{ruleNode: ruleNode{err: err}} }

// WithMessage replaces only the public message of one leaf. Composite trees
// retain their individual messages; overriding a composite is rejected.
func (r Rule[T]) WithMessage(message string) Rule[T] {
	if err := r.validateStructure(); err != nil {
		return r
	}
	if r.info.Kind != LeafKind || !validText(message, false) {
		r.err = invalid("message override requires a leaf rule and valid public text")
		return r
	}
	before := infoBytes(r.info)
	r.info = cloneDescription(r.info)
	r.info.Spec.Message = message
	r.info.Spec.Translation = nil
	r.message = nil
	r.bytes += infoBytes(r.info) - before
	if r.bytes > maxDescriptionBytes {
		r.err = invalid("validation metadata exceeds its byte bound")
	}

	return r
}

func composeNode(info Description, children []ruleNode) ruleNode {
	rule := ruleNode{info: info, nodes: 1, bytes: infoBytes(info)}
	definitions := make(map[RuleID]*definition)
	for _, child := range children {
		if child.err != nil {
			rule.err = child.err
			return rule
		}
		if child.nodes == 0 {
			rule.err = invalid("validation child is not defined")
			return rule
		}
		if child.nodes > maxRuleNodes-rule.nodes || child.depth >= jsonwire.MaxDepth || child.bytes > maxDescriptionBytes-rule.bytes {
			rule.err = invalid("validation declaration exceeds its structural bound")
			return rule
		}
		rule.nodes += child.nodes
		rule.prohibitions = rule.prohibitions || child.prohibitions
		rule.callbacks = rule.callbacks || child.callbacks
		for _, key := range child.slots {
			if !slices.Contains(rule.slots, key) {
				rule.slots = append(rule.slots, key)
			}
		}
		rule.depth = max(rule.depth, child.depth+1)
		rule.bytes += child.bytes
		rule.info.Children = append(rule.info.Children, child.info)
		rule.info.ServerOnly = rule.info.ServerOnly || child.info.ServerOnly
		for _, current := range child.definitions {
			if previous := definitions[current.id]; previous != nil && previous != current {
				rule.err = fault.New(fault.Duplicate, "validation rule ID has multiple definitions")
				return rule
			}
			definitions[current.id] = current
		}
	}
	for _, child := range children {
		for _, current := range child.definitions {
			if definitions[current.id] != nil {
				rule.definitions = append(rule.definitions, current)
				delete(definitions, current.id)
			}
		}
	}
	return rule
}
