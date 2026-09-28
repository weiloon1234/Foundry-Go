package storage

import (
	"context"
	"io"
)

// Capabilities are immutable adapter guarantees. Unsupported options must fail
// before consuming a source or mutating data; no option may be silently ignored.
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
// upload fragments or internal metadata as objects. Backend lifetime is borrowed
// by Disk: close the Disk and wait for Done before closing its adapter.
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
	return nil
}
