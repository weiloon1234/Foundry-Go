package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/imaging"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Variant objects live under their own framework prefix, so storage
// inspection of originals never reports them as untracked.
const variantPrefix = "foundry-attachment-variants/"

const (
	// MaxVariants bounds declared variants per collection.
	MaxVariants = 8
	// maxVariantIntents bounds unresolved variant journal rows per original.
	maxVariantIntents = 64
)

// VariantName is a stable semantic identifier stored with each derived file.
type VariantName string

func (n VariantName) Validate() error {
	if len(n) > 64 || !identifier.Semantic(string(n)) {
		return invalid()
	}
	return nil
}

// Variant declares a named derived image, for example a thumbnail or preview.
// It is generated from each ready original with an imaging plan and stored
// beside it; the original is never changed. Declare it once and reuse the
// same value in collection policies and link helpers.
type Variant struct {
	name VariantName
	plan imaging.Plan
}

func DefineVariant(name VariantName, plan imaging.Plan) Variant {
	return Variant{name: name, plan: plan}
}
func (v Variant) Name() VariantName { return v.name }
func (v Variant) Validate() error {
	if err := v.name.Validate(); err != nil {
		return err
	}
	return v.plan.Validate()
}
func (Variant) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("attachment variant declaration")) }

// VariantInfo describes one generated variant of a loaded attachment.
type VariantInfo struct {
	Name          VariantName
	MediaType     storage.MediaType
	Size          int64
	Width, Height int
}

// VariantUnavailable reports a declared variant that has not been generated
// (yet) for an attachment, for example while its job is queued. It matches
// fault.Missing.
var VariantUnavailable = fault.New(fault.Missing, "attachment variant is not available")

type storedVariant struct {
	info    VariantInfo
	disk    storage.DiskID
	key     storage.ObjectKey
	etag    storage.ETag
	version storage.VersionID
}

// Variants returns the attachment's generated variants, ordered by name.
func (a Attachment[M, K]) Variants() []VariantInfo {
	result := make([]VariantInfo, len(a.variants))
	for i, stored := range a.variants {
		result[i] = stored.info
	}
	return result
}

// Variant reports whether the declared variant has been generated.
func (a Attachment[M, K]) Variant(variant Variant) (VariantInfo, bool) {
	for _, stored := range a.variants {
		if stored.info.Name == variant.name {
			return stored.info, true
		}
	}
	return VariantInfo{}, false
}

func (p Policy) validateVariants() error {
	if len(p.Variants) == 0 {
		return nil
	}
	if len(p.Variants) > MaxVariants || p.AnyMedia {
		return invalid()
	}
	if !p.Image.IsSet() {
		for _, media := range p.Accepted {
			if !decodableImage(media) {
				return invalid()
			}
		}
	}
	seen := make(map[VariantName]bool, len(p.Variants))
	for _, variant := range p.Variants {
		if err := variant.Validate(); err != nil {
			return err
		}
		if seen[variant.name] {
			return invalid()
		}
		seen[variant.name] = true
	}
	return nil
}

// decodableImage reports an accepted media type the imaging engine can decode.
func decodableImage(media storage.MediaType) bool {
	for _, format := range []imaging.Format{imaging.JPEG, imaging.PNG, imaging.WebP, imaging.GIF, imaging.BMP, imaging.TIFF, imaging.ICO} {
		if format.CanDecode() && string(media) == format.MediaType() {
			return true
		}
	}
	return false
}

func (p Policy) variant(name VariantName) (Variant, bool) {
	for _, variant := range p.Variants {
		if variant.name == name {
			return variant, true
		}
	}
	return Variant{}, false
}

func variantKey(scope string, id model.ID[store.Variant]) (storage.ObjectKey, error) {
	return storage.ParseKey(variantPrefix + scope + "/" + id.String())
}

