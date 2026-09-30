package attachments

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// One is a model slot holding the ready file of a single-file collection. The
// zero value is not loaded. Reading a slot performs no I/O.
type One[M any] struct {
	file   *File[M]
	loaded bool
}

// Many is a model slot holding the ready files of a multiple-file collection
// in collection order. The zero value is not loaded.
type Many[M any] struct {
	files  []File[M]
	loaded bool
}

// IsLoaded reports whether the slot was filled by a loader.
func (o One[M]) IsLoaded() bool { return o.loaded }

// Get returns the optional file and whether the slot was loaded. A loaded but
// absent file differs from a slot that was not loaded.
func (o One[M]) Get() (value.Optional[File[M]], bool) {
	if o.file == nil {
		return value.Optional[File[M]]{}, o.loaded
	}
	return value.Set(*o.file), o.loaded
}

// IsLoaded reports whether the slot was filled by a loader.
func (m Many[M]) IsLoaded() bool { return m.loaded }

// Get returns a caller-owned copy of the files and whether the slot was loaded.
func (m Many[M]) Get() ([]File[M], bool) { return slices.Clone(m.files), m.loaded }

// Len reports the number of loaded files; it is zero when not loaded.
func (m Many[M]) Len() int { return len(m.files) }

// All yields each loaded file in collection order without copying the slice.
func (m Many[M]) All() iter.Seq2[int, File[M]] { return slices.All(m.files) }

func (One[M]) Format(s fmt.State, _ rune)    { _, _ = s.Write([]byte("attachment slot")) }
func (One[M]) MarshalJSON() ([]byte, error)  { return nil, invalid() }
func (Many[M]) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("attachment collection slot")) }
func (Many[M]) MarshalJSON() ([]byte, error) { return nil, invalid() }

// slotDefinition shares declaration, binding, manager, loading and links for
// both slot kinds; OneSlot and ManySlot promote its exported methods.
type slotDefinition[M any, K comparable, S any] struct {
	collection Collection[M, K]
	binding    extensions.SlotBinding[M, K, S]
	manager    *Manager
	err        error
}

// OneSlot binds one model's One field to its single-file collection.
// Generated <Model>Extensions() constructs it; applications bind a manager once
// with From. A bound slot is an eager-loading relation for With and Load.
type OneSlot[M any, K comparable] struct {
	query.ExtensionSlot[M]
	slotDefinition[M, K, One[M]]
}

// ManySlot binds one model's Many field to its multiple-file collection.
type ManySlot[M any, K comparable] struct {
	query.ExtensionSlot[M]
	slotDefinition[M, K, Many[M]]
}

// DefineOne is the generated declaration boundary for a One slot. The slot
// type selects Single cardinality; a policy declaring Multiple is invalid.
func DefineOne[M any, K comparable](owner extensions.Owner[M, K], name Name, policy Policy, binding extensions.SlotBinding[M, K, One[M]]) OneSlot[M, K] {
	return OneSlot[M, K]{slotDefinition: defineSlot(owner, name, policy, Single, binding)}.From(nil)
}

// DefineMany is the generated declaration boundary for a Many slot. The slot
// type selects Multiple cardinality; a policy declaring Single is invalid.
func DefineMany[M any, K comparable](owner extensions.Owner[M, K], name Name, policy Policy, binding extensions.SlotBinding[M, K, Many[M]]) ManySlot[M, K] {
	return ManySlot[M, K]{slotDefinition: defineSlot(owner, name, policy, Multiple, binding)}.From(nil)
}

func defineSlot[M any, K comparable, S any](owner extensions.Owner[M, K], name Name, policy Policy, cardinality Cardinality, binding extensions.SlotBinding[M, K, S]) slotDefinition[M, K, S] {
	var err error
	switch {
	case policy.Cardinality != 0 && policy.Cardinality != cardinality:
		err = fault.New(fault.Invalid, "attachment slot "+binding.Field+" declares a cardinality different from its slot type")
	case policy.Localized:
		err = fault.New(fault.Invalid, "attachment slot "+binding.Field+" cannot be localized; use the explicit ForLocale collection API")
	}
	policy.Cardinality = cardinality
	return slotDefinition[M, K, S]{collection: Define(owner, name, policy), binding: binding, err: err}
}

