package attachments

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensionslot"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// ManyHandle retains typed slot capabilities behind shared callback state.
// Example: slot.From(manager).Relation(Fields.Slot).
type ManyHandle[M any, K comparable] struct {
	_    [0]*M
	_    [0]*K
	_    [0]manyHandleKind
	core *attachmentHandleCore
}

type manyHandleKind struct{}

// ManyOf declares a thin multiple-file handle with the existing manager policy.
// Example: attachments.ManyOf(owner,"files",policy,binding).
func ManyOf[M any, K comparable](owner extensions.Owner[M, K], name Name, policy Policy, binding extensions.SlotBinding[M, K, Many[M]]) ManyHandle[M, K] {
	return ManyHandle[M, K]{core: wrapManyHandle(defineMany(owner, name, policy, binding))}
}

func wrapManyHandle[M any, K comparable](state manySlot[M, K]) *attachmentHandleCore {
	return &attachmentHandleCore{
		collection:   func(out unsafe.Pointer) { *(*Collection[M, K])(out) = state.collection },
		checkFiles:   func(count int) error { return checkAddFiles(state.slotDefinition, count) },
		bind:         func(m *Manager) *attachmentHandleCore { return wrapManyHandle(state.From(m)) },
		relation:     func(out unsafe.Pointer) { *(*extensionslot.Slot[M])(out) = state.loader },
		validate:     state.Validate,
		describe:     state.Describe,
		registration: state.Registration,
		name:         state.Name,
		replace: func(ctx context.Context, owner unsafe.Pointer, upload Upload, out unsafe.Pointer) error {
			result, err := state.Replace(ctx, *(*M)(owner), upload)
			*(*Result[M, K])(out) = result
			return err
		},
		replaceFile: func(ctx context.Context, owner unsafe.Pointer, file FileSource, out unsafe.Pointer) error {
			result, err := state.ReplaceFile(ctx, *(*M)(owner), file)
			*(*Result[M, K])(out) = result
			return err
		},
		prepare: func(ctx context.Context, owner unsafe.Pointer, upload Upload, out unsafe.Pointer) error {
			result, err := state.Prepare(ctx, *(*M)(owner), upload)
			*(*Prepared[M, K])(out) = result
			return err
		},
		prepareFile: func(ctx context.Context, owner unsafe.Pointer, file FileSource, out unsafe.Pointer) error {
			result, err := state.PrepareFile(ctx, *(*M)(owner), file)
			*(*Prepared[M, K])(out) = result
			return err
		},
		replaceIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, prepared unsafe.Pointer, out unsafe.Pointer) error {
			result, err := state.ReplaceIn(ctx, tx, *(*M)(owner), *(*Prepared[M, K])(prepared))
			*(*Result[M, K])(out) = result
			return err
		},
		discard: func(ctx context.Context, prepared unsafe.Pointer) error {
			return state.Discard(ctx, *(*Prepared[M, K])(prepared))
		},
		detach: func(ctx context.Context, owner unsafe.Pointer, id unsafe.Pointer) (ChangeResult, error) {
			return state.Detach(ctx, *(*M)(owner), *(*ID[M])(id))
		},
		clear: func(ctx context.Context, owner unsafe.Pointer) (ChangeResult, error) {
			return state.Clear(ctx, *(*M)(owner))
		},
		accepts: func(ctx context.Context, file FileSource) (bool, error) { return state.Accepts(ctx, file) },
		url: func(ctx context.Context, owner unsafe.Pointer, file unsafe.Pointer) (string, error) {
			return state.URL(ctx, *(*M)(owner), *(*File[M])(file))
		},
		temporaryURL: func(ctx context.Context, owner unsafe.Pointer, file unsafe.Pointer, options storage.LinkOptions) (storage.TemporaryURL, error) {
			return state.TemporaryURL(ctx, *(*M)(owner), *(*File[M])(file), options)
		},
		variantURL: func(ctx context.Context, owner unsafe.Pointer, file unsafe.Pointer, variant Variant) (string, error) {
			return state.VariantURL(ctx, *(*M)(owner), *(*File[M])(file), variant)
		},
		add: func(ctx context.Context, owner unsafe.Pointer, upload Upload, out unsafe.Pointer) error {
			result, err := state.Add(ctx, *(*M)(owner), upload)
			*(*Result[M, K])(out) = result
			return err
		},
		addFile: func(ctx context.Context, owner unsafe.Pointer, file FileSource, out unsafe.Pointer) error {
			result, err := state.AddFile(ctx, *(*M)(owner), file)
			*(*Result[M, K])(out) = result
			return err
		},
		addIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, prepared unsafe.Pointer, out unsafe.Pointer) error {
			result, err := state.AddIn(ctx, tx, *(*M)(owner), *(*Prepared[M, K])(prepared))
			*(*Result[M, K])(out) = result
			return err
		},
		reorder: func(ctx context.Context, owner unsafe.Pointer, order unsafe.Pointer, out unsafe.Pointer) error {
			result, err := state.Reorder(ctx, *(*M)(owner), *(*[]ID[M])(order))
			*(*[]Attachment[M, K])(out) = result
			return err
		},
		publicURLs: func(ctx context.Context, owner unsafe.Pointer) ([]string, error) {
			return state.PublicURLs(ctx, *(*M)(owner))
		},
	}
}

