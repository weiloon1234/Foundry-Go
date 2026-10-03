package imaging

import (
	"bytes"
	"encoding/binary"
)

const avifAlphaURN = "urn:mpeg:mpegB:cicp:systems:auxiliary:alpha"

type avifTrack struct {
	id                 uint32
	handler, auxiliary string
	timescale          uint32
	width, height      int
	samples            []avifExtent
	deltas             []uint32
	auxiliaryFor       []uint32
	duration, segment  uint64
	edits, repeating   bool
}

// singletonBoxes prevents ambiguous tables where the preflight and codec could
// otherwise select different declarations or append duplicate expanded arrays.
func (c *avifContainer) singletonBoxes(data []byte, visit func(avifBox) error) error {
	seen := make(map[string]bool)
	return c.walk(data, func(box avifBox) error {
		if seen[box.kind] {
			return unsupported()
		}
		seen[box.kind] = true
		return visit(box)
	})
}

func (c *avifContainer) parseMovie(data []byte) error {
	return c.walk(data, func(box avifBox) error {
		if box.kind != "trak" {
			return nil
		}
		if len(c.tracks) >= maxAVIFTracks {
			return limited()
		}
		track := &avifTrack{}
		c.tracks = append(c.tracks, track)
		return c.singletonBoxes(box.body, func(child avifBox) error {
			r := avifReader{data: child.body}
			switch child.kind {
			case "tkhd":
				version, _ := r.fullBox()
				if version > 1 {
					return unsupported()
				}
				width := 4
				if version == 1 {
					width = 8
				}
				r.bytes(width * 2)
				track.id = uint32(r.uint(4))
				r.bytes(4)
				track.duration = r.uint(width)
				if version == 0 && track.duration == 1<<32-1 {
					track.duration = ^uint64(0)
				}
			case "edts":
				track.edits = true
				return c.singletonBoxes(child.body, func(edit avifBox) error {
					if edit.kind != "elst" {
						return nil
					}
					r := avifReader{data: edit.body}
					version, flags := r.fullBox()
					// The decoder implements one ordinary/repeating segment, not
					// arbitrary timeline edits. Reject unsupported edits explicitly.
					if version > 1 || flags > 1 || r.uint(4) != 1 {
						return unsupported()
					}
					width := 4
					if version == 1 {
						width = 8
					}
					track.segment = r.uint(width)
					if r.uint(width) != 0 || r.uint(4) != 1<<16 {
						return unsupported()
					}
					track.repeating = flags == 1
					return r.finish()
				})
			case "tref":
				return c.singletonBoxes(child.body, func(ref avifBox) error {
					if ref.kind != "auxl" {
						return nil
					}
					if len(ref.body)%4 != 0 {
						return unsupported()
					}
					if err := c.count(uint64(len(ref.body) / 4)); err != nil {
						return err
					}
					for i := 0; i < len(ref.body); i += 4 {
						track.auxiliaryFor = append(track.auxiliaryFor, binary.BigEndian.Uint32(ref.body[i:]))
					}
					return nil
				})
			case "mdia":
				return c.parseMedia(track, child.body)
			}
			if r.bad {
				return unsupported()
			}
			return nil
		})
	})
}

func (c *avifContainer) parseMedia(track *avifTrack, data []byte) error {
	return c.singletonBoxes(data, func(box avifBox) error {
		r := avifReader{data: box.body}
		switch box.kind {
		case "mdhd":
			version, flags := r.fullBox()
			if version > 1 || flags != 0 {
				return unsupported()
			}
			width := 4
			if version == 1 {
				width = 8
			}
			r.bytes(width * 2)
			track.timescale = uint32(r.uint(4))
		case "hdlr":
			version, flags := r.fullBox()
			if version != 0 || flags != 0 {
				return unsupported()
			}
			r.bytes(4)
			track.handler = string(r.bytes(4))
		case "minf":
			return c.singletonBoxes(box.body, func(child avifBox) error {
				if child.kind == "stbl" {
					return c.parseSampleTable(track, child.body)
				}
				return nil
			})
		}
		if r.bad {
			return unsupported()
		}
		return nil
	})
}

type avifChunkRun struct{ first, count uint32 }

