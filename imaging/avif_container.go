package imaging

import (
	"encoding/binary"
	"image"
)

// These bounds cover metadata work independently of compressed file size.
const (
	maxAVIFBoxes          = 8192
	maxAVIFItems          = 1024
	maxAVIFEntries        = 8192
	maxAVIFTracks         = 16
	avifMetadataWorkspace = 2 << 20
)

type avifBox struct {
	kind string
	body []byte
}

type avifContainer struct {
	data, idat                                  []byte
	limits                                      Limits
	boxes, entries                              int
	items                                       map[uint32]*avifItem
	properties                                  []avifProperty
	refs                                        []avifReference
	primary                                     uint32
	tracks                                      []*avifTrack
	track                                       *avifTrack
	maxFramePixels, decodedPixels, encodedBytes int64
	frameUnits, gridPixels                      int64
	crop                                        image.Rectangle
	orientation                                 uint8
}

// walk validates box lengths, including 64-bit and extends-to-end lengths,
// before any codec parser can allocate arrays from their declared counts.
func (c *avifContainer) walk(data []byte, visit func(avifBox) error) error {
	for len(data) > 0 {
		c.boxes++
		if c.boxes > maxAVIFBoxes {
			return limited()
		}
		if len(data) < 8 {
			return unsupported()
		}
		n := uint64(binary.BigEndian.Uint32(data))
		header := 8
		if n == 1 {
			if len(data) < 16 {
				return unsupported()
			}
			n = binary.BigEndian.Uint64(data[8:])
			header = 16
		} else if n == 0 {
			n = uint64(len(data))
		}
		if n < uint64(header) || n > uint64(len(data)) {
			return unsupported()
		}
		if err := visit(avifBox{string(data[4:8]), data[header:int(n)]}); err != nil {
			return err
		}
		data = data[int(n):]
	}
	return nil
}

func (c *avifContainer) count(n uint64) error {
	if n > maxAVIFEntries || uint64(c.entries)+n > maxAVIFEntries {
		return limited()
	}
	c.entries += int(n)
	return nil
}

type avifReader struct {
	data []byte
	pos  int
	bad  bool
}

func (r *avifReader) bytes(n int) []byte {
	if n < 0 || n > len(r.data)-r.pos {
		r.bad = true
		return nil
	}
	result := r.data[r.pos : r.pos+n]
	r.pos += n
	return result
}
func (r *avifReader) uint(n int) uint64 {
	if n < 0 || n > 8 {
		r.bad = true
		return 0
	}
	var value uint64
	for _, b := range r.bytes(n) {
		value = value<<8 | uint64(b)
	}
	return value
}
func (r *avifReader) fullBox() (uint8, uint32) {
	return uint8(r.uint(1)), uint32(r.uint(3))
}
func (r *avifReader) finish() error {
	if r.bad || r.pos != len(r.data) {
		return unsupported()
	}
	return nil
}

