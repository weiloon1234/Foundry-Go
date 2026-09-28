package validation

import (
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

const DefaultConcurrency = 4
const maxConcurrency = 64

// Parallel checks independent branches with DefaultConcurrency workers. Each
// branch retains normal All/Bail order. Use a concurrency-safe pool or client;
// a transaction or other serial dependency belongs in a sequential branch.
func Parallel[T any](rules ...Rule[T]) Rule[T] { return ParallelLimit(DefaultConcurrency, rules...) }

// ParallelLimit runs bounded waves, merges issues in declaration order, and
// shares one work budget. Nested parallel groups execute sequentially inside
// their branch, so nesting cannot multiply concurrency or deadlock workers.
// All admitted callbacks finish before return, including after cancellation,
// panic or Goexit. Inputs/selectors/dependencies must support concurrent reads.
// Metadata is logical All, server-only; scheduling is not a wire contract.
func ParallelLimit[T any](concurrency int, rules ...Rule[T]) Rule[T] {
	base := All(rules...)
	if concurrency < 1 || concurrency > maxConcurrency {
		return failed[T](invalid("validation concurrency must be between 1 and 64"))
	}
	if base.Validate() != nil {
		return base
	}
	owned := slices.Clone(rules)
	base.info.ServerOnly = true
	sequential := base.apply
	base.apply = func(s *execution, input T, path string, depth int) {
		if s.parallel || concurrency == 1 {
			sequential(s, input, path, depth)
			return
		}
		for start := 0; start < len(owned); start += concurrency {
			if s.err != nil || s.truncated {
				return
			}
			if err := s.ctx.Err(); err != nil {
				s.err = err
				return
			}
			if len(s.issues) >= s.limits.Issues {
				s.truncated = true
				return
			}
			end := min(start+concurrency, len(owned))
			states := make([]execution, end-start)
			var workers sync.WaitGroup
			for i := range states {
				states[i] = execution{ctx: s.ctx, limits: s.limits, work: s.work, parallel: true,
					label: s.label, labelKey: s.labelKey, field: s.field, otherField: s.otherField, otherLabel: s.otherLabel, otherLabelKey: s.otherLabelKey, prohibitionsOnly: s.prohibitionsOnly}
				states[i].limits.Issues -= len(s.issues)
				workers.Add(1)
				go func(i int) {
					defer workers.Done()
					child := &states[i]
					if err := callback.Isolated("parallel validation", func() error {
						owned[start+i].run(child, input, path, depth+1)
						return nil
					}); err != nil {
						child.err = fault.Wrap(fault.Internal, "validation callback failed", err)
					}
				}(i)
			}
			workers.Wait()
			// Failures from any admitted branch take precedence over ordinary
			// rejections, even when the public issue limit has already been met.
			for i := range states {
				child := &states[i]
				if s.err == nil && child.err != nil {
					s.err = child.err
				}
				space := s.limits.Issues - len(s.issues)
				s.issues = append(s.issues, child.issues[:min(space, len(child.issues))]...)
				s.messages = append(s.messages, child.messages[:min(space, len(child.messages))]...)
				s.truncated = s.truncated || child.truncated || len(child.issues) > space
			}
		}
	}
	return base
}