// validateVariant treats a variant row as a checked persistence boundary
// against its original's scope and disk before any storage effect.
func (m *Manager) validateVariant(row store.Variant, original store.File) error {
	if row.ID.IsZero() || row.FileID != original.ID || row.Disk != original.Disk || row.WriterID.IsZero() || VariantName(row.Name).Validate() != nil || VariantState(row.State).Validate() != nil {
		return invalid()
	}
	expected, err := variantKey(original.Scope, row.ID)
	if err != nil || expected.String() != row.ObjectKey {
		return invalid()
	}
	if row.Size < 0 || row.Size > MaxUploadBytes || row.Width < 0 || row.Height < 0 || row.Width > 65535 || row.Height > 65535 {
		return invalid()
	}
	if storage.MediaType(row.ContentType).Validate() != nil || storage.ETag(row.ETag).Validate() != nil || storage.VersionID(row.ObjectVersion).Validate() != nil {
		return invalid()
	}
	digest, err := storage.ParseSHA256(row.Digest)
	if err != nil || digest.String() != row.Digest {
		return invalid()
	}
	if (row.State == string(VariantReady) || row.State == string(VariantCleanup)) && row.ETag == "" {
		return invalid()
	}
	return nil
}

// VariantState is the persisted journal state of one derived file.
type VariantState string

const (
	VariantWriting   VariantState = "writing"
	VariantReady     VariantState = "ready"
	VariantCleanup   VariantState = "cleanup"
	VariantCleaned   VariantState = "cleaned"
	VariantUncertain VariantState = "uncertain"
)

func (s VariantState) Validate() error {
	switch s {
	case VariantWriting, VariantReady, VariantCleanup, VariantCleaned, VariantUncertain:
		return nil
	}
	return invalid()
}

func (m *Manager) registrationOf(row store.File) (Registration, error) {
	for _, registration := range m.collections {
		if registration.name == Name(row.Collection) && string(registration.owner) == row.Owner {
			return registration, nil
		}
	}
	return Registration{}, invalid()
}
func (m *Manager) markVariantWriting(id model.ID[store.Variant]) func() {
	m.mu.Lock()
	m.variantWriting[id] = true
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.variantWriting, id)
		m.mu.Unlock()
	}
}

// GenerateVariants derives every declared variant still missing for one ready
// attachment. It is idempotent and is the handler of the variant job; an
// original that is no longer ready has nothing to generate.
func (m *Manager) GenerateVariants(ctx context.Context, id OperationID) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if id.IsZero() {
		return invalid()
	}
	return m.calls.Run(ctx, "attachment variant generation", func(ctx context.Context) error {
		return m.generate(ctx, fileID(id), false)
	})
}

// generate derives variants of one ready original. With replace every
// declared variant is regenerated; otherwise only those without a ready file.
func (m *Manager) generate(ctx context.Context, id model.ID[store.File], replace bool) error {
	original, variants, err := m.variantSource(ctx, id, replace)
	if err != nil || len(variants) == 0 {
		return err
	}
	data, err := m.readOriginal(ctx, original)
	if err != nil {
		return err
	}
	var failures error
	for _, variant := range variants {
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		failures = errors.Join(failures, m.produceVariant(ctx, original, variant, data))
	}
	return failures
}

func (m *Manager) variantSource(ctx context.Context, id model.ID[store.File], replace bool) (store.File, []Variant, error) {
	var original store.File
	var selected []Variant
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		original, err = store.QueryFoundryAttachments().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if original.State != string(Ready) {
			return nil
		}
		if _, err := m.validateRow(original); err != nil {
			return err
		}
		registration, err := m.registrationOf(original)
		if err != nil {
			return err
		}
		selected = slices.Clone(registration.policy.Variants)
		if replace || len(selected) == 0 {
			return nil
		}
		v := store.VariantFields()
		names, err := query.SelectValue(store.QueryFoundryAttachmentVariants().Where(v.FileID.Eq(id), v.State.Eq(string(VariantReady))).Limit(MaxVariants*2), v.Name.Value()).All(ctx, tx)
		if err != nil {
			return err
		}
		selected = slices.DeleteFunc(selected, func(variant Variant) bool { return slices.Contains(names, string(variant.name)) })
		return nil
	})
	return original, selected, err
}

