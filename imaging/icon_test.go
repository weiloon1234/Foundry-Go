package imaging

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func insertPNGChunk(input []byte, kind string, body []byte) []byte {
	chunk := make([]byte, 12+len(body))
	binary.BigEndian.PutUint32(chunk, uint32(len(body)))
	copy(chunk[4:], kind)
	copy(chunk[8:], body)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	result := append([]byte{}, input[:33]...)
	result = append(result, chunk...)
	return append(result, input[33:]...)
}
func iconPNG(input []byte, width, height int) []byte {
	header := make([]byte, 22)
	binary.LittleEndian.PutUint16(header[2:], 1)
	binary.LittleEndian.PutUint16(header[4:], 1)
	header[6], header[7] = byte(width%256), byte(height%256)
	binary.LittleEndian.PutUint16(header[10:], 1)
	binary.LittleEndian.PutUint16(header[12:], 32)
	binary.LittleEndian.PutUint32(header[14:], uint32(len(input)))
	binary.LittleEndian.PutUint32(header[18:], 22)
	return append(header, input...)
}
func TestIconEmbeddedPNGValidatesMetadataAndRejectsAnimation(t *testing.T) {
	exif := make([]byte, 26)
	copy(exif, []byte("II\x2a\x00"))
	binary.LittleEndian.PutUint32(exif[4:], 8)
	binary.LittleEndian.PutUint16(exif[8:], 1)
	binary.LittleEndian.PutUint16(exif[10:], 274)
	binary.LittleEndian.PutUint16(exif[12:], 3)
	binary.LittleEndian.PutUint32(exif[14:], 1)
	binary.LittleEndian.PutUint16(exif[18:], 6)
	embedded := insertPNGChunk(pngInput(t, 3, 2), "eXIf", exif)
	icon := iconPNG(embedded, 3, 2)
	info, err := Inspect(icon, DefaultLimits())
	if err != nil || info.Orientation != 6 {
		t.Fatal("embedded orientation", err)
	}
	e := testEngine(t, DefaultConfig())
	result, err := e.ProcessBytes(t.Context(), icon, NewPlan().Format(PNG))
	if err != nil || result.Info().Width != 2 || result.Info().Height != 3 || bytes.Contains(result.Bytes(), []byte("eXIf")) {
		t.Fatal("icon orientation or metadata normalization", err)
	}
	animation := make([]byte, 8)
	binary.BigEndian.PutUint32(animation, 2)
	animated := insertPNGChunk(pngInput(t, 3, 2), "acTL", animation)
	for _, invalid := range [][]byte{iconPNG(animated, 3, 2), iconPNG(embedded[:len(embedded)-1], 3, 2), iconPNG(insertPNGChunk(embedded, "eXIf", exif), 3, 2)} {
		if _, err := Inspect(invalid, DefaultLimits()); err == nil {
			t.Fatal("invalid embedded container accepted")
		}
	}
	corrupt := append([]byte{}, icon...)
	corrupt[22+33+8+4] ^= 1
	if _, err := Inspect(corrupt, DefaultLimits()); err == nil {
		t.Fatal("bad metadata CRC accepted")
	}
}
