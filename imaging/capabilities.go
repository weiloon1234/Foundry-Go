package imaging

import "slices"

// FormatCapability distinguishes reading, writing and animation support.
// A recognized format need not support every operation.
type FormatCapability struct {
	Format                        Format
	Read, Write                   bool
	ReadAnimation, WriteAnimation bool
	// WriteMetadata reports a native writer with verified ICC round-trip support.
	// Other metadata families are retained where that format supports them.
	WriteMetadata bool
}

// Capabilities is an owned snapshot of this engine's available functionality.
type Capabilities struct {
	Backend              Backend
	Formats              []FormatCapability
	ColorManagement      bool
	MetadataPreservation bool
	SmartCrop            bool
}

func (c Capabilities) ForFormat(format Format) (FormatCapability, bool) {
	for _, capability := range c.Formats {
		if capability.Format == format {
			return capability, true
		}
	}
	return FormatCapability{}, false
}

func (e *Engine) Capabilities() Capabilities {
	if e.Validate() != nil {
		return Capabilities{}
	}
	result := e.capabilities
	result.Formats = slices.Clone(result.Formats)
	return result
}

func portableFormats() [8]FormatCapability {
	return [8]FormatCapability{
		{Format: JPEG, Read: true, Write: true},
		{Format: PNG, Read: true, Write: true, ReadAnimation: true, WriteAnimation: true},
		{Format: WebP, Read: true, Write: true, ReadAnimation: true, WriteAnimation: true},
		{Format: GIF, Read: true, Write: true, ReadAnimation: true, WriteAnimation: true},
		{Format: BMP, Read: true, Write: true},
		{Format: TIFF, Read: true, Write: true},
		{Format: AVIF, Read: true, Write: true, ReadAnimation: true},
		{Format: ICO, Read: true, Write: true},
	}
}

func portableFormat(f Format) (FormatCapability, bool) {
	for _, capability := range portableFormats() {
		if capability.Format == f {
			return capability, true
		}
	}
	return FormatCapability{}, false
}

func (f Format) animationEncoding() bool {
	capability, ok := portableFormat(f)
	return ok && capability.WriteAnimation
}
