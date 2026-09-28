package i18n

import (
	"cmp"
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// Load reads locale/*.json from an explicit FS root (including embed.FS).
// Nested objects flatten with dots; plural leaves are {"$plural":{"one":...,
// "other":...}}. Duplicate keys/files, unknown messages/locales, symlinks,
// malformed leaves and excess resources fail construction atomically.
// Filesystem callbacks are isolated and awaited through actual return.
func Load(ctx context.Context, source fs.FS, locales LocaleCatalog, options CatalogOptions, definitions ...MessageDefinition) (*Catalog, error) {
	if ctx == nil || source == nil {
		return nil, invalidMessage()
	}
	set, err := SnapshotLocales(ctx, locales)
	if err != nil {
		return nil, err
	}
	var result *Catalog
	err = callback.Isolated("localization catalog load", func() error {
		messages := make(map[LocaleID]map[MessageKey]Template)
		entries, err := readCatalogDirectory(source, ".", MaxLocales)
		if err != nil {
			return invalidMessage()
		}
		if len(entries) > MaxLocales {
			return invalidMessage()
		}
		total, files := 0, 0
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			locale, err := ParseLocale(entry.Name())
			if err != nil || !entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !set.Contains(locale) {
				return invalidMessage()
			}
			if _, exists := messages[locale]; exists {
				return invalidMessage()
			}
			messages[locale] = make(map[MessageKey]Template)
			children, err := readCatalogDirectory(source, entry.Name(), 1024-files)
			if err != nil {
				return invalidMessage()
			}
			files += len(children)
			if files > 1024 {
				return invalidMessage()
			}
			for _, child := range children {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !child.Type().IsRegular() || !strings.HasSuffix(child.Name(), ".json") {
					return invalidMessage()
				}
				data, err := readCatalogFile(source, path.Join(entry.Name(), child.Name()))
				if err != nil {
					return err
				}
				total += len(data)
				if total > MaxCatalogBytes {
					return invalidMessage()
				}
				root, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: 1 << 20, Depth: 32, Nodes: MaxMessages * 4})
				if err != nil {
					return invalidMessage()
				}
				object, ok := root.(map[string]any)
				if !ok {
					return invalidMessage()
				}
				if err := flattenCatalog(messages[locale], "", object); err != nil {
					return err
				}
			}
		}
		result, err = NewCatalog(ctx, set, options, definitions, messages)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReadDir(n) bounds native directory materialization as well as the resulting
// catalog. FS implementations must expose directory handles implementing
// fs.ReadDirFile; this includes embed.FS, os.DirFS and os.Root.FS.
func readCatalogDirectory(source fs.FS, name string, maximum int) (entries []fs.DirEntry, err error) {
	file, err := source.Open(name)
	if err != nil {
		return nil, invalidMessage()
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			entries = nil
			err = errors.Join(err, invalidMessage())
		}
	}()
	directory, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, invalidMessage()
	}
	for {
		page, readErr := directory.ReadDir(maximum - len(entries) + 1)
		if len(page) > maximum-len(entries) {
			return nil, invalidMessage()
		}
		entries = append(entries, page...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || len(page) == 0 {
			return nil, invalidMessage()
		}
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
	for i, entry := range entries {
		if !fs.ValidPath(entry.Name()) || strings.Contains(entry.Name(), "/") || entry.Name() == "." || i > 0 && entry.Name() == entries[i-1].Name() {
			return nil, invalidMessage()
		}
	}
	return entries, nil
}

func readCatalogFile(source fs.FS, name string) (data []byte, err error) {
	f, err := source.Open(name)
	if err != nil {
		return nil, invalidMessage()
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			data = nil
			err = errors.Join(err, invalidMessage())
		}
	}()
	data, err = io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, invalidMessage()
	}
	return data, nil
}

func flattenCatalog(result map[MessageKey]Template, prefix string, object map[string]any) error {
	for name, value := range object {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		id := MessageKey(key)
		if id.Validate() != nil {
			return invalidMessage()
		}
		var template Template
		switch value := value.(type) {
		case string:
			template.Text = value
		case map[string]any:
			if forms, ok := value["$plural"]; ok {
				m, ok := forms.(map[string]any)
				if !ok || len(value) != 1 || len(m) > 6 {
					return invalidMessage()
				}
				template.Forms = make(map[PluralForm]string, len(m))
				for form, text := range m {
					s, ok := text.(string)
					if !ok {
						return invalidMessage()
					}
					template.Forms[PluralForm(form)] = s
				}
			} else {
				if err := flattenCatalog(result, key, value); err != nil {
					return err
				}
				continue
			}
		default:
			return invalidMessage()
		}
		if _, exists := result[id]; exists || len(result) >= MaxMessages {
			return invalidMessage()
		}
		result[id] = template
	}
	return nil
}