func (c *avifContainer) parseSampleTable(track *avifTrack, data []byte) error {
	var sizes []uint32
	var offsets []uint64
	var runs []avifChunkRun
	err := c.singletonBoxes(data, func(box avifBox) error {
		r := avifReader{data: box.body}
		switch box.kind {
		case "stts", "stsz", "stsc", "stco", "co64", "stsd":
			version, flags := r.fullBox()
			if version != 0 || flags != 0 {
				return unsupported()
			}
		default:
			// Composition offsets, alternate size tables and encryption need
			// codec support; do not silently interpret them as an ordinary track.
			if box.kind == "ctts" || box.kind == "stz2" || box.kind == "senc" {
				return unsupported()
			}
			return nil
		}
		uniform := uint32(0)
		if box.kind == "stsz" {
			uniform = uint32(r.uint(4))
		}
		count := r.uint(4)
		if err := c.count(count); err != nil {
			return err
		}
		if r.bad {
			return unsupported()
		}
		switch box.kind {
		case "stts":
			for range count {
				n, delta := r.uint(4), uint32(r.uint(4))
				if n == 0 || delta == 0 {
					return unsupported()
				}
				if n > uint64(c.limits.Frames-len(track.deltas)) {
					return limited()
				}
				if err := c.count(n); err != nil {
					return err
				}
				for range n {
					track.deltas = append(track.deltas, delta)
				}
			}
		case "stsz":
			if count > uint64(c.limits.Frames) {
				return limited()
			}
			for range count {
				size := uniform
				if size == 0 {
					size = uint32(r.uint(4))
				}
				if size == 0 {
					return unsupported()
				}
				sizes = append(sizes, size)
			}
		case "stco", "co64":
			if len(offsets) > 0 {
				return unsupported()
			}
			n := 4
			if box.kind == "co64" {
				n = 8
			}
			for range count {
				offsets = append(offsets, r.uint(n))
			}
		case "stsc":
			for range count {
				first, per := uint32(r.uint(4)), uint32(r.uint(4))
				if first == 0 || per == 0 || per > uint32(c.limits.Frames) || r.uint(4) != 1 {
					return unsupported()
				}
				if len(runs) == 0 && first != 1 || len(runs) > 0 && first <= runs[len(runs)-1].first {
					return unsupported()
				}
				runs = append(runs, avifChunkRun{first, per})
			}
		case "stsd":
			if count != 1 {
				return unsupported()
			}
			seen := false
			return c.walk(box.body[r.pos:], func(entry avifBox) error {
				if seen || entry.kind != "av01" || len(entry.body) < 78 {
					return unsupported()
				}
				seen = true
				track.width, track.height = int(binary.BigEndian.Uint16(entry.body[24:])), int(binary.BigEndian.Uint16(entry.body[26:]))
				if err := c.limits.dimensions(track.width, track.height); err != nil {
					return err
				}
				return c.singletonBoxes(entry.body[78:], func(property avifBox) error {
					if property.kind == "auxi" {
						if len(property.body) < 5 || binary.BigEndian.Uint32(property.body) != 0 {
							return unsupported()
						}
						end := bytes.IndexByte(property.body[4:], 0)
						if end < 0 {
							return unsupported()
						}
						track.auxiliary = string(property.body[4 : 4+end])
					}
					return nil
				})
			})
		}
		return r.finish()
	})
	if err != nil {
		return err
	}
	if len(sizes) == 0 {
		return nil
	}
	if len(offsets) == 0 || len(runs) == 0 || len(track.deltas) != len(sizes) {
		return unsupported()
	}
	sample, run := 0, 0
	for i, offset := range offsets {
		for run+1 < len(runs) && runs[run+1].first <= uint32(i+1) {
			run++
		}
		for range runs[run].count {
			if sample >= len(sizes) {
				return unsupported()
			}
			n := uint64(sizes[sample])
			sample++
			if offset > uint64(len(c.data)) || n > uint64(len(c.data))-offset {
				return unsupported()
			}
			track.samples = append(track.samples, avifExtent{offset, n})
			offset += n
		}
	}
	if sample != len(sizes) || int(runs[len(runs)-1].first) > len(offsets) {
		return unsupported()
	}
	return nil
}

func (c *avifContainer) admitTracks() error {
	seen := make(map[uint32]bool)
	for _, track := range c.tracks {
		if track.id == 0 || seen[track.id] {
			return unsupported()
		}
		seen[track.id] = true
		if c.track == nil && track.handler == "pict" && len(track.samples) > 0 {
			c.track = track
		}
	}
	if len(c.tracks) > 0 && c.track == nil {
		return unsupported()
	}
	if c.track == nil {
		return nil
	}
	if err := c.accountTrack(c.track); err != nil {
		return err
	}
	for _, track := range c.tracks {
		if track.handler != "auxv" || track.auxiliary != avifAlphaURN || len(track.samples) == 0 {
			continue
		}
		for _, id := range track.auxiliaryFor {
			if id != c.track.id {
				continue
			}
			if track.width != c.track.width || track.height != c.track.height || len(track.samples) != len(c.track.samples) {
				return unsupported()
			}
			if err := c.accountTrack(track); err != nil {
				return err
			}
			return nil // The decoder selects the first matching alpha track.
		}
	}
	return nil
}

func (c *avifContainer) accountTrack(track *avifTrack) error {
	if track.timescale == 0 || track.width == 0 || track.height == 0 {
		return unsupported()
	}
	c.maxFramePixels = max(c.maxFramePixels, int64(track.width)*int64(track.height))
	for _, sample := range track.samples {
		parts := [][]byte{c.data[int(sample.offset):int(sample.offset+sample.length)]}
		units, err := avifFrameUnits(parts, c.limits.Frames)
		if err != nil {
			return err
		}
		c.frameUnits += int64(units)
		c.encodedBytes += int64(sample.length)
		if c.encodedBytes > c.limits.InputBytes {
			return limited()
		}
	}
	return nil
}

func (t *avifTrack) plays() (uint32, error) {
	if !t.edits {
		return 0, nil
	}
	if !t.repeating {
		return 1, nil
	}
	if t.duration == ^uint64(0) || t.duration == 0 || t.segment == 0 {
		return 0, nil
	}
	n := t.duration / t.segment
	if t.duration%t.segment != 0 {
		n++
	}
	if n > 1<<31 {
		return 0, unsupported()
	}
	return uint32(max(1, n)), nil
}
