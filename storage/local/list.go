package local

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/storage"
)

const directoryBatch = 128

// walk bounds directory reads, open descriptors and examined entries. The
// callback owns no descriptor after return. Deleted entries may disappear during
// scans; listings never claim snapshot isolation against concurrent mutations.
func (b *Backend) walk(ctx context.Context, visit func(*os.Root, string) error) error {
	scanned := 0
	for i := 0; i < 256; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		directory := fmt.Sprintf("objects/%02x", i)
		parent, err := b.root.OpenRoot(directory)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		err = func() error {
			defer parent.Close()
			reader, err := parent.Open(".")
			if err != nil {
				return err
			}
			defer reader.Close()
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				entries, readErr := reader.ReadDir(directoryBatch)
				for _, entry := range entries {
					scanned++
					if scanned > b.config.MaxScan {
						return storage.Failure(storage.LimitExceeded, storage.ListOperation, storage.NotApplicable, nil)
					}
					if err := visit(parent, entry.Name()); err != nil {
						return err
					}
				}
				if readErr != nil {
					if errors.Is(readErr, io.EOF) {
						return nil
					}
					return readErr
				}
			}
		}()
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

type listCursor struct {
	Store     string `json:"s"`
	Prefix    string `json:"p"`
	After     string `json:"a"`
	Delimited bool   `json:"d,omitempty"`
}

func (b *Backend) after(options storage.ListOptions) (string, error) {
	if options.Cursor.IsZero() {
		return "", nil
	}
	invalid := func() (string, error) {
		return "", failure(storage.Invalid, storage.ListOperation, storage.NotApplicable, nil)
	}
	raw, err := base64.RawURLEncoding.DecodeString(options.Cursor.Token())
	if err != nil {
		return invalid()
	}
	var cursor listCursor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cursor); err != nil {
		return invalid()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return invalid()
	}
	if cursor.Store != b.storeID || cursor.Prefix != options.Prefix.String() || cursor.Delimited != options.Delimited || !strings.HasPrefix(cursor.After, cursor.Prefix) {
		return invalid()
	}
	// A delimited page can end on a child prefix; every other cursor is a key.
	if cursor.Delimited && strings.HasSuffix(cursor.After, "/") {
		if _, err = storage.ParsePrefix(cursor.After); err != nil {
			return invalid()
		}
		return cursor.After, nil
	}
	if _, err = storage.ParseKey(cursor.After); err != nil {
		return invalid()
	}
	return cursor.After, nil
}
func (b *Backend) cursor(options storage.ListOptions, after string) storage.Cursor {
	raw, _ := json.Marshal(listCursor{Store: b.storeID, Prefix: options.Prefix.String(), After: after, Delimited: options.Delimited})
	return storage.NewCursor(base64.RawURLEncoding.EncodeToString(raw))
}

// entry is one listing item: an object, or a child prefix of a delimited page.
type entry struct {
	name      string
	object    storage.ObjectInfo
	directory bool
}
type candidates []entry

func (h candidates) Len() int           { return len(h) }
func (h candidates) Less(i, j int) bool { return h[i].name > h[j].name }
func (h candidates) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidates) Push(v any)        { *h = append(*h, v.(entry)) }
func (h *candidates) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = entry{}
	*h = old[:n-1]
	return item
}

// List scans the bounded store and keeps the smallest Limit+1 entries after
// the cursor. Unreadable or mismatched records are counted in Skipped rather
// than failing the page; staging and control files are never listed.
func (b *Backend) List(ctx context.Context, options storage.ListOptions) (storage.Page, error) {
	if err := b.ready(ctx, storage.ListOperation); err != nil {
		return storage.Page{}, err
	}
	if err := options.Validate(); err != nil {
		return storage.Page{}, err
	}
	after, err := b.after(options)
	if err != nil {
		return storage.Page{}, err
	}
	prefix := options.Prefix.String()
	selected := make(candidates, 0, options.Limit+1)
	queued := make(map[string]bool)
	skipped := 0
	err = b.walk(ctx, func(parent *os.Root, name string) error {
		if len(name) != 64 || strings.ToLower(name) != name {
			return nil
		}
		if _, err := hex.DecodeString(name); err != nil {
			return nil
		}
		file, err := parent.Open(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		info, _, err := b.readHeader(file)
		closeErr := file.Close()
		if closeErr != nil {
			return closeErr
		}
		if err != nil {
			if options.Prefix.String() == "" {
				skipped++
			}
			return nil
		}
		if _, expected := address(info.Key); name != expected {
			if options.Prefix.Contains(info.Key) {
				skipped++
			}
			return nil
		}
		if !options.Prefix.Contains(info.Key) {
			return nil
		}
		item := entry{name: info.Key.String(), object: info}
		if options.Delimited {
			if slash := strings.IndexByte(item.name[len(prefix):], '/'); slash >= 0 {
				item = entry{name: item.name[:len(prefix)+slash+1], directory: true}
			}
		}
		if item.name <= after || item.directory && queued[item.name] {
			return nil
		}
		if len(selected) < options.Limit+1 {
			heap.Push(&selected, item)
		} else if item.name < selected[0].name {
			if selected[0].directory {
				delete(queued, selected[0].name)
			}
			selected[0] = item
			heap.Fix(&selected, 0)
		} else {
			return nil
		}
		if item.directory {
			queued[item.name] = true
		}
		return nil
	})
	if err != nil {
		return storage.Page{}, failure(storage.Unavailable, storage.ListOperation, storage.NotApplicable, err)
	}
	slices.SortFunc(selected, func(a, b entry) int { return strings.Compare(a.name, b.name) })
	page := storage.Page{Skipped: skipped}
	if len(selected) > options.Limit {
		selected = selected[:options.Limit]
		page.Next = b.cursor(options, selected[len(selected)-1].name)
	}
	for _, item := range selected {
		if item.directory {
			directory, err := storage.ParsePrefix(item.name)
			if err != nil {
				return storage.Page{}, failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, err)
			}
			page.Directories = append(page.Directories, directory)
			continue
		}
		page.Objects = append(page.Objects, item.object)
	}
	return page, nil
}
