package generate

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

func fieldDocumentationName(name string) bool {
	return filepath.Base(name) == name && strings.HasSuffix(name, ".go") && !outputName.MatchString(name)
}

func addFieldDocumentation(p *packageInput, plan *writePlan, updates map[string][]byte) error {
	plan.fieldNotes = make(map[string]bool, len(updates))
	for name, data := range updates {
		old, err := readFile(filepath.Join(p.dir, name))
		if err != nil {
			return err
		}
		matched := false
		for _, source := range p.files {
			if source.name == name && old.exists && bytes.Equal(old.data, source.data) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("field documentation source %s changed during generation; retry", name)
		}
		if _, exists := plan.before[name]; exists {
			return fmt.Errorf("field documentation overlaps a generator-owned target")
		}
		plan.before[name], plan.changes[name], plan.fieldNotes[name] = old, data, true
	}
	return validateFieldDocumentationPlan(*plan)
}

func validateFieldDocumentationPlan(plan writePlan) error {
	for name := range plan.fieldNotes {
		before, after := plan.before[name], plan.changes[name]
		if plan.scaffold || !fieldDocumentationName(name) || !before.exists || after == nil {
			return fmt.Errorf("invalid field documentation target")
		}
		if err := validateFieldDocumentationChange(before.data, after); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// Version 4 journals classify source notices separately from owned outputs.
// Backups/staged data must prove this was a comment-only edit before recovery
// may restore a handwritten file, even when publication otherwise completed.
func fieldDocumentationProof(name string) string { return stageName("field-notes-proof", name) }

func validateRecoveryFieldDocumentation(dir string, log journal, current map[string]oldFile) error {
	for _, name := range log.FieldNotes {
		entry := log.Files[name]
		before, err := readWithin(dir, filepath.Join(lockName, stageName("old", name)))
		if err != nil {
			return err
		}
		if !entry.Before.matches(before) {
			return fmt.Errorf("invalid field documentation backup for %s; staging retained", name)
		}
		proofPath := filepath.Join(lockName, fieldDocumentationProof(name))
		proof, err := readWithin(dir, proofPath)
		if err != nil {
			return err
		}
		after := proof
		if !proof.exists {
			after = current[name]
			if !entry.After.matches(after) {
				after, err = readWithin(dir, filepath.Join(lockName, stageName("new", name)))
				if err != nil {
					return err
				}
				// Old writers consumed the only new bytes during publication.
				// If an earlier recovery already restored this exact old target,
				// leave it alone; no handwritten restoration is authorized here.
				if !after.exists && entry.Before.matches(current[name]) {
					continue
				}
			}
		}
		if !entry.After.matches(after) {
			return fmt.Errorf("invalid staged field documentation for %s; staging retained", name)
		}
		if err := validateFieldDocumentationChange(before.data, after.data); err != nil {
			return fmt.Errorf("unsafe field documentation recovery for %s: %w", name, err)
		}
		if !proof.exists {
			// Upgrade old staging before the first restoration, so another
			// interruption cannot consume the validated new-state evidence.
			if err := writeSynced(filepath.Join(dir, proofPath), after.data, after.mode); err != nil {
				return err
			}
			if err := syncDir(filepath.Join(dir, lockName)); err != nil {
				return err
			}
		}
	}
	return nil
}
