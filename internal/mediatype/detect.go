// Package mediatype sniffs media types from bounded in-memory content.
// Detection classifies bytes; it never validates or sanitizes a format.
package mediatype

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime"
	"net/http"
	"strings"
)

const (
	Binary = "application/octet-stream"
	ZIP    = "application/zip"
	SVG    = "image/svg+xml"
)

// maxZipEntries bounds central-directory inspection.
const maxZipEntries = 65535

// Detect returns the media type of data. It extends net/http sniffing with
// types whose generic signature hides the specific format: OOXML documents and
// ODF/EPUB packages (ZIP containers), SVG (XML or text), and ISO base media
// brands (AVIF, HEIC/HEIF, MP4, QuickTime, 3GPP, M4A). declared is a client
// hint that may only specialize generic plain text to a compatible textual
// subtype (CSV, TSV, Markdown, calendar, or JSON that parses); it never
// overrides a binary detection or chooses a type for bytes alone.
func Detect(data []byte, declared string) string {
	if kind := isoMedia(data); kind != "" {
		return kind
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		if kind := zipContainer(data); kind != "" {
			return kind
		}
	}
	sniffed, _, err := mime.ParseMediaType(http.DetectContentType(data))
	if err != nil {
		return Binary
	}
	if (sniffed == "text/xml" || sniffed == "text/plain") && svg(data) {
		return SVG
	}
	if sniffed == "text/plain" {
		if specialized := textual(data, declared); specialized != "" {
			return specialized
		}
	}
	return sniffed
}

func textual(data []byte, declared string) string {
	parsed, _, err := mime.ParseMediaType(declared)
	if err != nil {
		return ""
	}
	switch parsed {
	case "text/csv", "text/tab-separated-values", "text/markdown", "text/calendar":
		return parsed
	case "application/json":
		if json.Valid(data) {
			return parsed
		}
	}
	return ""
}

// isoMedia classifies an ISO base media file by its leading ftyp box brands.
func isoMedia(data []byte) string {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return ""
	}
	size := int(binary.BigEndian.Uint32(data[:4]))
	if size < 16 || size > len(data) || size > 4096 || size%4 != 0 {
		return ""
	}
	brands := map[string]bool{string(data[8:12]): true}
	for at := 16; at+4 <= size; at += 4 {
		brands[string(data[at:at+4])] = true
	}
	has := func(names ...string) bool {
		for _, name := range names {
			if brands[name] {
				return true
			}
		}
		return false
	}
	switch {
	case has("avif", "avis"):
		return "image/avif"
	case has("heic", "heix", "heim", "heis", "hevc", "hevx"):
		return "image/heic"
	case has("mif1", "msf1"):
		return "image/heif"
	case has("qt  "):
		return "video/quicktime"
	case has("M4A "):
		return "audio/mp4"
	case has("3g2a", "3g2b", "3g2c"):
		return "video/3gpp2"
	case has("3gp4", "3gp5", "3gp6", "3gg6"):
		return "video/3gpp"
	case has("isom", "iso2", "iso4", "iso5", "iso6", "mp41", "mp42", "avc1", "dash", "M4V ", "mmp4", "MSNV", "f4v "):
		return "video/mp4"
	}
	return ""
}

