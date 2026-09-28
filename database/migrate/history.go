package migrate

import (
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Applied records a completed migration. Nontransactional statements finish
// before their history entry; their effects are not atomic with history.
type Applied struct {
	Key       Key       `json:"key"`
	Version   Version   `json:"version"`
	Checksum  Checksum  `json:"checksum"`
	Batch     int64     `json:"batch"`
	AppliedAt time.Time `json:"applied_at"`
}

// State is a migration's status relative to the currently registered definitions.
type State string

const (
	Pending    State = "pending"
	Incomplete State = "incomplete"
	Complete   State = "applied"
	Changed    State = "changed"
	Missing    State = "missing_definition"
)

// Status contains current metadata and, when present, its recorded history.
type Status struct {
	Progress   *Progress `json:"progress,omitempty"`
	Key        Key       `json:"key"`
	State      State     `json:"state"`
	Definition *Entry    `json:"definition,omitempty"`
	Applied    *Applied  `json:"applied,omitempty"`
}

// ProblemCode names a specific history conflict that prevents an up operation.
type ProblemCode string

const (
	ReconciliationRequired ProblemCode = "reconciliation_required"
	ProgressMismatch       ProblemCode = "progress_mismatch"
	DefinitionChanged      ProblemCode = "definition_changed"
	DefinitionMissing      ProblemCode = "definition_missing"
	DependencyNotApplied   ProblemCode = "dependency_not_applied"
)

// Problem identifies history drift without including migration SQL or values.
type Problem struct {
	Code       ProblemCode `json:"code"`
	Key        Key         `json:"key"`
	Dependency *Key        `json:"dependency,omitempty"`
}

// Report is a caller-owned status snapshot. Problems may be shown by read-only
// status commands; Check must succeed before applying any pending definitions.
type Report struct {
	Statuses  []Status  `json:"migrations"`
	Problems  []Problem `json:"problems,omitempty"`
	LastBatch int64     `json:"last_batch"`
}

// Check reports drift without formatting untrusted SQL or stored values.
func (r Report) Check() error {
	if len(r.Problems) != 0 {
		return fault.New(fault.Conflict, fmt.Sprintf("migration history has %d conflict(s); inspect status before applying changes", len(r.Problems)))
	}
	return nil
}

// Inspect validates a history snapshot and reports missing/changed definitions
// and dependencies. It performs no database I/O and does not repair history.
func (r *Registry) Inspect(history []Applied) (Report, error) {
	report := Report{Statuses: make([]Status, 0, len(r.ordered))}
	byKey := make(map[Key]Applied, len(history))
	for _, entry := range history {
		if !validKey(entry.Key) || !validName(string(entry.Version)) || entry.Batch <= 0 || entry.AppliedAt.IsZero() {
			return Report{}, fault.New(fault.Invalid, "invalid migration history record")
		}
		if _, exists := byKey[entry.Key]; exists {
			return Report{}, fault.New(fault.Duplicate, "duplicate migration history record")
		}
		entry.AppliedAt = entry.AppliedAt.UTC()
		byKey[entry.Key] = entry
		report.LastBatch = max(report.LastBatch, entry.Batch)
	}
	for _, item := range r.ordered {
		entry := copyEntry(item.entry)
		status := Status{Key: entry.Key, State: Pending, Definition: &entry}
		if applied, exists := byKey[entry.Key]; exists {
			status.State = Complete
			status.Applied = &applied
			if applied.Checksum != entry.Checksum || applied.Version != entry.Version {
				status.State = Changed
				report.Problems = append(report.Problems, Problem{Code: DefinitionChanged, Key: entry.Key})
			}
			for _, key := range entry.Requires {
				if _, exists := byKey[key]; !exists {
					dependency := key
					report.Problems = append(report.Problems, Problem{Code: DependencyNotApplied, Key: entry.Key, Dependency: &dependency})
				}
			}
		}
		report.Statuses = append(report.Statuses, status)
	}
	var missing []Key
	for key := range byKey {
		if _, exists := r.byKey[key]; !exists {
			missing = append(missing, key)
		}
	}
	sortKeys(missing)
	for _, key := range missing {
		applied := byKey[key]
		report.Statuses = append(report.Statuses, Status{Key: key, State: Missing, Applied: &applied})
		report.Problems = append(report.Problems, Problem{Code: DefinitionMissing, Key: key})
	}
	return report, nil
}

// Pending returns execution metadata only if the entire history is consistent.
// It is a snapshot, not a lock or authorization to execute an old plan; a runner
// must inspect history again while holding its database migration lock.
func (r *Registry) Pending(history []Applied) ([]Entry, error) {
	report, err := r.Inspect(history)
	if err != nil {
		return nil, err
	}
	if err := report.Check(); err != nil {
		return nil, err
	}
	var pending []Entry
	for _, status := range report.Statuses {
		if status.State == Pending {
			pending = append(pending, copyEntry(*status.Definition))
		}
	}
	return pending, nil
}