func inspectAVIF(data []byte, l Limits) (*avifContainer, error) {
	c := &avifContainer{data: data, limits: l, items: make(map[uint32]*avifItem), orientation: 1}
	// The dependency parses metadata from reader-at buffers. Admit its copies
	// and bounded metadata tables before invoking that parser.
	if err := l.admit(int64(len(data)), 3*int64(len(data))+avifMetadataWorkspace); err != nil {
		return nil, err
	}
	brand, seenFTYP, meta, primary, movie := false, false, false, false, false
	err := c.walk(data, func(box avifBox) error {
		switch box.kind {
		case "ftyp":
			if seenFTYP || len(box.body) < 8 || len(box.body)%4 != 0 {
				return unsupported()
			}
			seenFTYP = true
			for i := 0; i < len(box.body); i += 4 {
				if i == 4 {
					continue
				}
				if string(box.body[i:i+4]) == "avif" || string(box.body[i:i+4]) == "avis" {
					brand = true
				}
			}
		case "meta":
			if meta || len(box.body) < 4 || binary.BigEndian.Uint32(box.body) != 0 {
				return unsupported()
			}
			meta = true
			return c.walk(box.body[4:], func(child avifBox) error {
				switch child.kind {
				case "pitm":
					if primary {
						return unsupported()
					}
					primary = true
					r := avifReader{data: child.body}
					v, flags := r.fullBox()
					if v > 1 || flags != 0 {
						return unsupported()
					}
					n := 2
					if v == 1 {
						n = 4
					}
					c.primary = uint32(r.uint(n))
					return r.finish()
				case "iloc":
					return c.parseLocations(child.body)
				case "iinf":
					return c.parseItems(child.body)
				case "iref":
					return c.parseReferences(child.body)
				case "idat":
					if c.idat != nil {
						return unsupported()
					}
					c.idat = child.body
				case "iprp":
					return c.walk(child.body, func(property avifBox) error {
						switch property.kind {
						case "ipco":
							return c.parseProperties(property.body)
						case "ipma":
							return c.parseAssociations(property.body)
						}
						return nil
					})
				}
				return nil
			})
		case "moov":
			if movie {
				return unsupported()
			}
			movie = true
			return c.parseMovie(box.body)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !brand || !meta || !primary {
		return nil, unsupported()
	}
	if err := c.admitItems(); err != nil {
		return nil, err
	}
	if err := c.admitTracks(); err != nil {
		return nil, err
	}
	if c.track != nil {
		if crop := c.property(c.items[c.primary], "clap"); crop != nil {
			var err error
			c.crop, err = avifCleanAperture(crop, c.track.width, c.track.height)
			if err != nil {
				return nil, err
			}
		}
	}
	c.decodedPixels = c.frameUnits*c.maxFramePixels + c.gridPixels
	if c.decodedPixels > l.Pixels {
		return nil, limited()
	}
	if err := l.admit(int64(len(data)), c.workspace()); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *avifContainer) workspace() int64 {
	// AV1 keeps reference pictures, tile/transform buffers and high-bit-depth
	// planes. Account for them in addition to every potentially shown picture,
	// reconstructed grids, metadata copies and gathered item/sample bytes.
	return c.maxFramePixels*128 + c.decodedPixels*16 + c.encodedBytes + 3*int64(len(c.data)) + avifMetadataWorkspace
}

// avifFrameUnits bounds reference/output pictures before AV1 pixel decoding.
// The dependency accepts low-overhead OBUs (a size on each OBU); count both
// standalone frame headers and complete frame OBUs, including hidden pictures.
func avifFrameUnits(parts [][]byte, maximum int) (int, error) {
	reader := avifParts{parts: parts}
	frames, obus := 0, 0
	for !reader.empty() {
		obus++
		if obus > maxAVIFEntries {
			return 0, limited()
		}
		header, ok := reader.byte()
		if !ok || header&0x81 != 0 || header&2 == 0 {
			return 0, unsupported()
		}
		if header&4 != 0 {
			if extension, ok := reader.byte(); !ok || extension&7 != 0 {
				return 0, unsupported()
			}
		}
		var size uint64
		terminated := false
		for i := 0; i < 8; i++ {
			b, ok := reader.byte()
			if !ok {
				return 0, unsupported()
			}
			size |= uint64(b&0x7f) << uint(i*7)
			if b&0x80 == 0 {
				terminated = true
				break
			}
		}
		if !terminated || !reader.skip(size) {
			return 0, unsupported()
		}
		kind := (header >> 3) & 15
		if kind == 3 || kind == 6 {
			frames++
			if frames > maximum {
				return 0, limited()
			}
		}
	}
	if frames == 0 {
		return 0, unsupported()
	}
	return frames, nil
}

type avifParts struct {
	parts        [][]byte
	part, offset int
}

func (p *avifParts) empty() bool {
	for p.part < len(p.parts) && p.offset == len(p.parts[p.part]) {
		p.part++
		p.offset = 0
	}
	return p.part == len(p.parts)
}
func (p *avifParts) byte() (byte, bool) {
	if p.empty() {
		return 0, false
	}
	b := p.parts[p.part][p.offset]
	p.offset++
	return b, true
}
func (p *avifParts) skip(n uint64) bool {
	for n > 0 {
		if p.empty() {
			return false
		}
		part := min(n, uint64(len(p.parts[p.part])-p.offset))
		p.offset += int(part)
		n -= part
	}
	return true
}
