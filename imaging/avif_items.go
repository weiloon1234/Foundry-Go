package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
)

type avifExtent struct{ offset, length uint64 }
type avifItem struct {
	id                   uint32
	kind                 string
	located              bool
	method               uint8
	base                 uint64
	extents              []avifExtent
	properties           []int
	width, height, units int
	parts                [][]byte
}
type avifProperty struct {
	kind string
	data []byte
}
type avifReference struct {
	kind string
	from uint32
	to   []uint32
}

func (c *avifContainer) item(id uint32) (*avifItem, error) {
	if item := c.items[id]; item != nil {
		return item, nil
	}
	if len(c.items) >= maxAVIFItems {
		return nil, limited()
	}
	item := &avifItem{id: id}
	c.items[id] = item
	return item, nil
}

func (c *avifContainer) parseLocations(data []byte) error {
	r := avifReader{data: data}
	version, flags := r.fullBox()
	if version > 2 || flags != 0 {
		return unsupported()
	}
	sizes := r.uint(2)
	offsetSize, lengthSize, baseSize, indexSize := int(sizes>>12), int(sizes>>8&15), int(sizes>>4&15), int(sizes&15)
	if version == 0 {
		indexSize = 0
	}
	for _, n := range []int{offsetSize, lengthSize, baseSize, indexSize} {
		if n != 0 && n != 4 && n != 8 {
			return unsupported()
		}
	}
	idSize := 2
	if version == 2 {
		idSize = 4
	}
	count := r.uint(idSize)
	if err := c.count(count); err != nil {
		return err
	}
	for range count {
		item, err := c.item(uint32(r.uint(idSize)))
		if err != nil {
			return err
		}
		if item.located {
			return unsupported()
		}
		item.located = true
		if version > 0 {
			method := r.uint(2)
			if method > 1 {
				return unsupported()
			}
			item.method = uint8(method)
		}
		if r.uint(2) != 0 {
			return unsupported()
		} // External data references are not image bytes supplied by the caller.
		item.base = r.uint(baseSize)
		n := r.uint(2)
		if err := c.count(n); err != nil {
			return err
		}
		for range n {
			r.uint(indexSize)
			item.extents = append(item.extents, avifExtent{r.uint(offsetSize), r.uint(lengthSize)})
		}
	}
	return r.finish()
}