// From returns a copy that borrows the manager. A nil manager leaves the slot
// unbound; loading, links and writes then report fault.Missing.
func (s OneSlot[M, K]) From(m *Manager) OneSlot[M, K] {
	s.manager = m
	s.ExtensionSlot = relationOf(s.slotDefinition, m, func(o One[M]) bool { return o.loaded }, func(o One[M]) int { return boolCount(o.file != nil) }, func(files []File[M]) (One[M], error) {
		switch len(files) {
		case 0:
			return One[M]{loaded: true}, nil
		case 1:
			return One[M]{file: &files[0], loaded: true}, nil
		}
		return One[M]{}, fault.New(fault.Conflict, "stored attachment cardinality differs from its collection")
	})
	return s
}

// From returns a copy that borrows the manager. A nil manager leaves the slot
// unbound; loading, links and writes then report fault.Missing.
func (s ManySlot[M, K]) From(m *Manager) ManySlot[M, K] {
	s.manager = m
	s.ExtensionSlot = relationOf(s.slotDefinition, m, func(files Many[M]) bool { return files.loaded }, func(files Many[M]) int { return len(files.files) }, func(files []File[M]) (Many[M], error) {
		return Many[M]{files: files, loaded: true}, nil
	})
	return s
}

// relationOf builds the eager-loading relation; an unbound slot reports its
// Missing fault when validated, before parent SQL.
func relationOf[M any, K comparable, S any](s slotDefinition[M, K, S], m *Manager, loaded func(S) bool, count func(S) int, convert func([]File[M]) (S, error)) query.ExtensionSlot[M] {
	binding := query.ExtensionBinding[M, S]{Name: s.binding.Field, Get: s.binding.Get, Set: s.binding.Set, Loaded: loaded, Count: count, Unbound: s.unbound()}
	if s.collection.definition != nil {
		binding.Table = s.collection.definition.owner.ModelName()
	}
	if m != nil {
		binding.Fetch = func(ctx context.Context, executor database.Executor, parents []M) ([]S, error) {
			loadedFiles, err := s.files(ctx, executor, parents)
			if err != nil {
				return nil, err
			}
			result := make([]S, len(loadedFiles))
			for i, files := range loadedFiles {
				active, ok := files.Get()
				if !ok {
					continue
				}
				if result[i], err = convert(active); err != nil {
					return nil, err
				}
			}
			return result, nil
		}
	}
	return query.NewExtensionSlot(binding)
}

// files loads ready files for parents in owner parts that fit MaxBatchFiles;
// a part beyond the property budget is halved. Owners that are soft-deleted
// or no longer exist return an unset entry and keep an unloaded slot.
func (s slotDefinition[M, K, S]) files(ctx context.Context, executor database.Executor, parents []M) ([]value.Optional[[]File[M]], error) {
	m, err := s.bound()
	if err != nil {
		return nil, err
	}
	c := s.collection
	if err := c.check(m); err != nil {
		return nil, err
	}
	size := max(1, MaxBatchFiles/c.definition.policy.MaxFiles)
	return extensions.LoadInParts(ctx, parents, size, errBatchLimit, func(ctx context.Context, part []M) ([]value.Optional[[]File[M]], error) {
		references := make([]model.Reference[M, K], len(part))
		for i, parent := range part {
			references[i] = s.binding.Reference(parent)
		}
		var batch Batch[M, K]
		if err := m.reads.Run(ctx, "attachment slot batch", func(ctx context.Context) error {
			return m.store.ReadFor(ctx, executor, func(ctx context.Context, tx *database.Tx) error {
				var err error
				batch, err = c.scanRows(ctx, tx, m, references, []string{""}, value.Optional[ID[M]]{})
				return err
			})
		}); err != nil {
			return nil, err
		}
		result := make([]value.Optional[[]File[M]], len(part))
		for i, reference := range references {
			attachments, err := batch.Get(reference)
			if errors.Is(err, database.NotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			files := make([]File[M], len(attachments))
			for j, attachment := range attachments {
				files[j] = attachment.File
			}
			result[i] = value.Set(files)
		}
		return result, nil
	})
}

func (s slotDefinition[M, K, S]) validate() error {
	if s.err != nil {
		return s.err
	}
	if err := s.binding.Validate(); err != nil {
		return err
	}
	return s.collection.Validate()
}
func (s slotDefinition[M, K, S]) bound() (*Manager, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, s.unbound()
	}
	return s.manager, nil
}
func (s slotDefinition[M, K, S]) unbound() error {
	return fault.New(fault.Missing, "attachment slot "+s.binding.Field+" is not bound to an attachments manager; bind it with <Model>Extensions().From(runtime)")
}