// From returns a copy borrowing this family's manager; nil leaves it unbound.
// Example: slot.From(manager).
func (s ManyHandle[M, K]) From(m *Manager) ManyHandle[M, K] {
	if s.core != nil {
		s.core = s.core.bind(m)
	}
	return s
}

// Relation binds the loader to its concrete ORM field; mismatched fields fail validation.
// Example: Articles.Query().With(slot.Relation(Articles.Gallery)).
func (s ManyHandle[M, K]) Relation(slot orm.Slot[M, Many[M]]) orm.ExtensionRelation[M] {
	var source extensionslot.Slot[M]
	if s.core != nil {
		s.core.relation(unsafe.Pointer(&source))
	}
	return orm.FoundryExtension(slot, source, sqlowner.Queries())
}

// Validate checks the declaration and slot binding. Example: slot.Validate().
func (s ManyHandle[M, K]) Validate() error {
	if s.core == nil {
		return invalid()
	}
	return s.core.validate()
}

// Describe returns an owned inspection snapshot. Example: slot.Describe().
func (s ManyHandle[M, K]) Describe() extensions.SlotDescription {
	if s.core == nil {
		return *new(extensions.SlotDescription)
	}
	return s.core.describe()
}

// Registration returns the existing manager registration. Example: slot.Registration().
func (s ManyHandle[M, K]) Registration() Registration {
	if s.core == nil {
		return *new(Registration)
	}
	return s.core.registration()
}

// Name returns the stored name. Example: slot.Name().
func (s ManyHandle[M, K]) Name() Name {
	if s.core == nil {
		return *new(Name)
	}
	return s.core.name()
}

// Replace replaces the collection; inspect the publication outcome when an error is returned.
// Example: slot.Replace(ctx,article,upload).
func (s ManyHandle[M, K]) Replace(ctx context.Context, owner M, upload Upload) (Result[M, K], error) {
	if s.core == nil {
		return *new(Result[M, K]), invalid()
	}
	var result Result[M, K]
	err := s.core.replace(ctx, unsafe.Pointer(&owner), upload, unsafe.Pointer(&result))
	return result, err
}

// ReplaceFile replaces the collection from a file; inspect the publication outcome on error.
// Example: slot.ReplaceFile(ctx,article,file).
func (s ManyHandle[M, K]) ReplaceFile(ctx context.Context, owner M, file FileSource) (Result[M, K], error) {
	if s.core == nil {
		return *new(Result[M, K]), invalid()
	}
	var result Result[M, K]
	err := s.core.replaceFile(ctx, unsafe.Pointer(&owner), file, unsafe.Pointer(&result))
	return result, err
}

// Prepare prepares storage before a transaction without publishing it.
// Example: slot.Prepare(ctx,article,upload).
func (s ManyHandle[M, K]) Prepare(ctx context.Context, owner M, upload Upload) (Prepared[M, K], error) {
	if s.core == nil {
		return *new(Prepared[M, K]), invalid()
	}
	var result Prepared[M, K]
	err := s.core.prepare(ctx, unsafe.Pointer(&owner), upload, unsafe.Pointer(&result))
	return result, err
}

// PrepareFile prepares file storage before a transaction without publishing it.
// Example: slot.PrepareFile(ctx,article,file).
func (s ManyHandle[M, K]) PrepareFile(ctx context.Context, owner M, file FileSource) (Prepared[M, K], error) {
	if s.core == nil {
		return *new(Prepared[M, K]), invalid()
	}
	var result Prepared[M, K]
	err := s.core.prepareFile(ctx, unsafe.Pointer(&owner), file, unsafe.Pointer(&result))
	return result, err
}

// ReplaceIn publishes a prepared upload in the caller transaction.
// Example: slot.ReplaceIn(ctx,tx,article,prepared).
func (s ManyHandle[M, K]) ReplaceIn(ctx context.Context, tx *database.Tx, owner M, prepared Prepared[M, K]) (Result[M, K], error) {
	if s.core == nil {
		return *new(Result[M, K]), invalid()
	}
	var result Result[M, K]
	err := s.core.replaceIn(ctx, tx, unsafe.Pointer(&owner), unsafe.Pointer(&prepared), unsafe.Pointer(&result))
	return result, err
}

// Discard reclaims an unpublished prepared upload.
// Example: slot.Discard(ctx,prepared).
func (s ManyHandle[M, K]) Discard(ctx context.Context, prepared Prepared[M, K]) error {
	if s.core == nil {
		return invalid()
	}
	return s.core.discard(ctx, unsafe.Pointer(&prepared))
}