// readOriginal reads the pinned original and verifies its length and digest.
func (m *Manager) readOriginal(ctx context.Context, original store.File) ([]byte, error) {
	disk, err := m.disks.Disk(storage.DiskID(original.Disk))
	if err != nil {
		return nil, err
	}
	key, err := storage.ParseKey(original.ObjectKey)
	if err != nil {
		return nil, err
	}
	digest, err := storage.ParseSHA256(original.Digest)
	if err != nil {
		return nil, err
	}
	data, info, err := disk.ReadBytes(ctx, key, original.Size, storage.ReadOptions{IfMatch: storage.ETag(original.ETag), Version: storage.VersionID(original.ObjectVersion)})
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != original.Size || !diskVerified(info, digest) && storage.SHA256(sha256.Sum256(data)) != digest {
		return nil, storage.Failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	return data, nil
}

var errOriginalSuperseded = fault.New(fault.Conflict, "attachment original is no longer ready")

// produceVariant writes one derived file under its own intent and publishes
// it only while the original is still ready, retiring the previous ready
// variant of the same name.
func (m *Manager) produceVariant(ctx context.Context, original store.File, variant Variant, data []byte) error {
	output, err := m.image.ProcessBytes(ctx, data, variant.plan)
	if err != nil {
		return err
	}
	if output.Size() > MaxUploadBytes {
		return storage.Failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, output.Reader()); err != nil {
		return err
	}
	var digest storage.SHA256
	copy(digest[:], hash.Sum(nil))
	id, err := model.NewID[store.Variant]()
	if err != nil {
		return err
	}
	key, err := variantKey(original.Scope, id)
	if err != nil {
		return err
	}
	release := m.markVariantWriting(id)
	defer release()
	info := output.Info()
	media := storage.MediaType(info.Format.MediaType())
	if err := m.stageVariant(ctx, original, id, variant.name, key, media, output.Size(), digest, info); err != nil {
		if errors.Is(err, errOriginalSuperseded) {
			return nil
		}
		return err
	}
	disk, err := m.disks.Disk(storage.DiskID(original.Disk))
	if err != nil {
		return err
	}
	stored, putErr := disk.Put(ctx, key, output.Reader(), storage.PutOptions{ContentType: media, Size: value.Set(output.Size()), Checksum: value.Set(digest), Condition: storage.IfAbsent()})
	if putErr == nil && stored.Object.ETag == "" {
		putErr = storage.Failure(storage.IntegrityFailed, storage.PutOperation, storage.Applied, nil)
	}
	if putErr != nil {
		return errors.Join(putErr, m.recordVariantFailure(ctx, id, putErr))
	}
	publishCtx, cancel := m.cleanupContext(ctx)
	defer cancel()
	retired, err := m.publishVariant(publishCtx, original.ID, id, stored.Object)
	for _, previous := range retired {
		err = errors.Join(err, m.cleanupVariant(publishCtx, previous))
	}
	return err
}

func (m *Manager) stageVariant(ctx context.Context, original store.File, id model.ID[store.Variant], name VariantName, key storage.ObjectKey, media storage.MediaType, size int64, digest storage.SHA256, info imaging.Info) error {
	return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		current, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, original.ID)
		if err != nil {
			return err
		}
		if current.State != string(Ready) || current.ETag != original.ETag || current.ObjectVersion != original.ObjectVersion {
			return errOriginalSuperseded
		}
		v := store.VariantFields()
		count, err := store.QueryFoundryAttachmentVariants().Where(v.FileID.Eq(original.ID), v.State.Ne(string(VariantCleaned))).Count(ctx, tx)
		if err != nil {
			return err
		}
		if count >= maxVariantIntents {
			return fault.New(fault.Conflict, "attachment has too many unresolved variant intents")
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundryAttachmentVariants().Create(ctx, tx, store.VariantDraft{}.SetID(id).SetFileID(original.ID).SetName(string(name)).SetDisk(original.Disk).SetObjectKey(key.String()).SetContentType(string(media)).SetSize(size).SetDigest(digest.String()).SetETag("").SetObjectVersion("").SetWidth(int32(info.Width)).SetHeight(int32(info.Height)).SetState(string(VariantWriting)).SetWriterID(m.writer).SetAttempts(0).SetLastFailure("").SetCreatedAt(now).SetUpdatedAt(now))
		return err
	})
}

