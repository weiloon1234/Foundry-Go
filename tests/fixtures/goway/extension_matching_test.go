package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	e "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func TestExtensionMatchingUsesBoundedManagerQueries(t *testing.T) {
	capture := &goldenCapture{}
	db := capture.open(t.Context())
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	registry := must(extensions.NewRegistry(e.Owner.Registration()))
	store := must(extensions.New(db, registry, extensions.DefaultConfig()))
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	locales := must(i18n.NewLocaleSet("en", "en", "ms"))
	textSlot := translations.TextOf(e.Owner, "title", translations.Options{}, extensions.BindSlot(e.Articles.Key(), e.Articles.Title))
	field := translations.FieldOf(textSlot)
	texts := must(translations.New(store, locales, textSlot.Registration()))
	valueSlot := metadata.ValueOf(e.Owner, "seo", metadata.Spec[string]{}, contract.StringJSON[string](), extensions.BindSlot(e.Articles.Key(), e.Articles.SEO))
	key := metadata.KeyOf(valueSlot)
	meta := must(metadata.New(store, valueSlot.Registration()))
	diskDeclaration := storage.DefineDisk("files")
	backend := must(local.Open(t.Context(), local.DefaultConfig(t.TempDir())))
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	disk := must(diskDeclaration.Bind(backend, storage.DefaultConfig()))
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	collection := attachments.Define(e.Owner, "files", attachments.Policy{Disk: diskDeclaration, Localized: true, Cardinality: attachments.Multiple, Accepted: []storage.MediaType{"text/plain"}})
	oneSlot := attachments.OneOf(e.Owner, "cover", attachments.Policy{Disk: diskDeclaration, Accepted: []storage.MediaType{"text/plain"}}, extensions.BindSlot(e.Articles.Key(), e.Articles.Cover))
	manySlot := attachments.ManyOf(e.Owner, "gallery", attachments.Policy{Disk: diskDeclaration, Accepted: []storage.MediaType{"text/plain"}}, extensions.BindSlot(e.Articles.Key(), e.Articles.Gallery))
	files := must(attachments.New(attachments.Dependencies{Store: store, Disks: must(storage.NewRegistry(disk)), Locales: locales}, attachments.DefaultConfig(), collection.Registration(), oneSlot.Registration(), manySlot.Registration()))
	t.Cleanup(func() {
		if err := files.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	// Extraction must retain the registration identity, including after binding.
	t.Run("extracted-collection-registration", func(t *testing.T) {
		capture.reply = func(string, []driver.NamedValue) ([]string, [][]driver.Value) {
			return make([]string, 11), [][]driver.Value{}
		}
		for _, declaration := range []attachments.Collection[e.Article, int64]{attachments.OneCollection(oneSlot), attachments.OneCollection(oneSlot.From(files)), attachments.ManyCollection(manySlot), attachments.ManyCollection(manySlot.From(files))} {
			if _, err := attachments.Matching(t.Context(), files, declaration); err != nil {
				t.Fatal("extracted collection lost its registration", err)
			}
		}
	})
	for _, test := range []struct {
		name    string
		current func(context.Context) (orm.Predicate[e.Article], error)
	}{
		{"translation", func(ctx context.Context) (orm.Predicate[e.Article], error) {
			return translations.Matching(ctx, texts, field, "en", "private-text")
		}},
		{"metadata", func(ctx context.Context) (orm.Predicate[e.Article], error) {
			return metadata.Matching(ctx, meta, key, "private-json")
		}},
		{"attachment", func(ctx context.Context) (orm.Predicate[e.Article], error) {
			return attachments.Matching(ctx, files, collection.ForLocale("en"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, mode := range []string{"found", "empty", "corrupt", "overflow"} {
				t.Run(mode, func(t *testing.T) {
					closed := 0
					capture.onClose = func() { closed++ }
					capture.reply = func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
						if !strings.Contains(sql, "LIMIT") {
							t.Fatal("unbounded matching query", sql)
						}
						count := 2
						if mode == "empty" {
							count = 0
						}
						if mode == "overflow" {
							count = int(args[len(args)-1].Value.(int64))
						}
						width := 7
						if test.name == "attachment" {
							width = 11
						}
						result := make([][]driver.Value, 0, count)
						for i := range count {
							// Repeated file owners must collapse to one key. Other rows are unique.
							id := int64(i + 1)
							if test.name == "attachment" {
								id = 1
							}
							ref := extensions.ORMIdentity(e.Owner).Reference(id)
							identity := must(ref.Identity())
							wire := must(identity.MarshalJSON())
							subject := must(e.Owner.SubjectKey(ref))
							scope := e.Owner.Scope()
							if mode == "corrupt" {
								subject = "corrupt"
							}
							switch test.name {
							case "translation":
								result = append(result, []driver.Value{extensions.Digest(scope, subject, "title", "en"), "articles", scope, subject, wire, "title", "en"})
							case "metadata":
								result = append(result, []driver.Value{extensions.Digest(scope, subject, "seo"), "articles", scope, subject, wire, "seo", int64(1)})
							case "attachment":
								result = append(result, []driver.Value{orderID, "articles", scope, subject, wire, "files", "en", string(attachments.Ready), "files", "foundry-attachments/" + scope + "/" + orderID, time.Now().UTC()})
							}
						}
						return make([]string, width), result
					}
					capture.output.Reset()
					before := capture.statements
					current, err := test.current(t.Context())
					currentSQL := capture.output.String()
					currentStatements := capture.statements - before

					if !oracleMatch(t, "goway/extension_matching/manager", currentSQL) || currentStatements != 2 {
						t.Fatal("matching SQL, binds or statement count changed")
					}
					if !matchingOracle.Match(t, "matching_errors", oracleSnapshot([]bool{err == nil, errors.Is(err, fault.Invalid), errors.Is(err, fault.Conflict), errors.Is(err, context.Canceled)})) {
						t.Fatal("matching error classification changed")
					}
					if (mode == "corrupt" || mode == "overflow") && err == nil {
						t.Fatal("invalid match accepted")
					}
					if closed != 1 {
						t.Fatal("matching rows were not closed", closed)
					}
					if err != nil {
						return
					}
					next := must(orm.Compile(e.Articles.Query().Where(orm.KeyEq(e.Articles.Key(), 99), current)))

					if !oracleMatch(t, "goway/extension_matching/predicate", oracleSnapshot([]any{next.SQL(), next.Arguments()})) {
						t.Fatal("owner predicate changed or lost caller scope")
					}
				})
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			before := capture.statements
			if _, err := test.current(ctx); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled matching", err)
			}
			if capture.statements != before {
				t.Fatal("cancelled matching dispatched SQL")
			}
		})
	}
}

func TestExtensionMatchingRejectsInvalidDeclarationsBeforeSQL(t *testing.T) {
	ctx := t.Context()
	if _, err := attachments.Matching(ctx, nil, attachments.Collection[e.Article, int64]{}); err == nil {
		t.Fatal("zero collection accepted")
	}
	if _, err := translations.Matching(ctx, nil, translations.Field[e.Article, int64]{}, "en", "x"); err == nil {
		t.Fatal("zero field accepted")
	}
	if _, err := metadata.Matching(ctx, nil, metadata.Key[e.Article, int64, string]{}, "x"); err == nil {
		t.Fatal("zero key accepted")
	}
	identityError := (orm.ModelIdentity[e.Article, int64]{}).Validate().Error()
	invalid := extensions.DefineORMOwner("articles", orm.ModelIdentity[e.Article, int64]{}, extensions.OwnerOptions{})
	if _, err := translations.Matching(ctx, nil, translations.Define(invalid, "title", translations.Options{}), "en", "x"); err == nil || err.Error() != identityError {
		t.Fatal("invalid native identity was not rejected by ORM identity validation", err)
	}
	if _, err := metadata.Matching(ctx, nil, metadata.Define(invalid, "seo", 1, contract.StringJSON[string]()), "x"); err == nil || err.Error() != identityError {
		t.Fatal("invalid native identity was not rejected by ORM identity validation", err)
	}
	if _, err := attachments.Matching(ctx, nil, attachments.Define(invalid, "files", attachments.Policy{Disk: storage.DefineDisk("files"), Cardinality: attachments.Single, Accepted: []storage.MediaType{"text/plain"}})); err == nil || err.Error() != identityError {
		t.Fatal("invalid native identity was not rejected by ORM identity validation", err)
	}
}

func TestZeroExtensionDescriptorExtractionIsInvalid(t *testing.T) {
	for _, err := range []error{
		translations.FieldOf(translations.TextHandle[e.Article, int64]{}).Validate(),
		metadata.KeyOf(metadata.ValueHandle[e.Article, int64, string]{}).Validate(),
		attachments.OneCollection(attachments.OneHandle[e.Article, int64]{}).Validate(),
		attachments.ManyCollection(attachments.ManyHandle[e.Article, int64]{}).Validate(),
	} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatal("zero descriptor extraction succeeded", err)
		}
	}
}
