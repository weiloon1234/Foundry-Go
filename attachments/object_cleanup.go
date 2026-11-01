package attachments

import (
	"context"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
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
			if _, err := orm.Update(ctx, tx, store.Files.Query(), store.Files.Key(), id, orm.TextSet(store.Files.State, string(Cleaned)), orm.TextSet(store.Files.LastFailure, "writer_fenced"), orm.TimeSet(store.Files.UpdatedAt, now)); err != nil {
				return err
			}
			ended = true
		} else {
			ended = row.State == string(Cleaned) || row.State == string(CleanupPending)
		}
		return nil
	})
	if err != nil || !ended {
		return err
	}
	return deleteEndedObject(ctx, disk, key, temporal.DateTime{})
}

func (m *Manager) fenceVariant(ctx context.Context, original store.File, id model.ID[store.Variant], disk *storage.Disk, key storage.ObjectKey) error {
	ctx, cancel := m.cleanupContext(ctx)
	defer cancel()
	ended := false
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
			if _, err := orm.Update(ctx, tx, store.Variants.Query(), store.Variants.Key(), id, orm.TextSet(store.Variants.State, string(VariantCleaned)), orm.TextSet(store.Variants.LastFailure, "writer_fenced"), orm.TimeSet(store.Variants.UpdatedAt, now)); err != nil {
				return err
			}
			ended = true
		} else {
			ended = row.State == string(VariantCleaned) || row.State == string(VariantCleanup)
		}
		return nil
	})
	if err != nil || !ended {
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
// Rows are committed before Put and IDs are never reused, so absence cannot race
// with a new legitimate publication of the same key. Every nonterminal row,
// including Writing, Uncertain and Retained, prevents deletion. Cleaned is an
// irreversible tombstone, allowing recovery of a late provider create.
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
	var referenced map[string]bool
	if len(keys) > 0 {
		err = m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			var err error
			referenced, err = objectReferences(ctx, tx, target.disk.ID(), keys, false)
			return err
		})
		if err != nil {
			return err
		}
	}
	var failures error
	for _, keyText := range keys {
		if referenced[keyText] {
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

// objectReferences batches both journals so inspection and sweeping agree about
// variant ownership. Inspection includes tombstones; reclamation excludes only
// irreversible Cleaned rows. Unknown states conservatively protect their keys.
func objectReferences(ctx context.Context, tx *database.Tx, disk storage.DiskID, keys []string, includeCleaned bool) (map[string]bool, error) {
	f, v := store.Files, store.Variants
	files := f.Query().Where(orm.TextEq(f.Disk, string(disk)), orm.TextIn(f.ObjectKey, keys...))
	variants := v.Query().Where(orm.TextEq(v.Disk, string(disk)), orm.TextIn(v.ObjectKey, keys...))
	if !includeCleaned {
		files = files.Where(orm.TextNe(f.State, string(Cleaned)))
		variants = variants.Where(orm.TextNe(v.State, string(VariantCleaned)))
	}
	originals, err := rows.Read(rows.SelectValue(files.Limit(storage.MaxPageSize+1), orm.TextExpr(f.ObjectKey))).All(ctx, tx)
	if err != nil {
		return nil, err
	}
	derived, err := rows.Read(rows.SelectValue(variants.Limit(storage.MaxPageSize+1), orm.TextExpr(v.ObjectKey))).All(ctx, tx)
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]bool, len(originals)+len(derived))
	for _, key := range originals {
		referenced[key] = true
	}
	for _, key := range derived {
		referenced[key] = true
	}
	return referenced, nil
}
