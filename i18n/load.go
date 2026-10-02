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

// Catalog directory bounds. Raw directory listings are bounded separately from
// catalog files so ignored entries cannot force unbounded materialization.
const (
	MaxCatalogFiles        = 1024
	maxDirectoryEntries    = 4096
	maxCatalogFileBytes    = 1 << 20
	catalogFileExtension   = ".json"
	catalogDirectoryReason = "the catalog directory cannot be enumerated within its bounds"
)

// Load reads locale/*.json from an explicit FS root (including embed.FS).
// Nested objects flatten with dots; plural leaves are {"$plural":{"one":...,
// "other":...}}. Dot entries (.DS_Store, .git, .gitkeep) at any level and
// non-.json regular files are ignored; root .json files, nested directories,
// symlinks, duplicate keys/files, unknown messages/locales, malformed leaves and
// excess resources fail construction atomically. Every failure names its
// locale, file and key where known. Filesystem callbacks are isolated and
// awaited through actual return.
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
		messages, sources, err := readTemplates(ctx, source, set)
		if err != nil {
			return err
		}
		result, err = newCatalog(ctx, set, options, definitions, messages, sources)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReadTemplates reads a catalog tree with Load's layout, limits and rules but
// without message definitions, so an application can declare its own keys
// before NewCatalog compiles the templates against them. Locale directories must
// belong to locales; duplicate JSON names, flattened keys and keys repeated
// across files fail with fault.Invalid naming the locale, file and key. Plural
// leaves become Template.Forms; their forms are checked when NewCatalog compiles
// them against a declaration. The result is owned by the caller.
func ReadTemplates(ctx context.Context, source fs.FS, locales LocaleCatalog) (map[LocaleID]map[MessageKey]Template, error) {
	if ctx == nil || source == nil {
		return nil, invalidMessage()
	}
	set, err := SnapshotLocales(ctx, locales)
	if err != nil {
		return nil, err
	}
	var result map[LocaleID]map[MessageKey]Template
	err = callback.Isolated("localization catalog read", func() error {
		var err error
		result, _, err = readTemplates(ctx, source, set)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// readTemplates flattens every locale file and records which file supplied
// each key, so later compilation errors can name it.
func readTemplates(ctx context.Context, source fs.FS, set LocaleSet) (map[LocaleID]map[MessageKey]Template, messageSources, error) {
	messages := make(map[LocaleID]map[MessageKey]Template)
	sources := make(messageSources)
	directories := make(map[LocaleID]string)
	entries, err := readCatalogDirectory(source, ".", maxDirectoryEntries)
	if err != nil {
		return nil, nil, catalogError("", ".", "", catalogDirectoryReason)
	}
	total, files := 0, 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		name := entry.Name()
		if ignoredCatalogEntry(name) {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil, nil, catalogError("", name, "", "symlinks are not followed")
		}
		if !entry.IsDir() {
			if entry.Type().IsRegular() && !strings.HasSuffix(name, catalogFileExtension) {
				continue
			}
			return nil, nil, catalogError("", name, "", "catalog files belong in locale directories such as en/messages.json")
		}
		locale, err := ParseLocale(name)
		if err != nil {
			return nil, nil, catalogError("", name, "", "the directory name is not a BCP 47 locale")
		}
		if !set.Contains(locale) {
			return nil, nil, catalogError(locale, name, "", "the locale is not in the supported locale set")
		}
		if previous, exists := directories[locale]; exists {
			return nil, nil, catalogError(locale, name, "", "the locale directory duplicates "+boundedQuote(previous))
		}
		if len(directories) >= MaxLocales {
			return nil, nil, catalogError(locale, name, "", "more than 64 locale directories")
		}
		directories[locale] = name
		messages[locale] = make(map[MessageKey]Template)
		sources[locale] = make(map[MessageKey]string)
		children, err := readCatalogDirectory(source, name, maxDirectoryEntries)
		if err != nil {
			return nil, nil, catalogError(locale, name, "", catalogDirectoryReason)
		}
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			file := path.Join(name, child.Name())
			if ignoredCatalogEntry(child.Name()) {
				continue
			}
			if child.Type()&fs.ModeSymlink != 0 {
				return nil, nil, catalogError(locale, file, "", "symlinks are not followed")
			}
			if !child.Type().IsRegular() {
				return nil, nil, catalogError(locale, file, "", "nested directories and special files are not supported")
			}
			if !strings.HasSuffix(child.Name(), catalogFileExtension) {
				continue
			}
			files++
			if files > MaxCatalogFiles {
				return nil, nil, catalogError(locale, file, "", "more than 1,024 catalog files")
			}
			data, err := readCatalogFile(source, file)
			if err != nil {
				return nil, nil, catalogError(locale, file, "", "the file cannot be read or exceeds 1 MiB")
			}
			total += len(data)
			if total > MaxCatalogBytes {
				return nil, nil, catalogError(locale, file, "", "catalog files exceed 16 MiB in total")
			}
			root, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: maxCatalogFileBytes, Depth: 32, Nodes: MaxMessages * 4})
			if err != nil {
				return nil, nil, catalogError(locale, file, "", "the file is not one bounded JSON object without duplicate names")
			}
			object, ok := root.(map[string]any)
			if !ok {
				return nil, nil, catalogError(locale, file, "", "the file root must be a JSON object")
			}
			flat := catalogFile{locale: locale, file: file, messages: messages[locale], sources: sources[locale]}
			if err := flat.flatten("", object); err != nil {
				return nil, nil, err
			}
		}
	}
	return messages, sources, nil
}