// recordVariantFailure mirrors original uploads: only an explicit Unchanged
// result proves nothing was published; everything else awaits settlement.
func (m *Manager) recordVariantFailure(ctx context.Context, id model.ID[store.Variant], cause error) error {
	next := VariantUncertain
	if detail, ok := cause.(*storage.Error); ok && detail.Outcome() == storage.Unchanged {
		next = VariantCleaned
	}
	cleanupCtx, cancel := m.cleanupContext(ctx)
	defer cancel()
	return m.store.Write(cleanupCtx, func(ctx context.Context, tx *database.Tx) error {
		row, err := store.QueryFoundryAttachmentVariants().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if row.WriterID != m.writer || row.State != string(VariantWriting) {
			return invalid()
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundryAttachmentVariants().Update(ctx, tx, id, store.VariantDraft{}.SetState(string(next)).SetLastFailure("upload_failed").SetUpdatedAt(now))
		return err
	})
}

// publishVariant makes an acknowledged variant ready and retires the previous
// ready variant of that name. If the original stopped being ready meanwhile,
// the new variant itself is retired. Retired rows are returned for cleanup.
func (m *Manager) publishVariant(ctx context.Context, fileID model.ID[store.File], id model.ID[store.Variant], object storage.ObjectInfo) ([]model.ID[store.Variant], error) {
	var retired []model.ID[store.Variant]
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		retired = nil
		original, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, fileID)
		if err != nil {
			return err
		}
		row, err := store.QueryFoundryAttachmentVariants().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := m.validateVariant(row, original); err != nil {
			return err
		}
		checksum, ok := object.Checksum.Get()
		if row.State != string(VariantWriting) || row.WriterID != m.writer || row.ObjectKey != object.Key.String() || row.Size != object.Size || !ok || checksum.String() != row.Digest {
			return invalid()
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		pinned := store.VariantDraft{}.SetETag(string(object.ETag)).SetObjectVersion(string(object.Version)).SetUpdatedAt(now)
		if original.State != string(Ready) {
			if _, err := store.QueryFoundryAttachmentVariants().Update(ctx, tx, id, pinned.SetState(string(VariantCleanup)).SetLastFailure("original_superseded")); err != nil {
				return err
			}
			retired = append(retired, id)
			return nil
		}
		v := store.VariantFields()
		previous, err := store.QueryFoundryAttachmentVariants().Where(v.FileID.Eq(fileID), v.Name.Eq(row.Name), v.State.Eq(string(VariantReady))).ForUpdate().All(ctx, tx)
		if err != nil {
			return err
		}
		for _, old := range previous {
			if _, err := store.QueryFoundryAttachmentVariants().Update(ctx, tx, old.ID, store.VariantDraft{}.SetState(string(VariantCleanup)).SetUpdatedAt(now)); err != nil {
				return err
			}
			retired = append(retired, old.ID)
		}
		_, err = store.QueryFoundryAttachmentVariants().Update(ctx, tx, id, pinned.SetState(string(VariantReady)))
		return err
	})
	return retired, err
}

