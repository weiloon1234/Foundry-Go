package http

import (
	"path/filepath"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
)

// UploadSettings carries deployment-owned ingress settings into typed endpoint
// assembly. Disabled preserves existing descriptors and development temp paths.
// Request is the large capture pool; SmallDirectory has independent capacity.
type UploadSettings struct {
	Enabled        bool
	SmallDirectory string
	Request        UploadPoolConfig
	BodyBytes      int64
	FileBytes      int64
	Timeout        time.Duration
	// AttachmentMaxActive is the large-policy sublimit for the attachment owner.
	// Reserve additional capacity there for small publications.
	AttachmentMaxActive int
	// StorageTimeout is the total publication budget of the selected disk.
	StorageTimeout time.Duration
	// Staging supplies the local adapter's independent staging allocation. The
	// adapter owns its admission, cleanup references and persistent data budget.
	Staging UploadPoolConfig
}

// Validate checks declaration relationships without filesystem I/O. Creating
// the pool separately verifies exclusive writable storage before serving.
func (s UploadSettings) Validate() error {
	if !s.Enabled {
		return nil
	}
	if err := (upload.PoolConfig{Directory: s.Request.Directory, MaxActive: s.Request.MaxActive, MaxBytes: s.Request.MaxBytes}).Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(s.SmallDirectory) || filepath.Clean(s.SmallDirectory) == filepath.Clean(s.Request.Directory) || s.FileBytes <= 0 || s.BodyBytes <= s.FileBytes || s.BodyBytes > MaxRouteBodyBytes || s.BodyBytes > s.Request.MaxBytes || s.Timeout <= 0 || s.Timeout > MaxRouteTimeout || s.AttachmentMaxActive <= 0 || s.StorageTimeout <= 0 || s.StorageTimeout > s.Timeout || !filepath.IsAbs(s.Staging.Directory) || s.Staging.MaxActive <= 0 || s.Staging.MaxBytes <= 0 {
		return safeerror.NewFault(fault.Invalid, "invalid deployed upload limits or directories")
	}
	return nil
}
