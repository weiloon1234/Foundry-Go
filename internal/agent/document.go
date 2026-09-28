package agent

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const maxDocumentBytes = 2 << 20

// Position uses LSP's zero-based UTF-16 coordinates, not byte or visual columns.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type document struct {
	workspace, path, uri, text string
	position                   Position
}

func prepare(options Options) (document, error) {
	var result document
	root, err := filepath.Abs(options.Workspace)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return result, fmt.Errorf("workspace must be an existing directory")
	}
	path := options.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return result, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return result, fmt.Errorf("context file must be inside the consumer workspace")
	}
	if filepath.Ext(path) != ".go" {
		return result, fmt.Errorf("context file must be an existing Go source file")
	}
	info, err = os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return result, fmt.Errorf("context file must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return result, fmt.Errorf("context file must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return result, err
	}
	if len(data) > maxDocumentBytes || len(options.Insert) > maxDocumentBytes-len(data) {
		return result, fmt.Errorf("context document exceeds size limit")
	}
	if !utf8.Valid(data) || !utf8.ValidString(options.Insert) {
		return result, fmt.Errorf("context document and insertion must be UTF-8")
	}
	offset, err := byteOffset(string(data), options.Line, options.Column)
	if err != nil {
		return result, err
	}
	text := string(data[:offset]) + options.Insert + string(data[offset:])
	return document{root, path, fileURI(path), text, lspPosition(text[:offset+len(options.Insert)])}, nil
}

// CLI coordinates are one-based line and UTF-8 byte column, matching Go source
// diagnostics. CRLF counts as one newline; a column cannot split a UTF-8 rune.
func byteOffset(text string, line, column int) (int, error) {
	if line < 1 || column < 1 {
		return 0, fmt.Errorf("line and column must be positive")
	}
	start := 0
	for current := 1; current < line; current++ {
		next := strings.IndexByte(text[start:], '\n')
		if next < 0 {
			return 0, fmt.Errorf("line is outside the document")
		}
		start += next + 1
	}
	end := len(text)
	if next := strings.IndexByte(text[start:], '\n'); next >= 0 {
		end = start + next
	}
	if end > start && text[end-1] == '\r' {
		end--
	}
	if column-1 > end-start {
		return 0, fmt.Errorf("column is outside the line")
	}
	offset := start + column - 1
	if !utf8.ValidString(text[start:offset]) {
		return 0, fmt.Errorf("column splits a UTF-8 character")
	}
	return offset, nil
}
func lspPosition(prefix string) Position {
	line := strings.Count(prefix, "\n")
	start := strings.LastIndexByte(prefix, '\n') + 1
	units := 0
	for _, r := range prefix[start:] {
		units += utf16.RuneLen(r)
	}
	return Position{line, units}
}
func fileURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