// retireVariants moves an original's ready variants to cleanup inside the
// caller's transaction and returns every variant awaiting cleanup.
func (m *Manager) retireVariants(ctx context.Context, tx *database.Tx, fileID model.ID[store.File]) ([]model.ID[store.Variant], error) {
	v := store.VariantFields()
	rows, err := store.QueryFoundryAttachmentVariants().Where(v.FileID.Eq(fileID), v.State.In(string(VariantReady), string(VariantCleanup))).OrderBy(v.ID.Asc()).Limit(maxVariantIntents).ForUpdate().All(ctx, tx)
	if err != nil {
		return nil, err
	}
	now, err := m.store.Now()
	if err != nil {
		return nil, err
	}
	ids := make([]model.ID[store.Variant], 0, len(rows))
	for _, row := range rows {
		if row.State == string(VariantReady) {
			if _, err := store.QueryFoundryAttachmentVariants().Update(ctx, tx, row.ID, store.VariantDraft{}.SetState(string(VariantCleanup)).SetUpdatedAt(now)); err != nil {
				return nil, err
			}
		}
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// cleanupVariantsOf retires and deletes every derived file of an original
// that is leaving ready ownership. Any failure keeps the caller pending.
func (m *Manager) cleanupVariantsOf(ctx context.Context, fileID model.ID[store.File]) error {
	var ids []model.ID[store.Variant]
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		ids, err = m.retireVariants(ctx, tx, fileID)
		return err
	})
	if err != nil {
		return err
	}
	var failures error
	for _, id := range ids {
		failures = errors.Join(failures, m.cleanupVariant(ctx, id))
	}
	return failures
}

// cleanupVariant deletes exactly the pinned object of a retired variant and
// journals the result. Confirmed absence is success.
func (m *Manager) cleanupVariant(ctx context.Context, id model.ID[store.Variant]) error {
	var row store.Variant
	var original store.File
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		if row, err = store.QueryFoundryAttachmentVariants().RequireFind(ctx, tx, id); err != nil {
			return err
		}
		original, err = store.QueryFoundryAttachments().RequireFind(ctx, tx, row.FileID)
		return err
	})
	if err != nil {
		return err
	}
	if row.State != string(VariantCleanup) {
		return nil
	}
	if err := m.validateVariant(row, original); err != nil {
		return err
	}
	disk, err := m.disks.Disk(storage.DiskID(row.Disk))
	if err != nil {
		return err
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		return err
	}
	deleteErr := disk.Delete(ctx, key, pinnedDelete(row.ObjectVersion, row.ETag))
	if deleteErr != nil && (errorgraph.Is(deleteErr, storage.PreconditionFailed) || errorgraph.Is(deleteErr, storage.NotFound)) {
		_, statErr := disk.Stat(ctx, key, storage.ReadOptions{Version: storage.VersionID(row.ObjectVersion)})
		if errorgraph.Is(statErr, storage.NotFound) {
			deleteErr = nil
		} else if statErr != nil {
			deleteErr = errors.Join(deleteErr, statErr)
		}
	}
	journalCtx, cancel := m.cleanupContext(ctx)
	defer cancel()
	journalErr := m.store.Write(journalCtx, func(ctx context.Context, tx *database.Tx) error {
		current, err := store.QueryFoundryAttachmentVariants().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.State == string(VariantCleaned) {
			deleteErr = nil
			return nil
		}
		if current.State != string(VariantCleanup) || current.ETag != row.ETag || current.ObjectVersion != row.ObjectVersion {
			return invalid()
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		attempts := current.Attempts
		if attempts < 1<<32-1 {
			attempts++
		}
		draft := store.VariantDraft{}.SetAttempts(attempts).SetUpdatedAt(now)
		if deleteErr == nil {
			draft = draft.SetState(string(VariantCleaned)).SetLastFailure("")
		} else {
			draft = draft.SetLastFailure("storage_delete_failed")
		}
		_, err = store.QueryFoundryAttachmentVariants().Update(ctx, tx, id, draft)
		return err
	})
	return errors.Join(deleteErr, journalErr)
}

