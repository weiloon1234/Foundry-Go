package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	fixtureconfig "foundry.test/deployacceptance/internal/config"
	"foundry.test/deployacceptance/internal/work"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

type uploadOwner struct {
	Name string
}

var uploadOwners = orm.Define("acceptance_fixture", func(b orm.Builder[uploadOwner], m *uploadOwner) orm.Table[uploadOwner, string] {
	return orm.TableOf(b, &m.Name)
})
var uploadIdentity = extensions.DefineORMOwner("acceptance-uploads", orm.IdentityOf(uploadOwners, uploadOwners.Key()), extensions.OwnerOptions{})
var uploadDisk = storage.DefineDisk("acceptance-uploads")
var largeUploadPolicy = attachments.Policy{Disk: uploadDisk, Cardinality: attachments.Single, AnyMedia: true, MaxBytes: 512 << 20, MaxStoredBytes: 512 << 20}
var largeFiles = attachments.Define(uploadIdentity, "large", largeUploadPolicy)
var smallUploadDisk = storage.DefineDisk("acceptance-small-uploads")
var smallFiles = attachments.Define(uploadIdentity, "small", attachments.Policy{Disk: smallUploadDisk, Cardinality: attachments.Single, AnyMedia: true, MaxBytes: 1 << 20, MaxStoredBytes: 1 << 20})

type uploadQuery struct{ ID, Hold string }
type uploadBody struct{ File http.UploadedFile }