func (c *avifContainer) parseItems(data []byte) error {
	r := avifReader{data: data}
	version, flags := r.fullBox()
	if version > 1 || flags != 0 {
		return unsupported()
	}
	n := 2
	if version == 1 {
		n = 4
	}
	count := r.uint(n)
	if err := c.count(count); err != nil {
		return err
	}
	if r.bad {
		return unsupported()
	}
	seen := uint64(0)
	err := c.walk(data[r.pos:], func(box avifBox) error {
		if box.kind != "infe" {
			return unsupported()
		}
		seen++
		r := avifReader{data: box.body}
		version, _ := r.fullBox()
		if version != 2 && version != 3 {
			return unsupported()
		}
		n := 2
		if version == 3 {
			n = 4
		}
		item, err := c.item(uint32(r.uint(n)))
		if err != nil {
			return err
		}
		if item.kind != "" || r.uint(2) != 0 {
			return unsupported()
		}
		item.kind = string(r.bytes(4))
		if r.bad {
			return unsupported()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if seen != count {
		return unsupported()
	}
	return nil
}

func (c *avifContainer) parseProperties(data []byte) error {
	return c.walk(data, func(box avifBox) error {
		if err := c.count(1); err != nil {
			return err
		}
		c.properties = append(c.properties, avifProperty{box.kind, box.body})
		if box.kind == "ispe" {
			if len(box.body) != 12 || binary.BigEndian.Uint32(box.body) != 0 {
				return unsupported()
			}
			return c.limits.dimensions(int(binary.BigEndian.Uint32(box.body[4:])), int(binary.BigEndian.Uint32(box.body[8:])))
		}
		return nil
	})
}

func (c *avifContainer) parseAssociations(data []byte) error {
	r := avifReader{data: data}
	version, flags := r.fullBox()
	if version > 1 || flags > 1 {
		return unsupported()
	}
	count := r.uint(4)
	if err := c.count(count); err != nil {
		return err
	}
	idSize := 2
	if version == 1 {
		idSize = 4
	}
	for range count {
		item, err := c.item(uint32(r.uint(idSize)))
		if err != nil {
			return err
		}
		n := r.uint(1)
		if err := c.count(n); err != nil {
			return err
		}
		for range n {
			width, mask := 1, uint64(127)
			if flags == 1 {
				width, mask = 2, 32767
			}
			index := r.uint(width) & mask
			if index > 0 {
				item.properties = append(item.properties, int(index)-1)
			}
		}
	}
	return r.finish()
}

func (c *avifContainer) parseReferences(data []byte) error {
	r := avifReader{data: data}
	version, flags := r.fullBox()
	if version > 1 || flags != 0 || r.bad {
		return unsupported()
	}
	idSize := 2
	if version == 1 {
		idSize = 4
	}
	return c.walk(data[r.pos:], func(box avifBox) error {
		r := avifReader{data: box.body}
		ref := avifReference{kind: box.kind, from: uint32(r.uint(idSize))}
		n := r.uint(2)
		if err := c.count(n); err != nil {
			return err
		}
		for range n {
			ref.to = append(ref.to, uint32(r.uint(idSize)))
		}
		if err := r.finish(); err != nil {
			return err
		}
		c.refs = append(c.refs, ref)
		return nil
	})
}

func (c *avifContainer) property(item *avifItem, kind string) []byte {
	if item == nil {
		return nil
	}
	for _, index := range item.properties {
		if index < len(c.properties) && c.properties[index].kind == kind {
			return c.properties[index].data
		}
	}
	return nil
}

func (c *avifContainer) itemParts(item *avifItem) ([][]byte, error) {
	data := c.data
	if item.method == 1 {
		data = c.idat
	}
	var parts [][]byte
	for _, extent := range item.extents {
		offset := item.base + extent.offset
		if offset < item.base || offset > uint64(len(data)) || extent.length > uint64(len(data))-offset || extent.length == 0 {
			return nil, unsupported()
		}
		c.encodedBytes += int64(extent.length)
		if c.encodedBytes > c.limits.InputBytes {
			return nil, limited()
		}
		parts = append(parts, data[int(offset):int(offset+extent.length)])
	}
	if len(parts) == 0 {
		return nil, unsupported()
	}
	return parts, nil
}

func (c *avifContainer) admitItems() error {
	for _, item := range c.items {
		for _, index := range item.properties {
			if index >= len(c.properties) {
				return unsupported()
			}
		}
		if item.kind != "av01" && item.kind != "grid" {
			continue
		}
		dimensions := c.property(item, "ispe")
		if len(dimensions) != 12 {
			return unsupported()
		}
		item.width, item.height = int(binary.BigEndian.Uint32(dimensions[4:])), int(binary.BigEndian.Uint32(dimensions[8:]))
		var err error
		item.parts, err = c.itemParts(item)
		if err != nil {
			return err
		}
		if item.kind == "av01" {
			item.units, err = avifFrameUnits(item.parts, c.limits.Frames)
			if err != nil {
				return err
			}
		}
	}
	primary := c.items[c.primary]
	if primary == nil || (primary.kind != "av01" && primary.kind != "grid") {
		return unsupported()
	}
	if err := c.accountItem(primary); err != nil {
		return err
	}
	if alpha := c.alphaFor(c.primary); alpha != nil {
		if err := c.accountItem(alpha); err != nil {
			return err
		}
	} else if primary.kind == "grid" {
		tiles, err := c.gridTiles(primary)
		if err != nil {
			return err
		}
		var alphas []*avifItem
		for _, tile := range tiles {
			alpha := c.alphaFor(tile.id)
			if alpha == nil {
				alphas = nil
				break
			}
			alphas = append(alphas, alpha)
		}
		if len(alphas) > 0 {
			c.gridPixels += int64(primary.width) * int64(primary.height)
			for _, alpha := range alphas {
				if alpha.kind != "av01" {
					return unsupported()
				}
				if err := c.accountItem(alpha); err != nil {
					return err
				}
			}
		}
	}
	if crop := c.property(primary, "clap"); crop != nil {
		var err error
		c.crop, err = avifCleanAperture(crop, primary.width, primary.height)
		if err != nil {
			return err
		}
	}
	angle, mirror := 0, 0
	if rotation := c.property(primary, "irot"); rotation != nil {
		if len(rotation) != 1 || rotation[0] > 3 {
			return unsupported()
		}
		angle = int(rotation[0])
	}
	if axis := c.property(primary, "imir"); axis != nil {
		if len(axis) != 1 || axis[0] > 1 {
			return unsupported()
		}
		mirror = int(axis[0]) + 1
	}
	// Counterclockwise quarter turns, then vertical/horizontal mirroring.
	orientations := [3][4]uint8{{1, 8, 3, 6}, {4, 5, 2, 7}, {2, 7, 4, 5}}
	c.orientation = orientations[mirror][angle]
	return nil
}

func (c *avifContainer) alphaFor(id uint32) *avifItem {
	for _, ref := range c.refs {
		if ref.kind != "auxl" || len(ref.to) == 0 || ref.to[0] != id {
			continue
		}
		item := c.items[ref.from]
		aux := c.property(item, "auxC")
		if len(aux) >= 5 {
			end := bytes.IndexByte(aux[4:], 0)
			if end >= 0 && string(aux[4:4+end]) == avifAlphaURN {
				return item
			}
		}
	}
	return nil
}

func (c *avifContainer) accountItem(item *avifItem) error {
	if item.kind == "av01" {
		c.maxFramePixels = max(c.maxFramePixels, int64(item.width)*int64(item.height))
		c.frameUnits += int64(item.units)
		return nil
	}
	if item.kind != "grid" {
		return unsupported()
	}
	tiles, err := c.gridTiles(item)
	if err != nil {
		return err
	}
	c.gridPixels += int64(item.width) * int64(item.height)
	for _, tile := range tiles {
		if err := c.accountItem(tile); err != nil {
			return err
		}
	}
	return nil
}

func (c *avifContainer) gridTiles(item *avifItem) ([]*avifItem, error) {
	reader := avifParts{parts: item.parts}
	var header [12]byte
	for i := range 4 {
		b, ok := reader.byte()
		if !ok {
			return nil, unsupported()
		}
		header[i] = b
	}
	if header[0] != 0 || header[1] > 1 {
		return nil, unsupported()
	}
	size := 8
	if header[1] == 1 {
		size = 12
	}
	for i := 4; i < size; i++ {
		b, ok := reader.byte()
		if !ok {
			return nil, unsupported()
		}
		header[i] = b
	}
	if !reader.empty() {
		return nil, unsupported()
	}
	w, h := int(binary.BigEndian.Uint16(header[4:6])), int(binary.BigEndian.Uint16(header[6:8]))
	if size == 12 {
		w, h = int(binary.BigEndian.Uint32(header[4:8])), int(binary.BigEndian.Uint32(header[8:12]))
	}
	// The grid descriptor controls a separate allocation; never trust only ispe.
	if w != item.width || h != item.height {
		return nil, unsupported()
	}
	for _, ref := range c.refs {
		if ref.kind != "dimg" || ref.from != item.id {
			continue
		}
		if len(ref.to) != (int(header[2])+1)*(int(header[3])+1) {
			return nil, unsupported()
		}
		tiles := make([]*avifItem, 0, len(ref.to))
		for _, id := range ref.to {
			tile := c.items[id]
			if tile == nil || tile.kind != "av01" {
				return nil, unsupported()
			}
			tiles = append(tiles, tile)
		}
		return tiles, nil
	}
	return nil, unsupported()
}

func avifCleanAperture(data []byte, w, h int) (image.Rectangle, error) {
	if len(data) != 32 {
		return image.Rectangle{}, unsupported()
	}
	var v [8]int64
	for i := range v {
		v[i] = int64(int32(binary.BigEndian.Uint32(data[i*4:])))
	}
	if v[1] <= 0 || v[3] <= 0 || v[5] <= 0 || v[7] <= 0 || v[0] <= 0 || v[2] <= 0 || v[0]%v[1] != 0 || v[2]%v[3] != 0 {
		return image.Rectangle{}, unsupported()
	}
	cw, ch := v[0]/v[1], v[2]/v[3]
	xn, yn := int64(w)*v[5]+2*v[4]-cw*v[5], int64(h)*v[7]+2*v[6]-ch*v[7]
	xd, yd := 2*v[5], 2*v[7]
	if xn%xd != 0 || yn%yd != 0 {
		return image.Rectangle{}, unsupported()
	}
	x, y := xn/xd, yn/yd
	if x < 0 || y < 0 || x+cw > int64(w) || y+ch > int64(h) {
		return image.Rectangle{}, unsupported()
	}
	return image.Rect(int(x), int(y), int(x+cw), int(y+ch)), nil
}
