package cache

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxTags bounds application tag references per operation. One additional reserved
// namespace stamp is permitted. Config.MaxTags may impose a smaller application limit.
const MaxTags = 64
const TagVersionBytes = 16

// TagVersion is an opaque random identity. Missing metadata must get a fresh
// version; recreating a previous/default version could resurrect stale values.
type TagVersion struct{ value [TagVersionBytes]byte }

func NewTagVersion() (TagVersion, error) {
	var v TagVersion
	if _, err := rand.Read(v.value[:]); err != nil {
		return TagVersion{}, fault.Wrap(fault.Internal, "cache tag entropy failed", err)
	}
	if err := v.Validate(); err != nil {
		return TagVersion{}, err
	}
	return v, nil
}
func ParseTagVersion(data []byte) (TagVersion, error) {
	if len(data) != TagVersionBytes {
		return TagVersion{}, fault.New(fault.Invalid, "invalid cache tag version size")
	}
	var v TagVersion
	copy(v.value[:], data)
	return v, v.Validate()
}
func (v TagVersion) Validate() error {
	if v.value == [TagVersionBytes]byte{} {
		return fault.New(fault.Invalid, "cache tag version is not initialized")
	}
	return nil
}
func (v TagVersion) Bytes() []byte { return slices.Clone(v.value[:]) }

// TagStamp is the adapter boundary for a resolved tag identity and version.
type TagStamp struct {
	Key     EntryKey
	Version TagVersion
}
type taggedKeyData struct {
	base        EntryKey
	key         EntryKey
	flight      EntryKey
	stamps      []TagStamp
	fingerprint [sha256.Size]byte
}

// TaggedKey owns an immutable canonical snapshot. Its data address is stable
// across invalidations; its fill address includes versions for Remember isolation.
type TaggedKey struct{ data *taggedKeyData }

// NewTaggedKey is for adapters. Applications construct typed tags and WithTags
// views. Duplicate keys with conflicting versions and cross-namespace tags fail.
func NewTaggedKey(base EntryKey, stamps []TagStamp) (TaggedKey, error) {
	if err := base.Validate(); err != nil {
		return TaggedKey{}, err
	}
	if base.namespaceTag {
		return TaggedKey{}, fault.New(fault.Invalid, "namespace metadata cannot be a cache payload address")
	}

	if len(stamps) == 0 || len(stamps) > MaxTags+1 {
		return TaggedKey{}, fault.New(fault.Invalid, "invalid cache tag count")
	}
	ordered := slices.Clone(stamps)
	applicationTags := 0
	for _, stamp := range ordered {
		if !stamp.Key.namespaceTag {
			applicationTags++
		}
		if err := stamp.Key.Validate(); err != nil {
			return TaggedKey{}, err
		}
		if err := stamp.Version.Validate(); err != nil {
			return TaggedKey{}, err
		}
		if stamp.Key.Namespace() != base.Namespace() {
			return TaggedKey{}, fault.New(fault.Invalid, "cache tags cross namespaces")
		}
	}
	if applicationTags > MaxTags {
		return TaggedKey{}, fault.New(fault.Invalid, "invalid cache tag count")
	}
	// Render each address once; sorting compares the rendered text.
	type rendered struct {
		stamp TagStamp
		text  string
	}
	items := make([]rendered, len(ordered))
	for i, stamp := range ordered {
		items[i] = rendered{stamp, stamp.Key.String()}
	}
	slices.SortFunc(items, func(a, b rendered) int { return strings.Compare(a.text, b.text) })
	unique := ordered[:0]
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if len(unique) > 0 && unique[len(unique)-1].Key == item.stamp.Key {
			if unique[len(unique)-1].Version != item.stamp.Version {
				return TaggedKey{}, fault.New(fault.Invalid, "cache tag versions conflict")
			}
			continue
		}
		unique = append(unique, item.stamp)
		texts = append(texts, item.text)
	}
	versions := sha256.New()
	versions.Write([]byte("versions\x00"))
	for i, stamp := range unique {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(texts[i])))
		versions.Write(size[:])
		versions.Write([]byte(texts[i]))
		versions.Write(stamp.Version.value[:])
	}
	data := &taggedKeyData{base: base, key: taggedIdentity(base, texts), flight: base, stamps: slices.Clone(unique)}
	copy(data.fingerprint[:], versions.Sum(nil))
	fillBytes := append([]byte("tagged-fill\x00"), data.key.address.Hash[:]...)
	fillBytes = append(fillBytes, data.fingerprint[:]...)
	data.flight.address.Hash = sha256.Sum256(fillBytes)
	return TaggedKey{data: data}, nil
}

