package imaging

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"strings"
)

// Only explicitly supported families can reach native loaders. There is no
// filename, generic ImageMagick, PDF, remote URL or arbitrary-operation path.
func nativeInputFormat(data []byte) Format {
	if bytes.HasPrefix(data, []byte{0xff, 0x0a}) || bytes.HasPrefix(data, []byte("\x00\x00\x00\x0cJXL \r\n\x87\n")) {
		return JPEGXL
	}
	if bytes.HasPrefix(data, []byte{0xff, 0x4f, 0xff, 0x51}) || bytes.HasPrefix(data, []byte("\x00\x00\x00\x0cjP  \r\n\x87\n")) {
		return JPEG2000
	}
	if len(data) >= 16 && string(data[4:8]) == "ftyp" {
		n := int(binary.BigEndian.Uint32(data[:4]))
		if n < 16 || n > len(data) || n > 65536 || n%4 != 0 {
			return ""
		}
		heif := false
		for at := 8; at < n; at += 4 {
			if at == 12 {
				continue
			}
			switch string(data[at : at+4]) {
			case "avif", "avis":
				return "" // Portable AVIF keeps its stricter container admission.
			case "heic", "heix", "hevc", "hevx", "mif1", "msf1":
				heif = true
			}
		}
		if heif {
			return HEIF
		}
	}
	prefix := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if len(prefix) > 0 && prefix[0] == '<' {
		return SVG
	}
	return ""
}

// SVG rasterization accepts a bounded, self-contained document. Embedded raster
// images and external resources would bypass image admission, so callers insert
// those through ordinary, separately admitted image layers instead.
func validateNativeSVG(data []byte) error {
	if len(data) > 10<<20 {
		return limited()
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	depth, nodes, styleDepth := 0, 0, 0
	var style strings.Builder
	root := false
	for {
		token, err := d.Token()
		if err == io.EOF {
			if root && depth == 0 {
				return nil
			}
			return unsupported()
		}
		if err != nil {
			return unsupported()
		}
		switch v := token.(type) {
		case xml.Directive:
			return invalid("SVG declarations and external entities are unsupported")
		case xml.ProcInst:
			if v.Target != "xml" {
				return unsupported()
			}
		case xml.StartElement:
			if styleDepth > 0 {
				return unsupported()
			}
			nodes++
			depth++
			if nodes > 65536 || depth > 128 {
				return limited()
			}
			if !root {
				if v.Name.Local != "svg" || v.Name.Space != "" && v.Name.Space != "http://www.w3.org/2000/svg" {
					return unsupported()
				}
				root = true
			} else if depth == 1 {
				return unsupported()
			}
			if v.Name.Local == "style" {
				styleDepth = depth
				style.Reset()
			}
			switch strings.ToLower(v.Name.Local) {
			case "image", "script", "foreignobject":
				return invalid("SVG embedded or active resources are unsupported")
			}
			for _, a := range v.Attr {
				name := strings.ToLower(a.Name.Local)
				if name == "base" || strings.HasPrefix(name, "on") {
					return unsupported()
				}
				if (name == "href" || name == "src") && !strings.HasPrefix(strings.TrimSpace(a.Value), "#") {
					return invalid("SVG external resources are unsupported")
				}
				// CSS escapes can spell url without containing the literal token.
				if (name == "style" || strings.Contains(strings.ToLower(a.Value), "url(") || strings.Contains(a.Value, "\\")) && !localSVGReferences(a.Value) {
					return invalid("SVG external styles are unsupported")
				}
			}
		case xml.EndElement:
			if depth == styleDepth {
				if !localSVGReferences(style.String()) {
					return invalid("SVG external styles are unsupported")
				}
				styleDepth = 0
			}
			depth--
		case xml.CharData:
			if styleDepth > 0 {
				style.Write(v)
			}
		}
	}
}

func localSVGReferences(value string) bool {
	v := strings.ToLower(value)
	if strings.ContainsAny(v, "\\@") {
		return false
	}
	for {
		at := strings.Index(v, "url(")
		if at < 0 {
			return true
		}
		v = strings.TrimSpace(v[at+4:])
		v = strings.TrimLeft(v, "\"'")
		if !strings.HasPrefix(v, "#") {
			return false
		}
		end := strings.IndexByte(v, ')')
		if end < 0 {
			return false
		}
		v = v[end+1:]
	}
}

// Some native loaders discard malformed profiles with a warning. Remember
// explicit profile declarations so color/preservation requests cannot silently
// treat such input as unprofiled. Other native containers retain their raw ICC
// blob for the conversion-time compatibility check.
func declaresColorProfile(data []byte, format Format) bool {
	switch format {
	case PNG:
		for at := 8; at+12 <= len(data); {
			n := uint64(binary.BigEndian.Uint32(data[at:]))
			if n > uint64(len(data)-at-12) {
				return false
			}
			if string(data[at+4:at+8]) == "iCCP" {
				return true
			}
			at += int(n) + 12
		}
	case JPEG:
		for at := 2; at+4 <= len(data); {
			if data[at] != 0xff {
				return false
			}
			for at < len(data) && data[at] == 0xff {
				at++
			}
			if at >= len(data) {
				return false
			}
			marker := data[at]
			at++
			if marker == 0xda || marker == 0xd9 {
				return false
			}
			if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
				continue
			}
			if at+2 > len(data) {
				return false
			}
			n := int(binary.BigEndian.Uint16(data[at:]))
			if n < 2 || n > len(data)-at {
				return false
			}
			if marker == 0xe2 && bytes.HasPrefix(data[at+2:at+n], []byte("ICC_PROFILE\x00")) {
				return true
			}
			at += n
		}
	}
	return false
}