// sweepVariants retries variant cleanup and settles variant intents whose
// writer and provider request are bounded by the settlement cutoff. Variants
// are derived and never ready while unresolved, so a present object of an
// aged intent is deleted instead of published.
func (m *Manager) sweepVariants(ctx context.Context, cutoff temporal.DateTime, limit int) error {
	m.mu.Lock()
	active := make(map[model.ID[store.Variant]]bool, len(m.variantWriting))
	for id := range m.variantWriting {
		active[id] = true
	}
	m.mu.Unlock()
	var rows []store.Variant
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		v := store.VariantFields()
		found, err := store.QueryFoundryAttachmentVariants().Where(query.Or(v.State.Eq(string(VariantCleanup)), query.And(v.State.In(string(VariantWriting), string(VariantUncertain)), v.UpdatedAt.Lte(cutoff)))).OrderBy(v.UpdatedAt.Asc(), v.ID.Asc()).Limit(limit+len(active)).All(ctx, tx)
		for _, row := range found {
			if !active[row.ID] && len(rows) < limit {
				rows = append(rows, row)
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	var failures error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		if row.State == string(VariantCleanup) {
			failures = errors.Join(failures, m.cleanupVariant(ctx, row.ID))
		} else {
			failures = errors.Join(failures, m.settleVariant(ctx, row.ID, cutoff))
		}
	}
	return failures
}

func (m *Manager) settleVariant(ctx context.Context, id model.ID[store.Variant], cutoff temporal.DateTime) error {
	var row store.Variant
	var original store.File
	fenced := false
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		if row, err = store.QueryFoundryAttachmentVariants().ForUpdate().RequireFind(ctx, tx, id); err != nil {
			return err
		}
		if original, err = store.QueryFoundryAttachments().RequireFind(ctx, tx, row.FileID); err != nil {
			return err
		}
		if err := m.validateVariant(row, original); err != nil {
			return err
		}
		if row.State != string(VariantWriting) && row.State != string(VariantUncertain) || row.UpdatedAt.UTC().After(cutoff.UTC()) {
			return nil
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		// Fence the original writer before inspecting storage.
		row, err = store.QueryFoundryAttachmentVariants().Update(ctx, tx, id, store.VariantDraft{}.SetState(string(VariantUncertain)).SetWriterID(m.writer).SetLastFailure("automatic_settlement").SetUpdatedAt(now))
		fenced = err == nil
		return err
	})
	if err != nil || !fenced {
		return err
	}
	disk, err := m.disks.Disk(storage.DiskID(row.Disk))
	if err != nil {
		return err
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		return err
	}
	info, statErr := disk.Stat(ctx, key, storage.ReadOptions{})
	absent := errorgraph.Is(statErr, storage.NotFound)
	if statErr != nil && !absent {
		return statErr
	}
	err = m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		current, err := store.QueryFoundryAttachmentVariants().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.State != string(VariantUncertain) || current.WriterID != m.writer {
			return fault.New(fault.Conflict, "variant settlement changed concurrently")
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		draft := store.VariantDraft{}.SetUpdatedAt(now).SetLastFailure("")
		if absent {
			draft = draft.SetState(string(VariantCleaned))
		} else {
			draft = draft.SetState(string(VariantCleanup)).SetETag(string(info.ETag)).SetObjectVersion(string(info.Version))
		}
		_, err = store.QueryFoundryAttachmentVariants().Update(ctx, tx, id, draft)
		return err
	})
	if err != nil || absent {
		return err
	}
	return m.cleanupVariant(ctx, id)
}

