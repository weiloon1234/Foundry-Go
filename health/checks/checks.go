// Package checks adapts framework services to bounded, read-only readiness
// probes. Each adapter uses only its service's public API and performs no
// writes. Register the service's owner before the readiness module so probes
// drain before that owner closes.
package checks

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// DiskKey is the object key Disk probes by default. It need not exist: a
// missing object proves the backend answered, while network, missing-bucket or
// root failures report Down.
const DiskKey = "foundry-readiness-probe"

// DiskProbeKey returns DiskKey as a validated object key.
func DiskProbeKey() storage.ObjectKey {
	key, _ := storage.ParseKey(DiskKey)
	return key
}

// Disk stats one object key through the disk's normal read path (a HEAD/stat
// request for S3-compatible and local disks). It never lists or writes.
//
// A forbidden answer counts as reachable: S3 answers 403 instead of 404 for a
// missing key when the credentials lack s3:ListBucket, which an application
// that only reads and writes known keys need not grant. A HEAD response has no
// error body, so the probe cannot tell that apart from rejected credentials;
// it proves the endpoint and bucket answer, while credential failures surface
// through ordinary operations. Grant s3:GetObject on the probe key plus
// s3:ListBucket (it may be limited to the probe key's prefix) to get 404s.
func Disk(id health.ProbeID, disk *storage.Disk, key storage.ObjectKey) (health.Probe, error) {
	if disk == nil {
		return health.Probe{}, fault.New(fault.Invalid, "disk readiness probe requires a disk")
	}
	if err := key.Validate(); err != nil {
		return health.Probe{}, err
	}
	return health.Probe{ID: id, Check: func(ctx context.Context) error {
		_, err := disk.Exists(ctx, key)
		if err != nil && forbidden(err) {
			return ctx.Err()
		}
		return err
	}}, nil
}

// forbidden classifies with the bounded walker; provider causes are foreign.
func forbidden(err error) bool {
	found := false
	errorgraph.Walk(err, func(current error) bool {
		found = errorgraph.Matches(current, storage.Forbidden)
		return !found
	})
	return found
}

// Mailer reports whether the mailer still admits submissions. Mail drivers
// expose submission only, so the probe does not contact SMTP or provider APIs;
// transport failures surface through send results and mail diagnostics.
func Mailer(id health.ProbeID, mailer *email.Mailer) (health.Probe, error) {
	if mailer == nil {
		return health.Probe{}, fault.New(fault.Invalid, "mail readiness probe requires a mailer")
	}
	return health.Probe{ID: id, Check: func(ctx context.Context) error {
		if mailer.Snapshot().Closing {
			return fault.New(fault.Closed, "mailer is closing")
		}
		return ctx.Err()
	}}, nil
}
