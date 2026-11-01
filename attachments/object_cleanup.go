package attachments

import (
	"context"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Fencing runs after a rejected publication, including a lost acknowledgement.
// A fenced unresolved row becomes a durable tombstone before storage I/O. Ready,
// stored and retained rows are preserved even when a commit acknowledgement was
// lost. A provider that completes after this check is recovered by sweepObjects.
func (m *Manager) fenceOriginal(ctx context.Context, id model.ID[store.File], disk *storage.Disk, key storage.ObjectKey) error {
	ctx, cancel := m.cleanupContext(ctx)
	defer cancel()
	ended := false
	var retiredAt temporal.DateTime
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		ended = false
		row, err := orm.RequireLocked(ctx, tx, orm.Lock(store.Files.Query()).Where(orm.KeyEq(store.Files.Key(), id)))
		if err != nil {
			return err
		}
		if _, err := m.validateRow(row); err != nil {
			return err
		}
		if row.ObjectKey != key.String() || row.Disk != string(disk.ID()) {
			return invalid()
		}
		if (row.State == string(Writing) || row.State == string(Uncertain)) && row.WriterID != m.writer {
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			row, err = orm.Update(ctx, tx, store.Files.Query(), store.Files.Key(), id, orm.TextSet(store.Files.State, string(Cleaned)), orm.TextSet(store.Files.LastFailure, "writer_fenced"), orm.TimeSet(store.Files.UpdatedAt, now))
			if err != nil {
				return err
			}
			ended = true
			retiredAt = row.UpdatedAt
		} else {
			ended = row.State == string(Cleaned) || row.State == string(CleanupPending)
			retiredAt = row.UpdatedAt
		}
		return nil
	})
	if err != nil || !ended {
		return err
	}
	eligible, err := m.cleanupEligible(retiredAt)
	if err != nil || !eligible {
		return err
	}
	return deleteEndedObject(ctx, disk, key, temporal.DateTime{})
}

func (m *Manager) fenceVariant(ctx context.Context, original store.File, id model.ID[store.Variant], disk *storage.Disk, key storage.ObjectKey) error {
	ctx, cancel := m.cleanupContext(ctx)
	defer cancel()
	ended := false
	var retiredAt temporal.DateTime
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		ended = false
		row, err := orm.RequireLocked(ctx, tx, orm.Lock(store.Variants.Query()).Where(orm.KeyEq(store.Variants.Key(), id)))
		if err != nil {
			return err
		}
		if err := m.validateVariant(row, original); err != nil {
			return err
		}
		if row.ObjectKey != key.String() || row.Disk != string(disk.ID()) {
			return invalid()
		}
		if (row.State == string(VariantWriting) || row.State == string(VariantUncertain)) && row.WriterID != m.writer {
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			row, err = orm.Update(ctx, tx, store.Variants.Query(), store.Variants.Key(), id, orm.TextSet(store.Variants.State, string(VariantCleaned)), orm.TextSet(store.Variants.LastFailure, "writer_fenced"), orm.TimeSet(store.Variants.UpdatedAt, now))
			if err != nil {
				return err
			}
			ended = true
			retiredAt = row.UpdatedAt
		} else {
			ended = row.State == string(VariantCleaned) || row.State == string(VariantCleanup)
			retiredAt = row.UpdatedAt
		}
		return nil
	})
	if err != nil || !ended {
		return err
	}
	eligible, err := m.cleanupEligible(retiredAt)
	if err != nil || !eligible {
		return err
	}
	return deleteEndedObject(ctx, disk, key, temporal.DateTime{})
}

// Stat supplies the current pin on conditional-delete disks. The caller has
// already proved this key can never be published again. No general-purpose
// check-then-delete emulation is exposed through storage.
func deleteEndedObject(ctx context.Context, disk *storage.Disk, key storage.ObjectKey, cutoff temporal.DateTime) error {
	info, err := disk.Stat(ctx, key, storage.ReadOptions{})
	if callback.ErrorIs(err, storage.NotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !cutoff.UTC().IsZero() && info.Modified.UTC().After(cutoff.UTC()) {
		return nil
	}
	return disk.Delete(ctx, key, attachmentDelete(disk, string(info.Version), string(info.ETag)))
}

type objectSweepTarget struct {
	disk   *storage.Disk
	prefix storage.Prefix
	cursor storage.Cursor
}

func (m *Manager) initObjectSweep() error {
	seen := make(map[string]bool)
	for _, collection := range m.collections {
		scope, err := m.store.Registry().Scope(collection.owner)
		if err != nil {
			return err
		}
		disk, err := collection.policy.Disk.Resolve(m.disks)
		if err != nil {
			return err
		}
		prefix, err := storage.ParsePrefix(objectPrefix + scope + "/")
		if err != nil {
			return err
		}
		identity := string(disk.ID()) + "/" + prefix.String()
		if seen[identity] {
			continue
		}
		seen[identity] = true
		m.orphanTargets = append(m.orphanTargets, objectSweepTarget{disk: disk, prefix: prefix})
	}
	sort.Slice(m.orphanTargets, func(i, j int) bool {
		a, b := m.orphanTargets[i], m.orphanTargets[j]
		if a.disk.ID() != b.disk.ID() {
			return a.disk.ID() < b.disk.ID()
		}
		return a.prefix.String() < b.prefix.String()
	})
	return nil
}

// sweepObjects visits one bounded page per call, rotating registered disk/scope
// pairs and retaining each cursor. Repeated scheduled calls progress past live
// objects; restart safely starts over. Only canonical attachment IDs are eligible.
// Only a durable Cleaned tombstone in this database authorizes deletion. Missing
// rows can belong to another database sharing the namespace and never authorize
// reclamation. Every other state protects its key, including unknown states.
func (m *Manager) sweepObjects(ctx context.Context, cutoff temporal.DateTime, limit int) error {
	if !m.orphanMu.TryLock() {
		return nil
	}
	defer m.orphanMu.Unlock()
	if len(m.orphanTargets) == 0 {
		return nil
	}
	target := &m.orphanTargets[m.orphanNext]
	m.orphanNext = (m.orphanNext + 1) % len(m.orphanTargets)
	page, err := target.disk.List(ctx, storage.ListOptions{Prefix: target.prefix, Cursor: target.cursor, Limit: limit})
	if err != nil {
		target.cursor = storage.Cursor{}
		return err
	}
	keys := make([]string, 0, len(page.Objects))
	for _, object := range page.Objects {
		if canonicalAttachmentKey(target.prefix, object.Key) && !object.Modified.UTC().IsZero() && !object.Modified.UTC().After(cutoff.UTC()) {
			keys = append(keys, object.Key.String())
		}
	}
	var referenced map[string]objectReference
	cleanupCutoff, err := m.cleanupCutoff()
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		err = m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			var err error
			referenced, err = objectReferences(ctx, tx, target.disk.ID(), keys)
			return err
		})
		if err != nil {
			return err
		}
	}
	var failures error
	for _, keyText := range keys {
		reference, known := referenced[keyText]
		if !known || !reference.cleaned || reference.updatedAt.UTC().After(cleanupCutoff.UTC()) {
			continue
		}
		key, err := storage.ParseKey(keyText)
		if err == nil {
			err = deleteEndedObject(ctx, target.disk, key, cutoff)
		}
		failures = joinFailures(failures, err)
	}

	// Retry this page after failures, rather than stranding failed deletions.
	if failures == nil {
		target.cursor = page.Next
	}
	return failures
}