// Detach retires one file and schedules storage cleanup.
// Example: slot.Detach(ctx,article,id).
func (s ManyHandle[M, K]) Detach(ctx context.Context, owner M, id ID[M]) (ChangeResult, error) {
	if s.core == nil {
		return *new(ChangeResult), invalid()
	}
	return s.core.detach(ctx, unsafe.Pointer(&owner), unsafe.Pointer(&id))
}

// Clear retires every file and schedules storage cleanup.
// Example: slot.Clear(ctx,article).
func (s ManyHandle[M, K]) Clear(ctx context.Context, owner M) (ChangeResult, error) {
	if s.core == nil {
		return *new(ChangeResult), invalid()
	}
	return s.core.clear(ctx, unsafe.Pointer(&owner))
}

// Accepts checks policy using the existing manager admission and media checks.
// Example: slot.Accepts(ctx,file).
func (s ManyHandle[M, K]) Accepts(ctx context.Context, file FileSource) (bool, error) {
	if s.core == nil {
		return *new(bool), invalid()
	}
	return s.core.accepts(ctx, file)
}

// URL derives a loaded file URL without I/O; authorize access first.
// Example: slot.URL(ctx,article,file).
func (s ManyHandle[M, K]) URL(ctx context.Context, owner M, file File[M]) (string, error) {
	if s.core == nil {
		return *new(string), invalid()
	}
	return s.core.url(ctx, unsafe.Pointer(&owner), unsafe.Pointer(&file))
}

// TemporaryURL signs a loaded file URL pinned to its stored version.
// Example: slot.TemporaryURL(ctx,article,file,options).
func (s ManyHandle[M, K]) TemporaryURL(ctx context.Context, owner M, file File[M], options storage.LinkOptions) (storage.TemporaryURL, error) {
	if s.core == nil {
		return *new(storage.TemporaryURL), invalid()
	}
	return s.core.temporaryURL(ctx, unsafe.Pointer(&owner), unsafe.Pointer(&file), options)
}

// VariantURL derives a generated variant URL without I/O.
// Example: slot.VariantURL(ctx,article,file,variant).
func (s ManyHandle[M, K]) VariantURL(ctx context.Context, owner M, file File[M], variant Variant) (string, error) {
	if s.core == nil {
		return *new(string), invalid()
	}
	return s.core.variantURL(ctx, unsafe.Pointer(&owner), unsafe.Pointer(&file), variant)
}

// Add appends an upload to the multiple-file collection.
// Example: slot.Add(ctx,article,upload).
func (s ManyHandle[M, K]) Add(ctx context.Context, owner M, upload Upload) (Result[M, K], error) {
	if s.core == nil {
		return *new(Result[M, K]), invalid()
	}
	var result Result[M, K]
	err := s.core.add(ctx, unsafe.Pointer(&owner), upload, unsafe.Pointer(&result))
	return result, err
}

// AddFile appends a file to the multiple-file collection.
// Example: slot.AddFile(ctx,article,file).
func (s ManyHandle[M, K]) AddFile(ctx context.Context, owner M, file FileSource) (Result[M, K], error) {
	if s.core == nil {
		return *new(Result[M, K]), invalid()
	}
	var result Result[M, K]
	err := s.core.addFile(ctx, unsafe.Pointer(&owner), file, unsafe.Pointer(&result))
	return result, err
}

// AddIn publishes a prepared upload in the caller transaction.
// Example: slot.AddIn(ctx,tx,article,prepared).
func (s ManyHandle[M, K]) AddIn(ctx context.Context, tx *database.Tx, owner M, prepared Prepared[M, K]) (Result[M, K], error) {
	if s.core == nil {
		return *new(Result[M, K]), invalid()
	}
	var result Result[M, K]
	err := s.core.addIn(ctx, tx, unsafe.Pointer(&owner), unsafe.Pointer(&prepared), unsafe.Pointer(&result))
	return result, err
}

// Reorder sets an exact permutation of the collection file IDs.
// Example: slot.Reorder(ctx,article,order).
func (s ManyHandle[M, K]) Reorder(ctx context.Context, owner M, order []ID[M]) ([]Attachment[M, K], error) {
	if s.core == nil {
		return *new([]Attachment[M, K]), invalid()
	}
	var result []Attachment[M, K]
	err := s.core.reorder(ctx, unsafe.Pointer(&owner), unsafe.Pointer(&order), unsafe.Pointer(&result))
	return result, err
}

// PublicURLs derives loaded file URLs in collection order without I/O.
// Example: slot.PublicURLs(ctx,article).
func (s ManyHandle[M, K]) PublicURLs(ctx context.Context, owner M) ([]string, error) {
	if s.core == nil {
		return *new([]string), invalid()
	}
	return s.core.publicURLs(ctx, unsafe.Pointer(&owner))
}

// Format omits retained runtime state. Example: fmt.Sprintf("%+v",slot).
func (ManyHandle[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("attachments slot handle"))
}
