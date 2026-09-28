package storage

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/weiloon1234/Foundry-Go/value"
)

// ReadBytes is an explicitly bounded in-memory convenience. For large objects,
// use Open. It closes its reader on success and failure and returns no partial
// bytes. maximum applies to the requested span, not the complete object.
func (d *Disk) ReadBytes(ctx context.Context, key ObjectKey, maximum int64, options ReadOptions) ([]byte, ReadInfo, error) {
	if err := d.Validate(); err != nil {
		return nil, ReadInfo{}, err
	}
	if maximum < 0 || maximum > d.config.MaxObjectBytes {
		return nil, ReadInfo{}, Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	body, info, err := d.Open(ctx, key, options)
	if err != nil {
		return nil, ReadInfo{}, err
	}
	if info.Length > maximum {
		return nil, ReadInfo{}, Failure(LimitExceeded, OpenOperation, NotApplicable, closeBody(body))
	}
	data, readErr := io.ReadAll(io.LimitReader(body, maximum+1))
	err = errors.Join(readErr, closeBody(body))
	if err != nil {
		return nil, ReadInfo{}, finish(OpenOperation, ctx, err, NotApplicable)
	}
	if int64(len(data)) != info.Length {
		return nil, ReadInfo{}, Failure(IntegrityFailed, OpenOperation, NotApplicable, nil)
	}
	return data, info, nil
}

// PutFile opens a trusted application-selected regular file and delegates to Put.
// It never interprets an object key or client upload filename as a filesystem path.
// The caller's file is not removed or modified. Concurrent file changes can fail
// the exact length or optional checksum check; take a snapshot when required.
func (d *Disk) PutFile(ctx context.Context, key ObjectKey, path string, options PutOptions) (StoredObject, error) {
	if err := d.Validate(); err != nil {
		return StoredObject{}, err
	}
	if err := key.Validate(); err != nil {
		return StoredObject{}, err
	}
	if err := options.Validate(d.config.MaxObjectBytes); err != nil {
		return StoredObject{}, err
	}
	if ctx == nil {
		return StoredObject{}, Failure(Invalid, PutOperation, Unchanged, nil)
	}
	if err := ctx.Err(); err != nil {
		return StoredObject{}, Failure(Unavailable, PutOperation, Unchanged, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return StoredObject{}, Failure(Unavailable, PutOperation, Unchanged, err)
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = Failure(Invalid, PutOperation, Unchanged, nil)
	}
	if err == nil {
		if size, supplied := options.Size.Get(); supplied && size != info.Size() {
			err = Failure(Invalid, PutOperation, Unchanged, nil)
		}
	}
	if err != nil {
		return StoredObject{}, Failure(Invalid, PutOperation, Unchanged, errors.Join(err, file.Close()))
	}
	options.Size = value.Set(info.Size())
	result, err := d.Put(ctx, key, file, options)
	cleanup := file.Close()
	if cleanup != nil {
		if err == nil {
			err = Failure(Unavailable, PutOperation, Applied, cleanup)
		} else {
			err = joinCleanup(PutOperation, err, cleanup)
		}
	}
	if err != nil {
		return StoredObject{}, err
	}
	return result, nil
}

// CopyOptions reads one complete source version and publishes a destination.
// Destination metadata defaults to the source's media type, exact size and
// available checksum. Conditions remain explicit. Copy is always streamed with
// the adapter's bounded buffers; it never buffers the entire object.
type CopyOptions struct {
	Source      ReadOptions
	Destination PutOptions
}

// CopyTo copies this disk's source to destination without deleting the source.
// Capacity on both disks is required (two slots for a copy within one disk).
// Source close failures after a successful publication report Applied. The
// returned object is zero on any failure; reconcile uncertain writes by key.
func (d *Disk) CopyTo(ctx context.Context, source ObjectKey, destination *Disk, target ObjectKey, options CopyOptions) (StoredObject, error) {
	if err := d.Validate(); err != nil {
		return StoredObject{}, err
	}
	if err := destination.Validate(); err != nil {
		return StoredObject{}, err
	}
	if err := source.Validate(); err != nil {
		return StoredObject{}, err
	}
	if err := target.Validate(); err != nil {
		return StoredObject{}, err
	}
	if options.Source.Range.IsSet() || d == destination && source == target {
		return StoredObject{}, Failure(Invalid, CopyOperation, Unchanged, nil)
	}
	if err := options.Destination.Validate(destination.config.MaxObjectBytes); err != nil {
		return StoredObject{}, err
	}
	body, info, err := d.Open(ctx, source, options.Source)
	if err != nil {
		return StoredObject{}, err
	}
	write := options.Destination
	if write.ContentType == "" {
		write.ContentType = info.Object.ContentType
	}
	if size, supplied := write.Size.Get(); supplied && size != info.Length {
		return StoredObject{}, Failure(Invalid, CopyOperation, Unchanged, closeBody(body))
	}
	write.Size = value.Set(info.Length)
	if !write.Checksum.IsSet() {
		write.Checksum = info.Object.Checksum
	}
	result, err := destination.Put(ctx, target, body, write)
	cleanup := closeBody(body)
	if cleanup != nil {
		if err == nil {
			err = Failure(Unavailable, CopyOperation, Applied, cleanup)
		} else {
			err = joinCleanup(CopyOperation, err, cleanup)
		}
	}
	if err != nil {
		return StoredObject{}, err
	}
	return result, nil
}

// MoveResult records the two separately committed effects. A failed move can
// retain a complete Destination. SourceOutcome distinguishes a preserved source
// from an applied or uncertain deletion. Never automatically compensate by
// deleting Destination: another writer might already have replaced it.
type MoveResult struct {
	Destination        value.Optional[StoredObject]
	DestinationOutcome Outcome
	SourceOutcome      Outcome
}

// MoveTo is copy-then-conditional-delete, not an atomic rename. It pins the
// source with its ETag and preserves replacements with a different validator. Adapters
// without conditional read/delete are rejected before writing the destination.
// Both adapters must expose ObjectLocator. Physical aliases are rejected even
// if their disk names, namespaces or logical keys differ. Distinct stores can
// use the same logical key.
func (d *Disk) MoveTo(ctx context.Context, source ObjectKey, destination *Disk, target ObjectKey, options CopyOptions) (MoveResult, error) {
	result := MoveResult{DestinationOutcome: Unchanged, SourceOutcome: Unchanged}
	if err := d.Validate(); err != nil {
		return result, err
	}
	if err := destination.Validate(); err != nil {
		return result, err
	}
	if options.Source.Range.IsSet() || options.Source.Version != "" {
		return result, Failure(Invalid, MoveOperation, Unchanged, nil)
	}
	if !d.capabilities.ConditionalRead || !d.capabilities.ConditionalDelete {
		return result, Failure(Unsupported, MoveOperation, Unchanged, nil)
	}
	sourceAddress, err := d.address(ctx, source)
	if err != nil {
		return result, err
	}
	targetAddress, err := destination.address(ctx, target)
	if err != nil {
		return result, err
	}
	if sourceAddress == targetAddress {
		return result, Failure(Invalid, MoveOperation, Unchanged, nil)
	}
	info, err := d.Stat(ctx, source, options.Source)
	if err != nil {
		return result, err
	}
	if info.ETag == "" {
		return result, Failure(Unsupported, MoveOperation, Unchanged, nil)
	}
	options.Source.IfMatch = info.ETag
	copied, err := d.CopyTo(ctx, source, destination, target, options)
	if err != nil {
		result.DestinationOutcome = mutationOutcome(err)
		return result, err
	}
	result.Destination = value.Set(copied)
	result.DestinationOutcome = Applied
	err = d.Delete(ctx, source, DeleteOptions{IfMatch: info.ETag})
	if err != nil {
		result.SourceOutcome = mutationOutcome(err)
		return result, err
	}
	result.SourceOutcome = Applied
	return result, nil
}

func mutationOutcome(err error) Outcome {
	var failure *Error
	if errors.As(err, &failure) {
		if failure.Outcome() == NotApplicable {
			return Unchanged
		}
		return failure.Outcome()
	}
	return Unknown
}
func joinCleanup(op Operation, primary, cleanup error) error {
	code, outcome := Unavailable, mutationOutcome(primary)
	var prior *Error
	if errors.As(primary, &prior) {
		code = prior.Code()
	}
	result := Failure(code, op, outcome, errors.Join(primary, cleanup))
	if prior != nil {
		if id, ok := prior.Cleanup().Get(); ok {
			result = result.WithCleanup(id)
		}
	}
	return result
}