// Only the exact framework key grammar is eligible for automatic deletion.
func canonicalAttachmentKey(prefix storage.Prefix, key storage.ObjectKey) bool {
	if !prefix.Contains(key) {
		return false
	}
	suffix := strings.TrimPrefix(key.String(), prefix.String())
	suffix = strings.TrimPrefix(suffix, "variants/")
	id, err := model.ParseID[store.File](suffix)
	return err == nil && !id.IsZero() && id.String() == suffix
}

// cleanupEligible uses the journal timestamp, never object creation time: a
// long-lived ready object must still survive a full grace after retirement.
func (m *Manager) cleanupEligible(retiredAt temporal.DateTime) (bool, error) {
	cutoff, err := m.cleanupCutoff()
	return err == nil && !retiredAt.UTC().After(cutoff.UTC()), err
}

func (m *Manager) cleanupCutoff() (temporal.DateTime, error) {
	now, err := m.store.Now()
	if err != nil {
		return temporal.DateTime{}, err
	}
	return temporal.NewDateTime(now.UTC().Add(-m.config.CleanupGrace))
}

type objectReference struct {
	cleaned   bool
	updatedAt temporal.DateTime
}

type objectJournal struct {
	Key       string
	State     string
	UpdatedAt temporal.DateTime
}

type objectJournalFields struct {
	rows.Row[objectJournal]
	Key       rows.Field[objectJournal, string]
	State     rows.Field[objectJournal, string]
	UpdatedAt rows.Field[objectJournal, temporal.DateTime]
}

var objectJournalColumns = rows.Define(func(b rows.Builder[objectJournal], r *objectJournal) objectJournalFields {
	return objectJournalFields{b.Row(), rows.TextOf(b, &r.Key), rows.TextOf(b, &r.State), rows.TimeOf(b, &r.UpdatedAt)}
})

// objectReferences batches both journals for inspection and reclamation. Missing
// keys are unknown, not retired. A nonterminal reference always protects a key.
func objectReferences(ctx context.Context, tx *database.Tx, disk storage.DiskID, keys []string) (map[string]objectReference, error) {
	f, v, c := store.Files, store.Variants, objectJournalColumns
	files := f.Query().Where(orm.TextEq(f.Disk, string(disk)), orm.TextIn(f.ObjectKey, keys...)).Limit(storage.MaxPageSize + 1)
	variants := v.Query().Where(orm.TextEq(v.Disk, string(disk)), orm.TextIn(v.ObjectKey, keys...)).Limit(storage.MaxPageSize + 1)
	originals, err := rows.Read(rows.Select(rows.From(files), c, rows.Map(c.Key, expr.Value(orm.TextExpr(f.ObjectKey))), rows.Map(c.State, expr.Value(orm.TextExpr(f.State))), rows.Map(c.UpdatedAt, expr.Value(orm.TimeExpr(f.UpdatedAt))))).All(ctx, tx)
	if err != nil {
		return nil, err
	}
	derived, err := rows.Read(rows.Select(rows.From(variants), c, rows.Map(c.Key, expr.Value(orm.TextExpr(v.ObjectKey))), rows.Map(c.State, expr.Value(orm.TextExpr(v.State))), rows.Map(c.UpdatedAt, expr.Value(orm.TimeExpr(v.UpdatedAt))))).All(ctx, tx)
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]objectReference, len(originals)+len(derived))
	add := func(row objectJournal, cleaned bool) {
		previous, exists := referenced[row.Key]
		if exists {
			cleaned = cleaned && previous.cleaned
			if previous.updatedAt.UTC().After(row.UpdatedAt.UTC()) {
				row.UpdatedAt = previous.updatedAt
			}
		}
		referenced[row.Key] = objectReference{cleaned: cleaned, updatedAt: row.UpdatedAt}
	}
	for _, row := range originals {
		add(row, row.State == string(Cleaned))
	}
	for _, row := range derived {
		add(row, row.State == string(VariantCleaned))
	}
	return referenced, nil
}