// zipContainer recognizes ODF/EPUB packages by their leading stored mimetype
// entry and OOXML packages by their central-directory part names.
func zipContainer(data []byte) string {
	if len(data) >= 30 && binary.LittleEndian.Uint16(data[8:10]) == 0 {
		nameLength, extraLength := int(binary.LittleEndian.Uint16(data[26:28])), int(binary.LittleEndian.Uint16(data[28:30]))
		stored := int(binary.LittleEndian.Uint32(data[18:22]))
		start := 30 + nameLength + extraLength
		if 30+nameLength <= len(data) && string(data[30:30+nameLength]) == "mimetype" && stored > 0 && stored <= 128 && start+stored <= len(data) {
			declared := string(data[start : start+stored])
			if parsed, parameters, err := mime.ParseMediaType(declared); err == nil && len(parameters) == 0 && parsed == declared && (strings.HasPrefix(parsed, "application/vnd.oasis.opendocument.") || parsed == "application/epub+zip") {
				return parsed
			}
		}
	}
	names, ok := centralDirectory(data)
	if !ok {
		return ""
	}
	if !names["[Content_Types].xml"] {
		return ZIP
	}
	switch {
	case names["word/document.xml"] && names["word/vbaProject.bin"]:
		return "application/vnd.ms-word.document.macroEnabled.12"
	case names["word/document.xml"]:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case names["xl/workbook.xml"] && names["xl/vbaProject.bin"]:
		return "application/vnd.ms-excel.sheet.macroEnabled.12"
	case names["xl/workbook.xml"]:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case names["ppt/presentation.xml"] && names["ppt/vbaProject.bin"]:
		return "application/vnd.ms-powerpoint.presentation.macroEnabled.12"
	case names["ppt/presentation.xml"]:
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	return ZIP
}

// centralDirectory returns the set of recognized part names from a bounded
// scan of a (non-ZIP64) archive's central directory.
func centralDirectory(data []byte) (map[string]bool, bool) {
	const endLength = 22
	if len(data) < endLength {
		return nil, false
	}
	floor := max(0, len(data)-endLength-0xffff)
	end := -1
	for at := len(data) - endLength; at >= floor; at-- {
		if string(data[at:at+4]) == "PK\x05\x06" {
			end = at
			break
		}
	}
	if end < 0 {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint16(data[end+10 : end+12]))
	size := int(binary.LittleEndian.Uint32(data[end+12 : end+16]))
	offset := int(binary.LittleEndian.Uint32(data[end+16 : end+20]))
	if count > maxZipEntries || offset < 0 || size < 0 || offset > end || size > end-offset {
		return nil, false
	}
	interesting := map[string]bool{"[Content_Types].xml": true, "word/document.xml": true, "word/vbaProject.bin": true, "xl/workbook.xml": true, "xl/vbaProject.bin": true, "ppt/presentation.xml": true, "ppt/vbaProject.bin": true}
	names := make(map[string]bool)
	at := offset
	for range count {
		if at+46 > offset+size || string(data[at:at+4]) != "PK\x01\x02" {
			return nil, false
		}
		nameLength := int(binary.LittleEndian.Uint16(data[at+28 : at+30]))
		extraLength := int(binary.LittleEndian.Uint16(data[at+30 : at+32]))
		commentLength := int(binary.LittleEndian.Uint16(data[at+32 : at+34]))
		next := at + 46 + nameLength + extraLength + commentLength
		if next > offset+size {
			return nil, false
		}
		if name := string(data[at+46 : at+46+nameLength]); interesting[name] {
			names[name] = true
		}
		at = next
	}
	return names, true
}

// svg reports an XML/text document whose root element is <svg>, after an
// optional byte-order mark, XML declaration, comments, processing
// instructions and doctype within the first 16 KiB.
func svg(data []byte) bool {
	text := data[:min(len(data), 16<<10)]
	text = bytes.TrimPrefix(text, []byte("\xef\xbb\xbf"))
	for {
		text = bytes.TrimLeft(text, " \t\r\n")
		switch {
		case bytes.HasPrefix(text, []byte("<?")):
			end := bytes.Index(text, []byte("?>"))
			if end < 0 {
				return false
			}
			text = text[end+2:]
		case bytes.HasPrefix(text, []byte("<!--")):
			end := bytes.Index(text, []byte("-->"))
			if end < 0 {
				return false
			}
			text = text[end+3:]
		case len(text) >= 9 && strings.EqualFold(string(text[:9]), "<!DOCTYPE"):
			end := bytes.IndexByte(text, '>')
			if subset := bytes.IndexByte(text, '['); subset >= 0 && (end < 0 || subset < end) {
				end = bytes.Index(text, []byte("]>"))
				if end >= 0 {
					end++
				}
			}
			if end < 0 {
				return false
			}
			text = text[end+1:]
		default:
			return len(text) > 4 && string(text[:4]) == "<svg" && strings.IndexByte(" \t\r\n>/", text[4]) >= 0
		}
	}
}
