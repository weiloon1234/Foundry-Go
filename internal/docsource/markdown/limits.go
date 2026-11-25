package markdown

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Parser cost is not linear in document size for pathological input, so every
// document is shaped before it is parsed. The limits are far above any
// repository document: the longest line is 7 KB and no line starts with more
// than three container markers.
const (
	// MaxLineBytes bounds one source line; inline parsing cost grows with line length.
	MaxLineBytes = 16 << 10
	// MaxContainerMarkers bounds the quote markers and list bullets that may
	// open one source line; block nesting depth grows with that count.
	MaxContainerMarkers = 16
)

// DocumentLimit is the one size rule for a repository-relative Markdown path.
// Every documentation command applies it to every document it reads: dated
// historical records (docs/evidence/** and CHANGELOG.md) may reach
// MaxRecordBytes and every other document, including all teaching documents
// and everything an application checks, MaxDocumentBytes.
func DocumentLimit(name string) int64 {
	name = path.Clean(filepath.ToSlash(name))
	if name == "CHANGELOG.md" || strings.HasPrefix(name, "docs/evidence/") {
		return MaxRecordBytes
	}
	return MaxDocumentBytes
}

// checkShape refuses a document before parsing, naming the first offending line.
func checkShape(name string, data []byte) error {
	if limit := DocumentLimit(name); int64(len(data)) > limit {
		return safeerror.New(fmt.Sprintf("%s:0: document exceeds maximum %d bytes; split the document.", name, limit))
	}
	for number, line := 1, 0; line <= len(data); number++ {
		end := bytes.IndexByte(data[line:], '\n')
		next := len(data) + 1
		text := data[line:]
		if end >= 0 {
			end += line
			next = end + 1
			// A CR directly before LF is one CRLF ending, not a lone CR.
			text = bytes.TrimSuffix(data[line:end], []byte{'\r'})
		}
		if at := bytes.IndexByte(text, '\r'); at >= 0 {
			return safeerror.New(fmt.Sprintf("%s:%d: document uses CR-only line endings; convert them to LF or CRLF.", name, number))
		}
		if len(text) > MaxLineBytes {
			return safeerror.New(fmt.Sprintf("%s:%d: line exceeds maximum %d bytes; break the line.", name, number, MaxLineBytes))
		}
		if leadingMarkers(text) > MaxContainerMarkers {
			return safeerror.New(fmt.Sprintf("%s:%d: line opens with more than %d quote or list markers; flatten the nesting.", name, number, MaxContainerMarkers))
		}
		line = next
	}
	return nil
}

// leadingMarkers counts quote markers and list bullets that open a line.
func leadingMarkers(line []byte) int {
	count := 0
	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == ' ' || c == '\t':
			i++
		case c == '>':
			count++
			i++
		case (c == '-' || c == '*') && thematicBreak(line[i:]):
			// CommonMark reads this as one thematic break, never as nested items.
			return count
		case (c == '-' || c == '*' || c == '+') && (i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t'):
			count++
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(line) && j-i < 10 && line[j] >= '0' && line[j] <= '9' {
				j++
			}
			if j < len(line) && (line[j] == '.' || line[j] == ')') && (j+1 == len(line) || line[j+1] == ' ' || line[j+1] == '\t') {
				count++
				i = j + 1
				continue
			}
			return count
		default:
			return count
		}
	}
	return count
}

// thematicBreak reports whether rest is three or more of its first byte
// (a hyphen or an asterisk) separated only by spaces and tabs.
func thematicBreak(rest []byte) bool {
	count := 0
	for _, c := range rest {
		switch c {
		case rest[0]:
			count++
		case ' ', '\t':
		default:
			return false
		}
	}
	return count >= 3
}

// Cache holds the parse of each document read in one run, so the binding,
// retirement, reference and link checks of a document share one parse. It is
// owned by a single run and bounded like the run's other retained inputs. A
// nil Cache parses every call.
type Cache struct {
	entries map[string]cached
	bytes   int
}

type cached struct {
	data     []byte
	document Document
}

// NewCache returns an empty run-local cache.
func NewCache() *Cache { return &Cache{entries: make(map[string]cached)} }

// Read returns the shared parse of name when data is the same content, and
// parses (and retains, within the run bounds) otherwise. Refusals are not cached.
func (c *Cache) Read(name string, data []byte) (Document, error) {
	if c == nil {
		return Read(name, data)
	}
	if entry, ok := c.entries[name]; ok && bytes.Equal(entry.data, data) {
		return entry.document, nil
	}
	document, err := Read(name, data)
	if err != nil {
		return Document{}, err
	}
	if _, replacing := c.entries[name]; !replacing && len(c.entries) < MaxDocuments && c.bytes+len(data) <= MaxRunBytes {
		c.entries[name] = cached{data: data, document: document}
		c.bytes += len(data)
	}
	return document, nil
}

// Budget bounds the documents one command run reads, whichever walk finds them.
type Budget struct {
	documents int
	bytes     int64
}

// Take accounts for one more document of size bytes.
func (b *Budget) Take(name string, size int) error {
	b.documents++
	b.bytes += int64(size)
	if b.documents > MaxDocuments {
		return safeerror.New(fmt.Sprintf("%s:0: too many documents; maximum is %d.", name, MaxDocuments))
	}
	if b.bytes > MaxRunBytes {
		return safeerror.New(fmt.Sprintf("%s:0: documents exceed maximum %d bytes per run; split the documentation run.", name, MaxRunBytes))
	}
	return nil
}
