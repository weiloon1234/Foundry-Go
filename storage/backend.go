package storage

import (
	"context"
	"io"
)

// Capabilities are immutable adapter guarantees. Unsupported options must fail
// before consuming a source or mutating data; no option may be silently ignored.
// Individual flags describe single options; the combination flags below make
// provider limits on combined selectors explicit. ValidatePut, ValidateRead and
// ValidateDelete are the shared checks used by Disk and adapters alike.
type Capabilities struct {
	// RequiresNFCKeys restricts object keys, namespaces and listing prefixes to
	// Unicode NFC. Adapters reject other spellings; they never normalize input
	// into another object's identity. Local storage and AWS do not require NFC.
	RequiresNFCKeys    bool
	Ranges             bool
	ConditionalRead    bool
	ConditionalCreate  bool
	ConditionalReplace bool
	ConditionalDelete  bool
	Versions           bool
	// ConditionalVersionDelete permits IfMatch together with an explicit Version
	// on Delete. Providers that evaluate a delete condition against the current
	// object (AWS) leave it false: an explicit immutable Version already selects
	// exactly one object, so callers delete by Version alone.
	ConditionalVersionDelete bool
	// DelimitedList supports one-level ListOptions.Delimited listings that
	// report child prefixes as Page.Directories.
	DelimitedList bool
	// ObjectMetadata supports the optional PutOptions.Metadata fields (cache
	// and presentation headers, custom metadata, storage class, encryption key).
	ObjectMetadata bool
	// ConditionalWriteMaxBytes, when positive, requires a declared Size within
	// this bound for conditional creates/replacements. Zero adds no size limit.
	ConditionalWriteMaxBytes int64
}

// Backend implementations stream with bounded buffers and preserve cancellation.
// Put never closes its caller-owned source. A blocked uncooperative source may
// delay cancellation; capacity remains owned until the actual call exits. Open
// transfers a reader whose Close is safe concurrently with Read and interrupts
// it where supported. A reader returned alongside an error must still be closed.
//
// Each successful mutation must be fully published. Errors classify whether it
// was unchanged, applied or uncertain; adapters never blindly retry mutations.
// Complete metadata and object bytes must describe the same version. Delete is
// idempotent only when unconditional; a missing conditional target conflicts.
// List is bounded and not a transactional snapshot. Never return directories,
// upload fragments or internal metadata as objects; entries that cannot be
// represented as framework objects are skipped and counted in Page.Skipped.
// Backend lifetime is borrowed by Disk: close the Disk and wait for Done before
// closing its adapter.
type Backend interface {
	Capabilities() Capabilities
	Put(context.Context, ObjectKey, io.Reader, PutOptions) (ObjectInfo, error)
	Open(context.Context, ObjectKey, ReadOptions) (io.ReadCloser, ReadInfo, error)
	Stat(context.Context, ObjectKey, ReadOptions) (ObjectInfo, error)
	Delete(context.Context, ObjectKey, DeleteOptions) error
	List(context.Context, ListOptions) (Page, error)
}

// ValidatePut rejects an unsupported write contract before consuming its source.
// Adapters and Disk share this check; object-size/config validation is separate.
func (c Capabilities) ValidatePut(options PutOptions) error {
	conditional := options.Condition.RequiresAbsence() || options.Condition.Match() != ""
	if options.Condition.RequiresAbsence() && !c.ConditionalCreate || options.Condition.Match() != "" && !c.ConditionalReplace {
		return Failure(Unsupported, PutOperation, Unchanged, nil)
	}
	if conditional && c.ConditionalWriteMaxBytes > 0 {
		size, supplied := options.Size.Get()
		if !supplied || size > c.ConditionalWriteMaxBytes {
			return Failure(Unsupported, PutOperation, Unchanged, nil)
		}
	}
	if !options.Metadata.IsZero() && !c.ObjectMetadata {
		return Failure(Unsupported, PutOperation, Unchanged, nil)
	}
	return nil
}

// ValidateRead rejects read selectors the adapter cannot honor exactly.
func (c Capabilities) ValidateRead(options ReadOptions) error {
	if options.Range.IsSet() && !c.Ranges || options.IfMatch != "" && !c.ConditionalRead || options.Version != "" && !c.Versions {
		return Failure(Unsupported, OpenOperation, NotApplicable, nil)
	}
	return nil
}

// ValidateDelete rejects deletion selectors, including unsupported combinations,
// before any provider request. A version selector deletes exactly that version.
func (c Capabilities) ValidateDelete(options DeleteOptions) error {
	if options.IfMatch != "" && !c.ConditionalDelete || options.Version != "" && !c.Versions || options.IfMatch != "" && options.Version != "" && !c.ConditionalVersionDelete {
		return Failure(Unsupported, DeleteOperation, Unchanged, nil)
	}
	return nil
}

// ValidateList rejects listing modes the adapter cannot represent.
func (c Capabilities) ValidateList(options ListOptions) error {
	if options.Delimited && !c.DelimitedList {
		return Failure(Unsupported, ListOperation, NotApplicable, nil)
	}
	return nil
}
