package storage

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/weiloon1234/Foundry-Go/internal/deployrehearsal"
)

// ConditionalWriteMode distinguishes a startup guarantee from a rehearsal-only
// runtime restriction. The zero value preserves normal capability validation.
type ConditionalWriteMode string

const RehearsalRuntime ConditionalWriteMode = "rehearsal_runtime"

// Rehearsal is a sealed, immutable grant issued by deployment.LoadBindings.
// Its zero value grants nothing; environment or configuration values cannot create it.
type Rehearsal = deployrehearsal.Grant[DiskID]

// WithRehearsal applies a validated descriptor grant only to its exact disk and cap.
func (c Capabilities) WithRehearsal(id DiskID, grant Rehearsal) Capabilities {
	if cap, ok := grant.Cap(id); ok && cap > 0 && cap == c.ConditionalWriteMaxBytes {
		c.ConditionalWriteMode, c.rehearsal, c.rehearsalDisk = RehearsalRuntime, grant, id
	}
	return c
}

func (c Capabilities) validateRehearsal() error {
	if c.ConditionalWriteMode == "" {
		return nil
	}
	cap, ok := c.rehearsal.Cap(c.rehearsalDisk)
	if c.ConditionalWriteMode != RehearsalRuntime || !ok || cap <= 0 || cap != c.ConditionalWriteMaxBytes {
		return newFailure(Invalid, OpenOperation, NotApplicable, nil)
	}
	return nil
}

// ValidatePutPolicy checks startup requirements. Only the explicit rehearsal
// mode defers the conditional size restriction until a real write.
func (c Capabilities) ValidatePutPolicy(options PutOptions) error {
	if err := c.validateRehearsal(); err != nil {
		return err
	}
	if c.ConditionalWriteMode == RehearsalRuntime {
		c.ConditionalWriteMaxBytes = 0
	}
	return c.ValidatePut(options)
}

// WritePolicyMaximum is a declared policy, never an object key or upload name.
type WritePolicyMaximum struct {
	Name     string `json:"name"`
	MaxBytes int64  `json:"max_bytes"`
}

// RehearsalRefusals returns this disk instance's runtime cap refusal count.
func (d *Disk) RehearsalRefusals() uint64 { return d.rehearsalRefusals.Load() }

// LogRehearsal logs one redacted startup notice per disk instance. Dedicated
// JSON records on stderr let the disposable host collect evidence independently
// of application log formatting or filtering. No object identity is included.
func (d *Disk) LogRehearsal(ctx context.Context, policies []WritePolicyMaximum) {
	if d.capabilities.ConditionalWriteMode != RehearsalRuntime {
		return
	}
	d.rehearsalNotice.Do(func() {
		exceeded := make([]WritePolicyMaximum, 0, len(policies))
		for _, policy := range policies {
			if policy.MaxBytes > d.capabilities.ConditionalWriteMaxBytes {
				exceeded = append(exceeded, policy)
			}
		}
		d.rehearsalLogger().WarnContext(ctx, "reduced-fidelity rehearsal: conditional writes above the runtime cap are refused",
			"event", "foundry_rehearsal_storage", "disk", string(d.id),
			"runtime_cap_bytes", d.capabilities.ConditionalWriteMaxBytes,
			"max_object_bytes", d.config.MaxObjectBytes, "exceeding_policy_maxima", exceeded)
	})
}

func (d *Disk) rehearsalLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, nil))
}

func (d *Disk) validatePut(options PutOptions) error {
	err := d.capabilities.ValidatePut(options)
	// This result comes only from the framework capability validator.
	if errors.Is(err, RehearsalLimitExceeded) {
		count := d.rehearsalRefusals.Add(1)
		d.rehearsalLogger().Warn("reduced-fidelity rehearsal: conditional write refused",
			"event", "foundry_rehearsal_storage_refusal", "disk", string(d.id),
			"runtime_cap_bytes", d.capabilities.ConditionalWriteMaxBytes, "refusals", count)
	}

	return err
}
