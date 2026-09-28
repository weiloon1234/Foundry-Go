package schedule

import (
	"cmp"
	"slices"
	"time"
)

// Description contains timing and execution policy without callback references,
// coordination ownership or task history. Interval anchors are explicit UTC.
type Description struct {
	ID             ID            `json:"id"`
	Expression     string        `json:"expression"`
	TimeZone       string        `json:"time_zone"`
	Interval       time.Duration `json:"interval,omitzero"`
	Anchor         time.Time     `json:"anchor,omitzero"`
	Timeout        time.Duration `json:"timeout"`
	WithoutOverlap bool          `json:"without_overlap"`
	OverlapTTL     time.Duration `json:"overlap_ttl"`
	Environments   []string      `json:"environments"`
	CatchUp        CatchUp       `json:"catch_up"`
}

func (r *Registry) Describe() []Description {
	if r == nil {
		return nil
	}
	result := make([]Description, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, Description{ID: entry.id, Expression: entry.spec.String(), TimeZone: entry.spec.TimeZone(), Interval: entry.spec.interval, Anchor: entry.spec.anchor, Timeout: entry.options.Timeout, WithoutOverlap: entry.options.WithoutOverlap, OverlapTTL: entry.options.OverlapTTL, Environments: slices.Clone(entry.options.Environments), CatchUp: entry.options.CatchUp})
	}
	slices.SortFunc(result, func(a, b Description) int { return cmp.Compare(a.ID, b.ID) })
	return result
}

// Describe reads immutable declarations; it does not inspect leases or run work.
func (s *Scheduler) Describe() []Description {
	if s == nil {
		return nil
	}
	return s.registry.Describe()
}
