package validation

import "strings"

// FileKind correlates one normalized filename extension with one exact detected
// MIME type. Multiple declarations may share an extension with different types.
// Recognition is not full document validation.
type FileKind struct {
	// Extension is the final filename extension without a leading dot.
	Extension string `json:"extension"`
	// ContentType is the exact MIME type reported by the upload detector.
	ContentType string `json:"content_type"`
}

// PDFKind recognizes a PDF signature under the pdf extension.
func PDFKind() FileKind { return FileKind{Extension: "pdf", ContentType: "application/pdf"} }

// TextKind recognizes detected plain text under the txt extension.
func TextKind() FileKind { return FileKind{Extension: "txt", ContentType: "text/plain"} }

// CSVKind recognizes plain text under the csv extension; it does not parse CSV.
func CSVKind() FileKind { return FileKind{Extension: "csv", ContentType: "text/plain"} }

// ZIPContainerKind recognizes a ZIP signature under the chosen extension, such
// as zip, docx, xlsx or pptx. It does not validate ZIP entries or Office packages.
func ZIPContainerKind(extension string) FileKind {
	return FileKind{Extension: extension, ContentType: "application/zip"}
}

// FileKinds matches extension and detected MIME against the same declaration.
// MIME parameters are ignored; extensions are case insensitive and have no dot.
// Absent files reject. Use Optional for omitted values and FileMinSize for empty
// uploads. The server-only leaf supports WithMessage and WithTranslation; its
// kinds parameter also supplies browser extension hints through the manifest.
func FileKinds[F FileValue](allowed ...FileKind) Rule[F] {
	kinds, err := normalizeFileKinds(allowed, false)
	if err != nil {
		return failed[F](err)
	}
	return valueRule(Spec{ID: "foundry.file_kinds", Parameters: []Parameter{parameter("kinds", kinds)}}, true, func(s *execution, file F) (bool, error) {
		if file.IsZero() {
			return false, nil
		}
		extension, media := file.Extension(), file.ContentType()
		if !textValue(s, extension) || !textValue(s, media) {
			return false, nil
		}
		extension, ok := fileExtension(extension)
		if !ok {
			return false, nil
		}
		media, ok = fileMediaType(media, true)
		if !ok {
			return false, nil
		}
		for _, kind := range kinds {
			if kind.Extension == extension && kind.ContentType == media {
				return true, nil
			}
		}
		return false, nil
	})
}

func normalizeFileKinds(allowed []FileKind, allowUnsniffed bool) ([]FileKind, error) {
	if len(allowed) == 0 || len(allowed) >= maxRuleNodes {
		return nil, invalid("file validation requires a bounded nonempty kind list")
	}
	owned := make([]FileKind, len(allowed))
	seen := make(map[FileKind]bool, len(allowed))
	for i, kind := range allowed {
		extension, ok := fileExtension(kind.Extension)
		if !ok || !validText(kind.Extension, false) {
			return nil, invalid("invalid file kind extension")
		}
		media := ""
		if kind.ContentType != "" || !allowUnsniffed {
			media, ok = fileMediaType(kind.ContentType, false)
			if !ok || !validText(kind.ContentType, false) || strings.Contains(media, "*") {
				return nil, invalid("file kinds require exact detected content types")
			}
		}
		owned[i] = FileKind{Extension: extension, ContentType: media}
		if seen[owned[i]] {
			return nil, invalid("duplicate file kind")
		}
		seen[owned[i]] = true
	}
	return owned, nil
}
