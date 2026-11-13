package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
	"time"
)

// DiskDriver selects a storage disk implementation.
type DiskDriver string

const (
	LocalDisk DiskDriver = "local"
	S3Disk    DiskDriver = "s3"
	R2Disk    DiskDriver = "r2"
	// CompatibleDisk is a generic S3-compatible endpoint whose guarantees are
	// declared in CloudDiskSettings.Compatible.
	CompatibleDisk DiskDriver = "s3_compatible"
)

// LocalDiskSettings configures local disk root, scan budget, and synchronization.
type LocalDiskSettings struct {
	Root    string
	Staging local.StagingLimits
	MaxScan int
	Sync    bool
}

// CloudDiskSettings configures cloud storage location, credentials, transfer limits, and
// compatibility options.
type CloudDiskSettings struct {
	Bucket, Region, Endpoint, Namespace, PublicBase string
	Credentials                                     credentials.Name
	PathStyle                                       bool
	PartBytes                                       int64
	MaxUploads, ReadAttempts, PartAttempts          int
	AbortTimeout                                    time.Duration
	PartConcurrency                                 int
	VerifyPublication                               bool
	// AllowHTTP and Compatible apply only to the s3_compatible driver.
	AllowHTTP  bool
	Compatible s3.CompatibleCapabilities
}

// DiskSettings owns one driver selection and the common storage limits.
//
//foundry:config
type DiskSettings struct {
	Driver DiskDriver
	Config storage.Config
	Local  LocalDiskSettings
	Cloud  CloudDiskSettings
}

// DefaultDiskSettings returns local disk defaults and cloud transfer defaults.
func DefaultDiskSettings() DiskSettings {
	l, c := local.DefaultConfig(""), s3.DefaultConfig("", "")
	return DiskSettings{Driver: LocalDisk, Config: storage.DefaultConfig(), Local: LocalDiskSettings{MaxScan: l.MaxScan, Sync: l.Sync}, Cloud: CloudDiskSettings{PartBytes: c.PartBytes, MaxUploads: c.MaxUploads, ReadAttempts: c.ReadAttempts, PartAttempts: c.PartAttempts, AbortTimeout: c.AbortTimeout, PartConcurrency: c.PartConcurrency, VerifyPublication: c.VerifyPublication}}
}
func (s DiskSettings) cloudConfig(provider credentials.Provider) (s3.Config, error) {
	c := s3.DefaultConfig(s.Cloud.Bucket, s.Cloud.Region)
	switch s.Driver {
	case R2Disk:
		c = s3.R2WithCredentials(s.Cloud.Bucket, s.Cloud.Endpoint, provider)
	case CompatibleDisk:
		c = s3.CompatibleConfig(s.Cloud.Bucket, s.Cloud.Region, s.Cloud.Endpoint, s.Cloud.Compatible).WithCredentials(provider)
		c.AllowHTTP = s.Cloud.AllowHTTP
	default:
		c = c.WithCredentials(provider)
		c.Endpoint = s.Cloud.Endpoint
	}
	var err error
	c.Namespace, err = storage.ParsePrefix(s.Cloud.Namespace)
	if err != nil {
		return c, err
	}
	if s.Cloud.PublicBase != "" {
		c.PublicBase, err = storage.ParsePublicBase(s.Cloud.PublicBase)
		if err != nil {
			return c, err
		}
	}
	c.PathStyle = c.PathStyle || s.Cloud.PathStyle
	c.PartBytes = s.Cloud.PartBytes
	c.MaxUploads = s.Cloud.MaxUploads
	c.MaxObjectBytes = s.Config.MaxObjectBytes
	c.ReadAttempts = s.Cloud.ReadAttempts
	c.PartAttempts = s.Cloud.PartAttempts
	c.AbortTimeout = s.Cloud.AbortTimeout
	if s.Cloud.PartConcurrency != 0 {
		c.PartConcurrency = s.Cloud.PartConcurrency
	}
	c.VerifyPublication = s.Cloud.VerifyPublication
	return c, c.Validate()
}

// Disks maps disk identifiers to settings.
type Disks map[storage.DiskID]DiskSettings

// UnmarshalText decodes disk settings, replacing the receiver only on success.
func (m *Disks) UnmarshalText(data []byte) error {
	schema, err := DiskSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(storage.DiskID) DiskSettings { return DefaultDiskSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

// StorageSettings configures named storage disks and their default.
type StorageSettings struct {
	Default storage.DiskID
	Disks   Disks
}

// DefaultStorageSettings returns storage settings selecting the default disk name.
func DefaultStorageSettings() StorageSettings { return StorageSettings{Default: "default"} }

// CredentialSources maps credential names to source settings.
type CredentialSources map[credentials.Name]credentials.Settings

// UnmarshalText decodes credential source settings, replacing the receiver only on success.
func (m *CredentialSources) UnmarshalText(data []byte) error {
	schema, err := credentials.SettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(credentials.Name) credentials.Settings { return credentials.DefaultSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}
