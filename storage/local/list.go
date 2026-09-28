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
	Store  string `json:"s"`
	Prefix string `json:"p"`
	After  string `json:"a"`
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
	key, err := storage.ParseKey(cursor.After)
	if err != nil || cursor.Store != b.storeID || cursor.Prefix != options.Prefix.String() || !options.Prefix.Contains(key) {
		return invalid()
	}
	return cursor.After, nil
}
func (b *Backend) cursor(prefix storage.Prefix, after storage.ObjectKey) storage.Cursor {
	raw, _ := json.Marshal(listCursor{Store: b.storeID, Prefix: prefix.String(), After: after.String()})
	return storage.NewCursor(base64.RawURLEncoding.EncodeToString(raw))
}

type candidates []storage.ObjectInfo

func (h candidates) Len() int           { return len(h) }
func (h candidates) Less(i, j int) bool { return h[i].Key.String() > h[j].Key.String() }
func (h candidates) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidates) Push(v any)        { *h = append(*h, v.(storage.ObjectInfo)) }
func (h *candidates) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = storage.ObjectInfo{}
	*h = old[:n-1]
	return item
}
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
	selected := make(candidates, 0, options.Limit+1)
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
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
		_, expected := address(info.Key)
		if name != expected {
			return failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
		}
		if info.Key.String() <= after || !options.Prefix.Contains(info.Key) {
			return nil
		}
		if len(selected) < options.Limit+1 {
			heap.Push(&selected, info)
		} else if info.Key.String() < selected[0].Key.String() {
			selected[0] = info
			heap.Fix(&selected, 0)
		}
		return nil
	})
	if err != nil {
		return storage.Page{}, failure(storage.Unavailable, storage.ListOperation, storage.NotApplicable, err)
	}
	slices.SortFunc(selected, func(a, b storage.ObjectInfo) int { return strings.Compare(a.Key.String(), b.Key.String()) })
	page := storage.Page{Objects: []storage.ObjectInfo(selected)}
	if len(page.Objects) > options.Limit {
		page.Objects = page.Objects[:options.Limit]
		page.Next = b.cursor(options.Prefix, page.Objects[len(page.Objects)-1].Key)
	}
	return page, nil
}