// ignoredCatalogEntry skips editor, VCS and operating-system metadata such as
// .DS_Store, .git and .gitkeep without opening it.
func ignoredCatalogEntry(name string) bool { return strings.HasPrefix(name, ".") }

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
	data, err = io.ReadAll(io.LimitReader(f, maxCatalogFileBytes+1))
	if err != nil || len(data) > maxCatalogFileBytes {
		return nil, invalidMessage()
	}
	return data, nil
}

// catalogFile flattens one locale file while recording which file supplied
// each key, so duplicates and later compilation errors can name both.
type catalogFile struct {
	locale   LocaleID
	file     string
	messages map[MessageKey]Template
	sources  map[MessageKey]string
}

func (c catalogFile) flatten(prefix string, object map[string]any) error {
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		value := object[name]
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		id := MessageKey(key)
		if id.Validate() != nil {
			return catalogError(c.locale, c.file, key, "the flattened key is not a semantic message key")
		}
		var template Template
		switch value := value.(type) {
		case string:
			template.Text = value
		case map[string]any:
			if forms, ok := value["$plural"]; ok {
				m, ok := forms.(map[string]any)
				if !ok || len(value) != 1 || len(m) > 6 {
					return catalogError(c.locale, c.file, key, "a $plural leaf must be the only member and contain at most six string forms")
				}
				template.Forms = make(map[PluralForm]string, len(m))
				for form, text := range m {
					s, ok := text.(string)
					if !ok {
						return catalogError(c.locale, c.file, key, "plural form "+boundedQuote(form)+" must be a string")
					}
					template.Forms[PluralForm(form)] = s
				}
			} else {
				if err := c.flatten(key, value); err != nil {
					return err
				}
				continue
			}
		default:
			return catalogError(c.locale, c.file, key, "a leaf must be a string or a $plural object")
		}
		if previous, exists := c.sources[id]; exists {
			return catalogError(c.locale, c.file, key, "the key is already defined in "+boundedQuote(previous))
		}
		if len(c.messages) >= MaxMessages {
			return catalogError(c.locale, c.file, key, "more than 10,000 messages for this locale")
		}
		c.messages[id] = template
		c.sources[id] = c.file
	}
	return nil
}
