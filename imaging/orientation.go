package imaging

import (
	"bytes"
	"encoding/binary"
)

func jpegOrientation(data []byte) (uint8, error) {
	for p := 2; p < len(data); {
		if data[p] != 0xff {
			return 0, unsupported()
		}
		for p < len(data) && data[p] == 0xff {
			p++
		}
		if p == len(data) {
			return 0, unsupported()
		}
		marker := data[p]
		p++
		if marker == 0xda || marker == 0xd9 {
			return 1, nil
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if len(data)-p < 2 {
			return 0, unsupported()
		}
		n := int(binary.BigEndian.Uint16(data[p:]))
		if n < 2 || n > len(data)-p {
			return 0, unsupported()
		}
		body := data[p+2 : p+n]
		if marker == 0xe1 && bytes.HasPrefix(body, []byte("Exif\x00\x00")) {
			return exifOrientation(body)
		}
		p += n
	}
	return 0, unsupported()
}

func tiffOrder(data []byte) (binary.ByteOrder, uint32, error) {
	if len(data) < 8 {
		return nil, 0, unsupported()
	}
	var order binary.ByteOrder
	switch string(data[:4]) {
	case "II\x2a\x00":
		order = binary.LittleEndian
	case "MM\x00\x2a":
		order = binary.BigEndian
	default:
		return nil, 0, unsupported()
	}
	return order, order.Uint32(data[4:]), nil
}

// Only the first IFD's orientation is read. No thumbnail, GPS pointer, nested
// metadata tree, or attacker-controlled allocation is followed.
func exifOrientation(data []byte) (uint8, error) {
	data = bytes.TrimPrefix(data, []byte("Exif\x00\x00"))
	order, offset, err := tiffOrder(data)
	if err != nil {
		return 0, err
	}
	entries, _, err := tiffIFD(data, order, offset)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if order.Uint16(entry) != 274 {
			continue
		}
		if order.Uint16(entry[2:]) != 3 || order.Uint32(entry[4:]) != 1 {
			return 0, unsupported()
		}
		n := order.Uint16(entry[8:])
		if n < 1 || n > 8 {
			return 0, unsupported()
		}
		return uint8(n), nil
	}
	return 1, nil
}
func tiffIFD(data []byte, order binary.ByteOrder, offset uint32) ([][]byte, uint32, error) {
	if offset < 8 || uint64(offset)+2 > uint64(len(data)) {
		return nil, 0, unsupported()
	}
	p := int(offset)
	count := int(order.Uint16(data[p:]))
	p += 2
	if count > 2048 || count*12+4 > len(data)-p {
		return nil, 0, unsupported()
	}
	entries := make([][]byte, count)
	for i := range entries {
		entries[i] = data[p+i*12 : p+(i+1)*12]
	}
	return entries, order.Uint32(data[p+count*12:]), nil
}
func tiffScalar(entry []byte, order binary.ByteOrder) (int, error) {
	if order.Uint32(entry[4:]) != 1 {
		return 0, unsupported()
	}
	switch order.Uint16(entry[2:]) {
	case 3:
		return int(order.Uint16(entry[8:])), nil
	case 4:
		return int(order.Uint32(entry[8:])), nil
	default:
		return 0, unsupported()
	}
}
func tiffDetails(data []byte, l Limits) (int, uint8, error) {
	order, offset, err := tiffOrder(data)
	if err != nil {
		return 0, 0, err
	}
	seen := make(map[uint32]bool)
	orientation := uint8(1)
	var pixels int64
	for offset != 0 {
		if seen[offset] || len(seen) >= l.Frames {
			return 0, 0, limited()
		}
		seen[offset] = true
		entries, next, err := tiffIFD(data, order, offset)
		if err != nil {
			return 0, 0, err
		}
		w, h := 0, 0
		for _, e := range entries {
			switch order.Uint16(e) {
			case 256:
				w, err = tiffScalar(e, order)
			case 257:
				h, err = tiffScalar(e, order)
			case 274:
				if len(seen) == 1 {
					var o int
					o, err = tiffScalar(e, order)
					if o < 1 || o > 8 {
						return 0, 0, unsupported()
					}
					orientation = uint8(o)
				}
			}
			if err != nil {
				return 0, 0, err
			}
		}
		if err := l.dimensions(w, h); err != nil {
			return 0, 0, err
		}
		pixels += int64(w) * int64(h)
		if pixels > l.Pixels {
			return 0, 0, limited()
		}
		offset = next
	}
	if len(seen) == 0 {
		return 0, 0, unsupported()
	}
	return len(seen), orientation, nil
}