// loadVariants attaches ready declared variants to loaded attachments using
// one bounded query. Variants no longer declared are not returned.
func (c Collection[M, K]) loadVariants(ctx context.Context, tx *database.Tx, m *Manager, files map[string][]Attachment[M, K]) error {
	if len(c.definition.policy.Variants) == 0 {
		return nil
	}
	var ids []model.ID[store.File]
	for _, list := range files {
		for _, file := range list {
			ids = append(ids, model.IDFromBytes[store.File](file.id.Bytes()))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	v := store.VariantFields()
	rows, err := store.QueryFoundryAttachmentVariants().Where(v.FileID.In(ids...), v.State.Eq(string(VariantReady))).OrderBy(v.FileID.Asc(), v.Name.Asc()).Limit(len(ids)*MaxVariants*2+1).All(ctx, tx)
	if err != nil {
		return err
	}
	if len(rows) > len(ids)*MaxVariants*2 {
		return fault.New(fault.Conflict, "attachment batch exceeds its variant limit")
	}
	byFile := make(map[[16]byte][]storedVariant, len(ids))
	for _, row := range rows {
		if _, declared := c.definition.policy.variant(VariantName(row.Name)); !declared {
			continue
		}
		key, err := storage.ParseKey(row.ObjectKey)
		if err != nil {
			return err
		}
		if VariantName(row.Name).Validate() != nil || storage.DiskID(row.Disk) != c.definition.policy.Disk.ID() || storage.MediaType(row.ContentType).Validate() != nil || storage.ETag(row.ETag).Validate() != nil || row.ETag == "" || storage.VersionID(row.ObjectVersion).Validate() != nil {
			return invalid()
		}
		expected, err := variantKey(c.definition.owner.Scope(), row.ID)
		if err != nil || expected != key {
			return invalid()
		}
		byFile[row.FileID.Bytes()] = append(byFile[row.FileID.Bytes()], storedVariant{info: VariantInfo{Name: VariantName(row.Name), MediaType: storage.MediaType(row.ContentType), Size: row.Size, Width: int(row.Width), Height: int(row.Height)}, disk: storage.DiskID(row.Disk), key: key, etag: storage.ETag(row.ETag), version: storage.VersionID(row.ObjectVersion)})
	}
	for subject, list := range files {
		for i := range list {
			list[i].variants = byFile[list[i].id.Bytes()]
		}
		files[subject] = list
	}
	return nil
}

// VariantPublicURLOf derives the public URL of a generated variant of a loaded
// attachment without database or storage I/O. It fails with
// VariantUnavailable until the variant has been generated.
func (c Collection[M, K]) VariantPublicURLOf(ctx context.Context, m *Manager, file Attachment[M, K], variant Variant) (string, error) {
	stored, err := c.variantOf(m, file, variant)
	if err != nil {
		return "", err
	}
	disk, err := m.disks.Disk(stored.disk)
	if err != nil {
		return "", err
	}
	return disk.PublicURL(ctx, stored.key)
}

// VariantTemporaryURLOf signs a read link for a generated variant, pinned to
// its stored version, without database or storage I/O.
func (c Collection[M, K]) VariantTemporaryURLOf(ctx context.Context, m *Manager, file Attachment[M, K], variant Variant, options storage.LinkOptions) (storage.TemporaryURL, error) {
	if options.Version != "" {
		return storage.TemporaryURL{}, invalid()
	}
	stored, err := c.variantOf(m, file, variant)
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	disk, err := m.disks.Disk(stored.disk)
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	options.Version = stored.version
	return disk.TemporaryURL(ctx, stored.key, options)
}
func (c Collection[M, K]) variantOf(m *Manager, file Attachment[M, K], variant Variant) (storedVariant, error) {
	if err := c.owns(m, file); err != nil {
		return storedVariant{}, err
	}
	if _, declared := c.definition.policy.variant(variant.name); !declared {
		return storedVariant{}, invalid()
	}
	for _, stored := range file.variants {
		if stored.info.Name == variant.name {
			return stored, nil
		}
	}
	return storedVariant{}, VariantUnavailable
}

// RegenerationOptions selects one bounded batch of ready attachments.
// Missing generates only variants without a ready file; otherwise every
// declared variant is regenerated and replaces the previous one.
type RegenerationOptions struct {
	Missing bool
	Cursor  RegenerationCursor
	Limit   int
}

// RegenerationCursor continues a regeneration run within one process.
type RegenerationCursor struct {
	owner      string
	collection string
	after      model.ID[store.File]
}

func (c RegenerationCursor) IsZero() bool { return c.after.IsZero() }

// RegenerationPage reports one batch. Failed attachments keep their previous
// variants; rerun them after resolving the cause. Continue with Next until it
// is zero.
type RegenerationPage struct {
	Regenerated []OperationID
	Failed      []OperationID
	Next        RegenerationCursor
}

const MaxRegenerationBatch = 100

// RegenerateVariants processes one batch of this collection's ready
// attachments in ID order under one write admission slot.
func (c Collection[M, K]) RegenerateVariants(ctx context.Context, m *Manager, options RegenerationOptions) (RegenerationPage, error) {
	if err := c.checkRegistered(m); err != nil {
		return RegenerationPage{}, err
	}
	return m.RegenerateVariants(ctx, c.definition.owner.Name(), c.Name(), options)
}

// RegenerateVariants is the name-addressed form used by operational commands.
func (m *Manager) RegenerateVariants(ctx context.Context, owner extensions.OwnerName, collection Name, options RegenerationOptions) (RegenerationPage, error) {
	if err := m.Validate(); err != nil {
		return RegenerationPage{}, err
	}
	var registration Registration
	found := false
	for _, candidate := range m.collections {
		if candidate.owner == owner && candidate.name == collection {
			registration, found = candidate, true
		}
	}
	if !found || len(registration.policy.Variants) == 0 || options.Limit < 1 || options.Limit > MaxRegenerationBatch {
		return RegenerationPage{}, invalid()
	}
	if !options.Cursor.IsZero() && (options.Cursor.owner != string(owner) || options.Cursor.collection != string(collection)) {
		return RegenerationPage{}, invalid()
	}
	var page RegenerationPage
	err := m.calls.Run(ctx, "attachment variant regeneration", func(ctx context.Context) error {
		var ids []model.ID[store.File]
		err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			f := store.FileFields()
			q := store.QueryFoundryAttachments().Where(f.Owner.Eq(string(owner)), f.Collection.Eq(string(collection)), f.State.Eq(string(Ready)))
			if !options.Cursor.IsZero() {
				ordered := query.OrderedField[store.File, model.ID[store.File]]{ScalarField: f.ID}
				q = q.Where(ordered.Gt(options.Cursor.after))
			}
			var err error
			ids, err = query.SelectValue(q.OrderBy(f.ID.Asc()).Limit(options.Limit+1), f.ID.Value()).All(ctx, tx)
			return err
		})
		if err != nil {
			return err
		}
		if len(ids) > options.Limit {
			ids = ids[:options.Limit]
			page.Next = RegenerationCursor{owner: string(owner), collection: string(collection), after: ids[len(ids)-1]}
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := m.generate(ctx, id, !options.Missing); err != nil {
				page.Failed = append(page.Failed, operationID(id))
				continue
			}
			page.Regenerated = append(page.Regenerated, operationID(id))
		}
		return nil
	})
	if err != nil {
		return RegenerationPage{}, err
	}
	return page, nil
}

// withVariants reloads the ready variants of one just-published attachment.
func (c Collection[M, K]) withVariants(ctx context.Context, m *Manager, file Attachment[M, K]) (Attachment[M, K], error) {
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		files := map[string][]Attachment[M, K]{"": {file}}
		if err := c.loadVariants(ctx, tx, m, files); err != nil {
			return err
		}
		file = files[""][0]
		return nil
	})
	return file, err
}