//foundry:dto
type UploadReceipt struct {
	Operation string `json:"operation"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}

//foundry:dto
type UploadUsage struct {
	Active       int   `json:"active"`
	Reserved     int64 `json:"reserved"`
	Publications int64 `json:"publications"`
	Publishing   int64 `json:"publishing"`
}

type uploadControlKey struct{}
type uploadControl struct{ id, hold string }

// The barrier is inside the real adapter call. It never sends response bytes.
// Only this disposable consumer understands the controller's exact marker IDs.
type delayedUploadBackend struct {
	storage.Backend
	receipts                    *work.Receipts
	attempts, active, published atomic.Int64
}

func (b *delayedUploadBackend) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	control, _ := ctx.Value(uploadControlKey{}).(uploadControl)
	attempt := b.attempts.Add(1)
	b.active.Add(1)
	defer b.active.Add(-1)
	if err := b.receipts.Mark(fmt.Sprintf("%s.put-%d", control.id, attempt)); err != nil {
		return storage.ObjectInfo{}, err
	}
	wait := func() error {
		if err := b.receipts.Mark(control.id + ".held"); err != nil {
			return err
		}
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			released, err := b.receipts.Released(control.id + ".release")
			if err != nil || released {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
	if control.hold == "publication" {
		if err := wait(); err != nil {
			return storage.ObjectInfo{}, err
		}
	}
	if control.hold == "staging" {
		source = &holdUploadEOF{Reader: source, wait: wait}
	}
	info, err := b.Backend.Put(ctx, key, source, options)
	if err == nil {
		b.published.Add(1)
	}
	return info, err
}

type holdUploadEOF struct {
	io.Reader
	wait func() error
}

func (r *holdUploadEOF) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.wait != nil {
		wait := r.wait
		r.wait = nil
		if failed := wait(); failed != nil {
			return n, failed
		}
	}
	return n, err
}

type uploadRuntime struct {
	manager      *attachments.Manager
	store        *extensions.Store
	pool         *http.UploadPool
	backend      *delayedUploadBackend
	smallBackend *delayedUploadBackend
	settings     http.UploadSettings
	receipts     *work.Receipts
}

// Only serve with the explicit deployed upload profile enters this path. Normal
// config inspection, worker and scheduler assembly perform no upload-root I/O.
func serveUploads(ctx context.Context, settings fixtureconfig.Settings, receipts *work.Receipts) (result error) {
	if err := settings.App.HTTP.Uploads.Validate(); err != nil {
		return err
	}
	pool, err := http.NewUploadPool(settings.App.HTTP.Uploads.Request)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, pool.Close()) }()
	// The adapter owns staging admission and atomic publication in this root.
	localConfig := local.DefaultConfig(settings.App.HTTP.Uploads.Staging.Directory)
	localConfig.MaxObjectBytes = largeUploadPolicy.MaxStoredBytes
	localConfig.Staging = local.StagingLimits{MaxActive: settings.App.HTTP.Uploads.Staging.MaxActive, MaxBytes: settings.App.HTTP.Uploads.Staging.MaxBytes, SmallObjectBytes: attachments.MaxBufferedBytes}
	backend, err := local.Open(ctx, localConfig)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, backend.Close()) }()
	delayed := &delayedUploadBackend{Backend: backend, receipts: receipts}
	diskConfig := storage.DefaultConfig()
	diskConfig.MaxObjectBytes = localConfig.MaxObjectBytes
	diskConfig.Timeout = settings.App.HTTP.Uploads.StorageTimeout
	disk, err := uploadDisk.Bind(delayed, diskConfig)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, disk.Close(context.Background())) }()
	// A full large volume cannot consume the separately provisioned small volume.
	smallRoot := filepath.Join(settings.App.HTTP.Uploads.SmallDirectory, "objects")
	if err := os.MkdirAll(smallRoot, 0700); err != nil {
		return err
	}
	smallLocal, err := local.Open(ctx, local.DefaultConfig(smallRoot))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, smallLocal.Close()) }()
	smallDelayed := &delayedUploadBackend{Backend: smallLocal, receipts: receipts}
	smallDisk, err := smallUploadDisk.Bind(smallDelayed, storage.DefaultConfig())
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, smallDisk.Close(context.Background())) }()
	disks, err := storage.NewRegistry(disk, smallDisk)
	if err != nil {
		return err
	}
	runtime := &uploadRuntime{pool: pool, backend: delayed, smallBackend: smallDelayed, settings: settings.App.HTTP.Uploads, receipts: receipts}
	built, err := Builder(settings, receipts).HTTP(runtime.routes).Build(ctx)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, shutdown(built)) }()
	if err := built.Start(ctx); err != nil {
		return err
	}
	runtime.store, err = application.Resolve(built.Resources(), application.ExtensionKey)
	if err != nil {
		return err
	}
	attachmentConfig := settings.App.Features.Attachments.Config
	attachmentConfig.MaxLargeActive = settings.App.HTTP.Uploads.AttachmentMaxActive
	runtime.manager, err = attachments.New(attachments.Dependencies{Store: runtime.store, Disks: disks}, attachmentConfig, largeFiles.Registration(), smallFiles.Registration())
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, runtime.manager.Close(context.Background())) }()
	return built.Run(ctx, foundation.HTTP)
}

func (u *uploadRuntime) routes(application.Services) ([]http.RouteRegistration, error) {
	query := http.DefineQuery(http.QueryParam("id", http.StringQuery[string](), func(q *uploadQuery) *string { return &q.ID }), http.DefaultQueryParam("hold", http.StringQuery[string](), "", func(q *uploadQuery) *string { return &q.Hold }))
	var routes []http.RouteRegistration
	fileForm := http.DefineMultipart(http.FilePart("file", func(b *uploadBody) *http.UploadedFile { return &b.File }))
	smallLimits := http.DefaultEndpointLimits()
	smallLimits.Multipart.Bytes, smallLimits.Multipart.FileBytes = 2<<20, 1<<20
	smallLimits.Multipart.Files, smallLimits.Multipart.Parts, smallLimits.Multipart.Readers = 1, 1, 1
	multipart := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "acceptance.upload.multipart", Method: http.POST, Access: http.Public}, http.StaticPath("/acceptance/uploads/multipart"), http.BodyLimit(2<<20)), query, http.MultipartBody(http.MultipartDirectory(fileForm, u.settings.SmallDirectory)), http.EmptyResponse(204), http.Limits(smallLimits))
	routes = append(routes, multipart.Handle(func(_ context.Context, in http.Input[http.NoPath, uploadQuery, uploadBody]) (http.NoContent, error) {
		if err := work.ValidateID(in.Query.ID); err != nil {
			return http.NoContent{}, http.BadRequest
		}
		return http.NoContent{}, nil
	}))
	for _, kind := range []string{"large", "short", "small"} {
		form := fileForm
		bodyBytes, fileBytes, timeout := u.settings.BodyBytes, u.settings.FileBytes, u.settings.Timeout
		collection := largeFiles
		if kind == "small" {
			form = http.MultipartDirectory(form, u.settings.SmallDirectory)
			bodyBytes, fileBytes = 2<<20, 1<<20
			collection = smallFiles
		} else {
			form = http.MultipartAdmission(form, u.pool)
		}
		if kind == "short" {
			timeout = 6 * time.Second
		}
		limits := http.DefaultEndpointLimits()
		limits.Multipart.Bytes = bodyBytes
		limits.Multipart.FileBytes = fileBytes
		limits.Multipart.Parts, limits.Multipart.Files, limits.Multipart.Readers = 1, 1, 1
		endpoint := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "acceptance.upload." + http.RouteID(kind), Method: http.POST, Access: http.Public}, http.StaticPath("/acceptance/uploads/"+kind), http.BodyLimit(bodyBytes), http.Timeout(timeout)), query, http.MultipartBody(form), http.JSONResponse(200, UploadReceiptJSON()), http.Limits(limits))
		routes = append(routes, endpoint.Handle(func(ctx context.Context, in http.Input[http.NoPath, uploadQuery, uploadBody]) (UploadReceipt, error) {
			if err := work.ValidateID(in.Query.ID); err != nil {
				return UploadReceipt{}, http.BadRequest
			}
			if in.Query.Hold != "" && in.Query.Hold != "publication" && in.Query.Hold != "staging" {
				return UploadReceipt{}, http.BadRequest
			}
			if err := u.receipts.Mark(in.Query.ID + ".captured"); err != nil {
				return UploadReceipt{}, err
			}
			ctx = context.WithValue(ctx, uploadControlKey{}, uploadControl{in.Query.ID, in.Query.Hold})
			owner := extensions.ORMIdentity(uploadIdentity).Reference(in.Query.ID)
			if err := u.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
				_, err := tx.Exec(ctx, `INSERT INTO acceptance_fixture (name,value) VALUES ($1,'upload')`, in.Query.ID)
				return err
			}); err != nil {
				return UploadReceipt{}, err
			}
			reader, err := in.Body.File.Open(ctx)
			if err != nil {
				return UploadReceipt{}, err
			}
			defer reader.Close()
			prepared, err := collection.Prepare(ctx, u.manager, owner, attachments.Upload{Source: reader, OriginalName: "payload.txt"})
			if err != nil {
				return UploadReceipt{}, err
			}
			var published attachments.Result[uploadOwner, string]
			err = u.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
				var err error
				published, err = collection.ReplaceIn(ctx, tx, u.manager, owner, prepared)
				return err
			})
			if err != nil {
				return UploadReceipt{}, err
			}
			file, ok := published.Attachment.Get()
			if !ok {
				return UploadReceipt{}, http.Unavailable
			}
			stored, _, err := collection.Open(ctx, u.manager, owner, file.ID())
			if err != nil {
				return UploadReceipt{}, err
			}
			hash := sha256.New()
			n, readErr := io.Copy(hash, stored)
			if err := errors.Join(readErr, stored.Close()); err != nil {
				return UploadReceipt{}, err
			}
			if err := u.receipts.Mark(in.Query.ID + ".committed"); err != nil {
				return UploadReceipt{}, err
			}
			return UploadReceipt{Operation: published.Operation.String(), Bytes: n, SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
		}))
	}
	usage := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "acceptance.upload.usage", Method: http.GET, Access: http.Public}, http.StaticPath("/acceptance/uploads/usage")), http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, UploadUsageJSON()))
	routes = append(routes, usage.Handle(func(context.Context, http.Input[http.NoPath, http.NoQuery, http.NoBody]) (UploadUsage, error) {
		active, reserved := u.pool.Usage()
		return UploadUsage{Active: active, Reserved: reserved, Publications: u.backend.published.Load() + u.smallBackend.published.Load(), Publishing: u.backend.active.Load() + u.smallBackend.active.Load()}, nil
	}))
	cleanup := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "acceptance.upload.cleanup", Method: http.POST, Access: http.Public}, http.StaticPath("/acceptance/uploads/cleanup")), query, http.EmptyBody(), http.EmptyResponse(204))
	routes = append(routes, cleanup.Handle(func(ctx context.Context, in http.Input[http.NoPath, uploadQuery, http.NoBody]) (http.NoContent, error) {
		if err := work.ValidateID(in.Query.ID); err != nil {
			return http.NoContent{}, http.BadRequest
		}
		owner := extensions.ORMIdentity(uploadIdentity).Reference(in.Query.ID)
		for _, collection := range []attachments.Collection[uploadOwner, string]{largeFiles, smallFiles} {
			files, err := collection.List(ctx, u.manager, owner)
			if err != nil {
				return http.NoContent{}, err
			}
			for _, file := range files {
				if _, err := collection.Detach(ctx, u.manager, owner, file.ID()); err != nil {
					return http.NoContent{}, err
				}
			}
		}
		return http.NoContent{}, nil
	}))
	return routes, nil
}