// taggedIdentity derives the stable payload address from base and the rendered,
// canonical tag addresses. It does not depend on versions.
func taggedIdentity(base EntryKey, texts []string) EntryKey {
	identity := sha256.New()
	identity.Write([]byte("tagged\x00"))
	identity.Write(base.address.Hash[:])
	for _, text := range texts {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(text)))
		identity.Write(size[:])
		identity.Write([]byte(text))
	}
	key := base
	copy(key.address.Hash[:], identity.Sum(nil))
	return key
}

// TaggedDataKey returns the stable payload address NewTaggedKey produces for base
// under the canonical metadata addresses tags, for any versions. Adapters use it
// to address a combined metadata/payload operation before versions are known.
func TaggedDataKey(base EntryKey, tags []EntryKey) (EntryKey, error) {
	if err := base.Validate(); err != nil {
		return EntryKey{}, err
	}
	if base.namespaceTag {
		return EntryKey{}, fault.New(fault.Invalid, "namespace metadata cannot be a cache payload address")
	}
	if err := ValidateTagKeys(tags); err != nil {
		return EntryKey{}, err
	}
	if tags[0].Namespace() != base.Namespace() {
		return EntryKey{}, fault.New(fault.Invalid, "cache tags cross namespaces")
	}
	texts := make([]string, len(tags))
	for i, key := range tags {
		texts[i] = key.String()
	}
	return taggedIdentity(base, texts), nil
}

func (k TaggedKey) Validate() error {
	if k.data == nil {
		return fault.New(fault.Invalid, "tagged cache key is not initialized")
	}
	return nil
}

// resolves reports whether k is the snapshot of base under exactly the canonical
// metadata keys, so an adapter cannot redirect a combined snapshot read.
func (k TaggedKey) resolves(base EntryKey, keys []EntryKey) bool {
	if k.data == nil || k.data.base != base || len(k.data.stamps) != len(keys) {
		return false
	}
	for i, stamp := range k.data.stamps {
		if stamp.Key != keys[i] {
			return false
		}
	}
	return true
}
func (k TaggedKey) DataKey() EntryKey {
	if k.data == nil {
		return EntryKey{}
	}
	return k.data.key
}
func (k TaggedKey) FillKey() EntryKey {
	if k.data == nil {
		return EntryKey{}
	}
	return k.data.flight
}
func (k TaggedKey) Stamps() []TagStamp {
	if k.data == nil {
		return nil
	}
	return slices.Clone(k.data.stamps)
}
func (k TaggedKey) Fingerprint() [sha256.Size]byte {
	if k.data == nil {
		return [sha256.Size]byte{}
	}
	return k.data.fingerprint
}

// VersionFingerprint is the canonical concatenation of the snapshot's versions
// (TagVersionBytes each, in Stamps order). The data address already fixes the
// tag identities, so this identifies the snapshot exactly. Adapters whose
// authority cannot compute SHA-256 (such as Redis Lua) persist this instead of
// Fingerprint, which lets one atomic operation resolve metadata and compare.
func (k TaggedKey) VersionFingerprint() []byte {
	if k.data == nil {
		return nil
	}
	result := make([]byte, 0, len(k.data.stamps)*TagVersionBytes)
	for _, stamp := range k.data.stamps {
		result = append(result, stamp.Version.value[:]...)
	}
	return result
}
