package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
	"time"
)

type DiskDriver string

const (
	LocalDisk DiskDriver = "local"
	S3Disk    DiskDriver = "s3"
	R2Disk    DiskDriver = "r2"
)

type LocalDiskSettings struct {
	Root    string
	MaxScan int
	Sync    bool
}
type CloudDiskSettings struct {
	Bucket, Region, Endpoint, Namespace, PublicBase string
	Credentials                                     credentials.Name
	PathStyle                                       bool
	PartBytes                                       int64
	MaxUploads, ReadAttempts, PartAttempts          int
	AbortTimeout                                    time.Duration
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

func DefaultDiskSettings() DiskSettings {
	l, c := local.DefaultConfig(""), s3.DefaultConfig("", "")
	return DiskSettings{Driver: LocalDisk, Config: storage.DefaultConfig(), Local: LocalDiskSettings{MaxScan: l.MaxScan, Sync: l.Sync}, Cloud: CloudDiskSettings{PartBytes: c.PartBytes, MaxUploads: c.MaxUploads, ReadAttempts: c.ReadAttempts, PartAttempts: c.PartAttempts, AbortTimeout: c.AbortTimeout}}
}
func (s DiskSettings) cloudConfig(provider credentials.Provider) (s3.Config, error) {
	c := s3.DefaultConfig(s.Cloud.Bucket, s.Cloud.Region)
	if s.Driver == R2Disk {
		c = s3.R2WithCredentials(s.Cloud.Bucket, s.Cloud.Endpoint, provider)
	} else {
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
	return c, c.Validate()
}

type Disks map[storage.DiskID]DiskSettings

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

type StorageSettings struct {
	Default storage.DiskID
	Disks   Disks
}

func DefaultStorageSettings() StorageSettings { return StorageSettings{Default: "default"} }

type CredentialSources map[credentials.Name]credentials.Settings

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
