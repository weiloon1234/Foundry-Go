package attachments

import (
	"context"
	"crypto/sha256"
	"hash"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

const (
	MaxBatchFiles           = 4096
	MaxBatchPropertiesBytes = 4 << 20
)

// errBatchLimit reports a batch beyond MaxBatchFiles or MaxBatchPropertiesBytes;
// slot loading retries such a batch in smaller parts.
var errBatchLimit = fault.New(fault.Conflict, "attachment batch exceeds its row or property limit")

type Batch[M any, K comparable] struct {
	collection Collection[M, K]
	active     map[string]bool
	files      map[string][]Attachment[M, K]
	// subjects are the subject keys of the loaded owners, in load order.
	subjects []string
}

func (b Batch[M, K]) Get(owner model.Reference[M, K]) ([]Attachment[M, K], error) {
	if b.active == nil || b.files == nil {
		return nil, invalid()
	}
	subject, err := b.collection.definition.owner.SubjectKey(owner)
	if err != nil {
		return nil, err
	}
	return b.get(subject)
}

// getAt returns the files of the i-th loaded owner with the subject key
// derived while loading.
func (b Batch[M, K]) getAt(i int) ([]Attachment[M, K], error) {
	if b.active == nil || b.files == nil || i < 0 || i >= len(b.subjects) {
		return nil, invalid()
	}
	return b.get(b.subjects[i])
}
func (b Batch[M, K]) get(subject string) ([]Attachment[M, K], error) {
	if !b.active[subject] {
		return nil, database.NotFound
	}
	return slices.Clone(b.files[subject]), nil
}
func (c Collection[M, K]) Load(ctx context.Context, m *Manager, owners []model.Reference[M, K]) (Batch[M, K], error) {
	if err := c.check(m); err != nil {
		return Batch[M, K]{}, err
	}
	var result Batch[M, K]
	err := m.reads.Run(ctx, "attachment batch", func(ctx context.Context) error {
		var err error
		result, err = c.loadRows(ctx, m, owners, value.Optional[ID[M]]{})
		return err
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
}
func (c Collection[M, K]) loadRows(ctx context.Context, m *Manager, owners []model.Reference[M, K], only value.Optional[ID[M]]) (Batch[M, K], error) {
	var result Batch[M, K]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		result, err = c.scanRows(ctx, tx, m, owners, []string{locale}, only)
		return err
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
}

// scanRows is shared by exact-locale and all-locale reads. Owner visibility and
// attachment rows use the same repeatable-read transaction and bounded budget.
func (c Collection[M, K]) scanRows(ctx context.Context, tx *database.Tx, m *Manager, owners []model.Reference[M, K], locales []string, only value.Optional[ID[M]]) (Batch[M, K], error) {
	if id, ok := only.Get(); ok && id.IsZero() {
		return Batch[M, K]{}, invalid()
	}
	active, subjects, err := c.definition.owner.ActiveSubjects(ctx, tx, m.store.Registry(), owners)
	if err != nil {
		return Batch[M, K]{}, err
	}
	result := Batch[M, K]{collection: c, active: active, files: make(map[string][]Attachment[M, K], len(active)), subjects: subjects}
	if len(active) == 0 {
		return result, nil
	}
	keys := make([]string, 0, len(active))
	for key := range active {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	f := store.FileFields()
	q := store.QueryFoundryAttachments().Where(f.Scope.Eq(c.definition.owner.Scope()), f.SubjectKey.In(keys...), f.Collection.Eq(string(c.Name())), f.Locale.In(locales...), f.State.Eq(string(Ready)))
	if id, ok := only.Get(); ok {
		q = q.Where(f.ID.Eq(model.IDFromBytes[store.File](id.Bytes())))
	}
	count, bytes := 0, 0
	counts := make(map[string]map[string]int, len(active))
	err = q.OrderBy(f.SubjectKey.Asc(), f.Locale.Asc(), f.SortOrder.Asc(), f.ID.Asc()).Limit(MaxBatchFiles+1).Each(ctx, tx, func(row store.File) error {
		count++
		properties, err := row.Properties.Text()
		if err != nil {
			return err
		}
		bytes += len(properties)
		if count > MaxBatchFiles || bytes > MaxBatchPropertiesBytes {
			return errBatchLimit
		}
		if !active[row.SubjectKey] {
			return invalid()
		}
		selected := c
		if c.definition.policy.Localized {
			selected = c.ForLocale(i18n.LocaleID(row.Locale))
		}
		file, err := selected.attachment(m, row)
		if err != nil {
			return err
		}
		if counts[row.SubjectKey] == nil {
			counts[row.SubjectKey] = make(map[string]int)
		}
		counts[row.SubjectKey][row.Locale]++
		if counts[row.SubjectKey][row.Locale] > c.definition.policy.MaxFiles {
			return fault.New(fault.Conflict, "stored attachment cardinality differs from its collection")
		}
		result.files[row.SubjectKey] = append(result.files[row.SubjectKey], file)
		return nil
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	if err := c.loadVariants(ctx, tx, m, result.files); err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
}
func (c Collection[M, K]) List(ctx context.Context, m *Manager, owner model.Reference[M, K]) ([]Attachment[M, K], error) {
	batch, err := c.Load(ctx, m, []model.Reference[M, K]{owner})
	if err != nil {
		return nil, err
	}
	return batch.Get(owner)
}
func (c Collection[M, K]) First(ctx context.Context, m *Manager, owner model.Reference[M, K]) (value.Optional[Attachment[M, K]], error) {
	files, err := c.List(ctx, m, owner)
	if err != nil {
		return value.Optional[Attachment[M, K]]{}, err
	}
	if len(files) == 0 {
		return value.Optional[Attachment[M, K]]{}, nil
	}
	return value.Set(files[0]), nil
}
func (c Collection[M, K]) find(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (Attachment[M, K], error) {
	batch, err := c.loadRows(ctx, m, []model.Reference[M, K]{owner}, value.Set(id))
	if err != nil {
		return Attachment[M, K]{}, err
	}
	files, err := batch.Get(owner)
	if err != nil {
		return Attachment[M, K]{}, err
	}
	if len(files) != 1 {
		return Attachment[M, K]{}, database.NotFound
	}
	return files[0], nil
}
func (c Collection[M, K]) Find(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (Attachment[M, K], error) {
	if err := c.check(m); err != nil {
		return Attachment[M, K]{}, err
	}
	var result Attachment[M, K]
	err := m.reads.Run(ctx, "attachment lookup", func(ctx context.Context) error { var err error; result, err = c.find(ctx, m, owner, id); return err })
	if err != nil {
		return Attachment[M, K]{}, err
	}
	return result, nil
}

func readBytes[M any, K comparable](ctx context.Context, m *Manager, file Attachment[M, K], maximum int64) ([]byte, error) {
	if maximum < 1 || maximum > MaxUploadBytes || file.info.Size > maximum {
		return nil, storage.Failure(storage.LimitExceeded, storage.OpenOperation, storage.NotApplicable, nil)
	}
	disk, err := m.disks.Disk(file.disk)
	if err != nil {
		return nil, err
	}
	data, info, err := disk.ReadBytes(ctx, file.key, maximum, storage.ReadOptions{IfMatch: file.etag, Version: file.version})
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != file.info.Size {
		return nil, storage.Failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	if !diskVerified(info, file.digest) && storage.SHA256(sha256.Sum256(data)) != file.digest {
		return nil, storage.Failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	return data, nil
}

// diskVerified reports a complete read whose stored full-object checksum
// equals the attachment pin. The disk reader verified those bytes at EOF, so
// hashing them again is unnecessary.
func diskVerified(info storage.ReadInfo, digest storage.SHA256) bool {
	checksum, ok := info.Object.Checksum.Get()
	return ok && checksum == digest && info.Offset == 0 && info.Length == info.Object.Size
}
func (c Collection[M, K]) ReadBytes(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M], maximum int64) ([]byte, error) {
	if err := c.check(m); err != nil {
		return nil, err
	}
	if maximum < 1 || maximum > MaxUploadBytes {
		return nil, storage.Failure(storage.LimitExceeded, storage.OpenOperation, storage.NotApplicable, nil)
	}
	var result []byte
	err := m.reads.Run(ctx, "attachment read", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		result, err = readBytes(ctx, m, file, maximum)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (c Collection[M, K]) Image(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M], plan imaging.Plan) (imaging.Result, error) {
	if err := c.check(m); err != nil {
		return imaging.Result{}, err
	}
	if err := m.image.Validate(); err != nil {
		return imaging.Result{}, err
	}
	if err := plan.Validate(); err != nil {
		return imaging.Result{}, err
	}
	var result imaging.Result
	err := m.reads.Run(ctx, "attachment image", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		data, err := readBytes(ctx, m, file, min(MaxUploadBytes, m.image.Limits().InputBytes))
		if err != nil {
			return err
		}
		result, err = m.image.ProcessBytes(ctx, data, plan)
		return err
	})
	if err != nil {
		return imaging.Result{}, err
	}
	return result, nil
}

// PublicURL rechecks ready membership, then derives the public URL of the
// pinned object. Keys are immutable per upload, so no storage request is made.
func (c Collection[M, K]) PublicURL(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (string, error) {
	if err := c.check(m); err != nil {
		return "", err
	}
	var result string
	err := m.reads.Run(ctx, "attachment public URL", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		result, err = c.publicURL(ctx, m, file)
		return err
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// TemporaryURL rechecks ready membership, then signs a read link pinned to the
// stored version. Signing performs no storage request.
func (c Collection[M, K]) TemporaryURL(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M], expires time.Duration) (storage.TemporaryURL, error) {
	if err := c.check(m); err != nil {
		return storage.TemporaryURL{}, err
	}
	if err := (storage.LinkOptions{ExpiresIn: expires}).Validate(); err != nil {
		return storage.TemporaryURL{}, err
	}
	var result storage.TemporaryURL
	err := m.reads.Run(ctx, "attachment temporary URL", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		result, err = c.temporaryURL(ctx, m, file, storage.LinkOptions{ExpiresIn: expires})
		return err
	})
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	return result, nil
}

// PublicURLOf derives the public URL of an attachment already loaded through
// this collection (for example from Load or List) without database or storage
// I/O. Authorize access to the attachment first; the URL does not.
func (c Collection[M, K]) PublicURLOf(ctx context.Context, m *Manager, file Attachment[M, K]) (string, error) {
	if err := c.owns(m, file); err != nil {
		return "", err
	}
	return c.publicURL(ctx, m, file)
}

// PublicURLsOf maps loaded attachments to public URLs in order, for listing
// responses. It performs no database or storage I/O.
func (c Collection[M, K]) PublicURLsOf(ctx context.Context, m *Manager, files []Attachment[M, K]) ([]string, error) {
	if len(files) > MaxBatchFiles {
		return nil, invalid()
	}
	urls := make([]string, len(files))
	for i, file := range files {
		var err error
		if urls[i], err = c.PublicURLOf(ctx, m, file); err != nil {
			return nil, err
		}
	}
	return urls, nil
}

// TemporaryURLOf signs a read link for a loaded attachment, pinned to its
// stored version, without database or storage I/O. options.Version must be
// zero; response overrides (for example a download Content-Disposition using
// the original name) are signed into the link.
func (c Collection[M, K]) TemporaryURLOf(ctx context.Context, m *Manager, file Attachment[M, K], options storage.LinkOptions) (storage.TemporaryURL, error) {
	if err := c.owns(m, file); err != nil {
		return storage.TemporaryURL{}, err
	}
	if options.Version != "" {
		return storage.TemporaryURL{}, invalid()
	}
	return c.temporaryURL(ctx, m, file, options)
}
func (c Collection[M, K]) publicURL(ctx context.Context, m *Manager, file Attachment[M, K]) (string, error) {
	// A stable public URL renders inline; script-capable files would run in
	// the serving origin (stored XSS) unless the policy explicitly allows it.
	if activeContent(file.info.MediaType) && !c.definition.policy.InlineActiveContent {
		return "", ActiveContentRefused
	}
	disk, err := m.disks.Disk(file.disk)
	if err != nil {
		return "", err
	}
	return disk.PublicURL(ctx, file.key)
}
func (c Collection[M, K]) temporaryURL(ctx context.Context, m *Manager, file Attachment[M, K], options storage.LinkOptions) (storage.TemporaryURL, error) {
	if activeContent(file.info.MediaType) && !c.definition.policy.InlineActiveContent {
		// Signed links to script-capable files always download.
		switch {
		case options.ResponseContentDisposition == "":
			options.ResponseContentDisposition = "attachment"
		case !strings.HasPrefix(strings.ToLower(options.ResponseContentDisposition), "attachment"):
			return storage.TemporaryURL{}, ActiveContentRefused
		}
	}
	disk, err := m.disks.Disk(file.disk)
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	options.Version = file.version
	return disk.TemporaryURL(ctx, file.key, options)
}

// ActiveContentRefused reports an inline link to a script-capable attachment
// (SVG, HTML, XML, JavaScript) whose policy does not set InlineActiveContent.
// It matches fault.Invalid.
var ActiveContentRefused = fault.New(fault.Invalid, "script-capable attachment cannot be linked inline")

// activeContent reports media browsers can execute scripts from when a
// response renders inline: SVG and other XML documents, HTML and JavaScript.
func activeContent(media storage.MediaType) bool {
	text := strings.ToLower(string(media))
	switch text {
	case "text/html", "application/xhtml+xml", "text/xml", "application/xml", "text/javascript", "application/javascript", "application/ecmascript", "text/ecmascript":
		return true
	}
	return strings.HasSuffix(text, "+xml")
}

// owns checks that a loaded attachment belongs to this registered collection,
// locale and disk. It never trusts a value from another collection.
func (c Collection[M, K]) owns(m *Manager, file Attachment[M, K]) error {
	if err := c.check(m); err != nil {
		return err
	}
	if file.IsZero() || file.collection != c.Name() || file.disk != c.definition.policy.Disk.ID() || file.locale != c.locale || file.key.IsZero() || file.etag == "" {
		return invalid()
	}
	return nil
}

// Open streams an attachment pinned to its stored ETag/version. The full
// SHA-256 and length are verified at EOF (by the disk when the stored checksum
// matches the pin, otherwise here). Only the lookup uses manager read
// admission; the returned reader holds one disk stream slot until Close.
// Always close it, and close it before closing the disk.
func (c Collection[M, K]) Open(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (io.ReadCloser, Attachment[M, K], error) {
	if err := c.check(m); err != nil {
		return nil, Attachment[M, K]{}, err
	}
	var file Attachment[M, K]
	err := m.reads.Run(ctx, "attachment stream lookup", func(ctx context.Context) error { var err error; file, err = c.find(ctx, m, owner, id); return err })
	if err != nil {
		return nil, Attachment[M, K]{}, err
	}
	disk, err := m.disks.Disk(file.disk)
	if err != nil {
		return nil, Attachment[M, K]{}, err
	}
	body, info, err := disk.Open(ctx, file.key, storage.ReadOptions{IfMatch: file.etag, Version: file.version})
	if err != nil {
		return nil, Attachment[M, K]{}, err
	}
	if info.Length != file.info.Size || info.Offset != 0 {
		cleanup := body.Close()
		return nil, Attachment[M, K]{}, storage.Failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, cleanup)
	}
	if diskVerified(info, file.digest) {
		return body, file, nil
	}
	return &verifiedReader{body: body, digest: sha256.New(), expected: file.digest, remaining: file.info.Size}, file, nil
}

// verifiedReader checks the pinned digest and exact length at EOF when the
// disk has no matching stored checksum to verify.
type verifiedReader struct {
	body      io.ReadCloser
	digest    hash.Hash
	expected  storage.SHA256
	remaining int64
}

func (r *verifiedReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		_, _ = r.digest.Write(p[:n])
		r.remaining -= int64(n)
	}
	if err == io.EOF {
		var actual storage.SHA256
		copy(actual[:], r.digest.Sum(nil))
		if r.remaining != 0 || actual != r.expected {
			return n, storage.Failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, nil)
		}
	}
	return n, err
}
func (r *verifiedReader) Close() error { return r.body.Close() }
