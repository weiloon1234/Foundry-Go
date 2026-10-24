package attachments

import (
	"context"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Private callbacks retain shared slot operations without exposing their generic descriptor graph.
// Argument/result pointers are synchronous and never retained by these callbacks.
type attachmentHandleCore struct {
	bind         func(*Manager) *attachmentHandleCore
	relation     func(unsafe.Pointer)
	validate     func() error
	describe     func() extensions.SlotDescription
	registration func() Registration
	name         func() Name
	replace      func(ctx context.Context, owner unsafe.Pointer, upload Upload, out unsafe.Pointer) error
	replaceFile  func(ctx context.Context, owner unsafe.Pointer, file FileSource, out unsafe.Pointer) error
	prepare      func(ctx context.Context, owner unsafe.Pointer, upload Upload, out unsafe.Pointer) error
	prepareFile  func(ctx context.Context, owner unsafe.Pointer, file FileSource, out unsafe.Pointer) error
	replaceIn    func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, prepared unsafe.Pointer, out unsafe.Pointer) error
	discard      func(ctx context.Context, prepared unsafe.Pointer) error
	detach       func(ctx context.Context, owner unsafe.Pointer, id unsafe.Pointer) (ChangeResult, error)
	clear        func(ctx context.Context, owner unsafe.Pointer) (ChangeResult, error)
	accepts      func(ctx context.Context, file FileSource) (bool, error)
	url          func(ctx context.Context, owner unsafe.Pointer, file unsafe.Pointer) (string, error)
	temporaryURL func(ctx context.Context, owner unsafe.Pointer, file unsafe.Pointer, options storage.LinkOptions) (storage.TemporaryURL, error)
	variantURL   func(ctx context.Context, owner unsafe.Pointer, file unsafe.Pointer, variant Variant) (string, error)
	publicURL    func(ctx context.Context, owner unsafe.Pointer) (value.Optional[string], error)
	add          func(ctx context.Context, owner unsafe.Pointer, upload Upload, out unsafe.Pointer) error
	addFile      func(ctx context.Context, owner unsafe.Pointer, file FileSource, out unsafe.Pointer) error
	addIn        func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, prepared unsafe.Pointer, out unsafe.Pointer) error
	reorder      func(ctx context.Context, owner unsafe.Pointer, order unsafe.Pointer, out unsafe.Pointer) error
	publicURLs   func(ctx context.Context, owner unsafe.Pointer) ([]string, error)
}
