// Package probe checks deployment storage access with a temporary object.
package probe

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// Result contains only safe disk diagnostics and the probe's own cleanup key.
type Result struct {
	Disk        storage.DiskID `json:"disk"`
	Outcome     string         `json:"outcome"`
	Stage       string         `json:"stage,omitempty"`
	Code        string         `json:"code,omitempty"`
	LeftoverKey string         `json:"leftover_key,omitempty"`
}

// Disk writes, reads and deletes one random object within a shared 20-second
// budget. Failed reads and ambiguous writes get a separate cleanup budget.
func Disk(ctx context.Context, disk *storage.Disk) Result {
	result := Result{Disk: disk.ID(), Outcome: "failed", Stage: "write"}
	var nonce [16]byte
	var payload [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		result.Code = "storage_failed"
		return result
	}
	if _, err := rand.Read(payload[:]); err != nil {
		result.Code = "storage_failed"
		return result
	}
	name := "foundry-deploy-probe/" + hex.EncodeToString(nonce[:])
	key, err := storage.ParseKey(name)
	if err != nil {
		result.Code = classify(err)
		return result
	}
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if disk.Delete(cleanupCtx, key, storage.DeleteOptions{}) != nil {
			result.LeftoverKey = name
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := disk.PutBytes(probeCtx, key, payload[:], storage.PutOptions{}); err != nil {
		result.Code = classify(err)
		var failure *storage.Error
		if !callback.ErrorAs(err, &failure) || failure.Outcome() != storage.Unchanged {
			cleanup()
		}
		return result
	}
	result.Stage = "read"
	data, _, err := disk.ReadBytes(probeCtx, key, int64(len(payload)), storage.ReadOptions{})
	if err != nil {
		result.Code = classify(err)
		cleanup()
		return result
	}
	if !bytes.Equal(data, payload[:]) {
		result.Code = "storage_probe_mismatch"
		cleanup()
		return result
	}
	result.Stage = "delete"
	if err := disk.Delete(probeCtx, key, storage.DeleteOptions{}); err != nil {
		result.Code = classify(err)
		result.LeftoverKey = name
		return result
	}
	return Result{Disk: disk.ID(), Outcome: "ok"}
}

func classify(err error) string {
	for _, code := range []storage.Code{
		storage.NotFound, storage.Forbidden, storage.Unsupported,
		storage.PreconditionFailed, storage.RangeNotSatisfiable, storage.LimitExceeded,
		storage.Invalid, storage.Unavailable, storage.IntegrityFailed, storage.Closed,
	} {
		if callback.ErrorIs(err, code) {
			return string(code)
		}
	}
	return "storage_failed"
}
