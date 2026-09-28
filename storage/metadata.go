package storage

import (
	"encoding/hex"
	"mime"
	"strings"

	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxContentTypeBytes = 512

// MediaType is stored object metadata, not proof of a validated file format.
type MediaType string

const Binary MediaType = "application/octet-stream"

func (m MediaType) Validate() error {
	if len(m) == 0 || len(m) > MaxContentTypeBytes {
		return Failure(Invalid, PutOperation, Unchanged, nil)
	}
	for _, c := range m {
		if c < 32 || c == 127 {
			return Failure(Invalid, PutOperation, Unchanged, nil)
		}
	}
	parsed, _, err := mime.ParseMediaType(string(m))
	major, minor, ok := strings.Cut(parsed, "/")
	if err != nil || !ok || major == "" || minor == "" || strings.ContainsAny(parsed, "*") || strings.Contains(minor, "/") {
		return Failure(Invalid, PutOperation, Unchanged, err)
	}
	return nil
}

// ETag is an opaque strong object validator, including its quotes. It is not
// assumed to be an MD5 or other content digest. Zero means no validator.
type ETag string

func (t ETag) Validate() error {
	if t == "" {
		return nil
	}
	if len(t) < 2 || len(t) > 1024 || t[0] != '"' || t[len(t)-1] != '"' {
		return Failure(Invalid, StatOperation, NotApplicable, nil)
	}
	for _, c := range []byte(t[1 : len(t)-1]) {
		if c < 0x21 || c == 0x22 || c > 0x7e {
			return Failure(Invalid, StatOperation, NotApplicable, nil)
		}
	}
	return nil
}

// VersionID selects an immutable provider object version. A local generation
// validator is an ETag, not a promise that previous versions remain readable.
type VersionID string

func (v VersionID) Validate() error {
	if len(v) > 1024 {
		return Failure(Invalid, StatOperation, NotApplicable, nil)
	}
	for _, c := range v {
		if c < 32 || c == 127 {
			return Failure(Invalid, StatOperation, NotApplicable, nil)
		}
	}
	return nil
}

type SHA256 [32]byte

func ParseSHA256(text string) (SHA256, error) {
	var result SHA256
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != len(result) {
		return result, Failure(Invalid, PutOperation, Unchanged, err)
	}
	copy(result[:], raw)
	return result, nil
}
func (s SHA256) String() string { return hex.EncodeToString(s[:]) }

// ObjectInfo describes a complete object. Checksum is a full-object SHA-256
// when available; do not compare it to the bytes of a partial ranged read.
type ObjectInfo struct {
	Key         ObjectKey
	Size        int64
	ContentType MediaType
	Modified    temporal.DateTime
	ETag        ETag
	Version     VersionID
	Checksum    value.Optional[SHA256]
}

func (i ObjectInfo) Validate() error {
	for _, err := range []error{i.Key.Validate(), i.ContentType.Validate(), i.ETag.Validate(), i.Version.Validate()} {
		if err != nil {
			return err
		}
	}
	if i.Size < 0 || i.Modified.UTC().IsZero() {
		return Failure(IntegrityFailed, StatOperation, NotApplicable, nil)
	}
	return nil
}

type StoredObject struct {
	Disk   DiskID
	Object ObjectInfo
}

// ReadInfo separates the complete object's metadata from the returned byte span.
type ReadInfo struct {
	Object         ObjectInfo
	Offset, Length int64
}