// attachment restores the typed owner of a loaded file for link derivation;
// the collection still checks that the file belongs to it.
func (s slotDefinition[M, K, S]) attachment(owner M, file File[M]) Attachment[M, K] {
	return Attachment[M, K]{File: file, owner: s.binding.Reference(owner)}
}

// Collection returns the underlying declaration for the explicit attachment API.
func (s slotDefinition[M, K, S]) Collection() Collection[M, K] { return s.collection }

// Describe returns the slot's inspection snapshot.
func (s slotDefinition[M, K, S]) Describe() extensions.SlotDescription {
	result := extensions.SlotDescription{Field: s.binding.Field, Kind: "many", Name: string(s.Name()), Storage: "foundry_attachments"}
	if s.collection.definition == nil {
		return result
	}
	policy := s.collection.definition.policy
	if policy.Cardinality == Single {
		result.Kind = "one"
	}
	result.Disk, result.MaxBytes, result.MaxFiles = string(policy.Disk.ID()), policy.MaxBytes, policy.MaxFiles
	for _, media := range policy.Accepted {
		result.Accepted = append(result.Accepted, string(media))
	}
	for _, variant := range policy.Variants {
		result.Variants = append(result.Variants, string(variant.name))
	}
	return result
}

// Name is the stored collection name.
func (s slotDefinition[M, K, S]) Name() Name { return s.collection.Name() }

// Validate checks the declaration, cardinality and generated binding.
func (s slotDefinition[M, K, S]) Validate() error { return s.validate() }

// Registration is the manager registration of the underlying collection,
// including the slot's cardinality and binding validation.
func (s slotDefinition[M, K, S]) Registration() Registration {
	registration := s.collection.Registration()
	if err := s.validate(); err != nil && registration.validate != nil {
		registration.validate = func(*extensions.Registry) error { return err }
	}
	return registration
}

// URL derives the public URL of one loaded file of owner without database or
// storage I/O, under the collection's active-content policy. Authorize access
// to the file first; the URL does not.
func (s slotDefinition[M, K, S]) URL(ctx context.Context, owner M, file File[M]) (string, error) {
	m, err := s.bound()
	if err != nil {
		return "", err
	}
	return s.collection.PublicURLOf(ctx, m, s.attachment(owner, file))
}

// TemporaryURL signs a read link for one loaded file, pinned to its stored
// version, without database or storage I/O.
func (s slotDefinition[M, K, S]) TemporaryURL(ctx context.Context, owner M, file File[M], options storage.LinkOptions) (storage.TemporaryURL, error) {
	m, err := s.bound()
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	return s.collection.TemporaryURLOf(ctx, m, s.attachment(owner, file), options)
}

// VariantURL derives the public URL of a generated variant of one loaded
// file. A variant not generated yet reports VariantUnavailable.
func (s slotDefinition[M, K, S]) VariantURL(ctx context.Context, owner M, file File[M], variant Variant) (string, error) {
	m, err := s.bound()
	if err != nil {
		return "", err
	}
	return s.collection.VariantPublicURLOf(ctx, m, s.attachment(owner, file), variant)
}

// PublicURL derives the public URL of owner's loaded file without I/O. It is
// absent when the loaded slot holds no file and fails when it was not loaded.
func (s OneSlot[M, K]) PublicURL(ctx context.Context, owner M) (value.Optional[string], error) {
	file, loaded := s.binding.Get(owner).Get()
	if !loaded {
		return value.Optional[string]{}, notLoaded()
	}
	present, ok := file.Get()
	if !ok {
		return value.Optional[string]{}, nil
	}
	url, err := s.URL(ctx, owner, present)
	if err != nil {
		return value.Optional[string]{}, err
	}
	return value.Set(url), nil
}

// PublicURLs derives the public URLs of owner's loaded files in collection
// order without I/O. It fails when the slot was not loaded.
func (s ManySlot[M, K]) PublicURLs(ctx context.Context, owner M) ([]string, error) {
	files, loaded := s.binding.Get(owner).Get()
	if !loaded {
		return nil, notLoaded()
	}
	urls := make([]string, len(files))
	for i, file := range files {
		var err error
		if urls[i], err = s.URL(ctx, owner, file); err != nil {
			return nil, err
		}
	}
	return urls, nil
}

func notLoaded() error {
	return fault.New(fault.Missing, "attachment slot is not loaded; load it with With or Load")
}
func boolCount(present bool) int {
	if present {
		return 1
	}
	return 0
}

func (OneSlot[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("attachment slot descriptor"))
}
func (ManySlot[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("attachment collection slot descriptor"))
}
