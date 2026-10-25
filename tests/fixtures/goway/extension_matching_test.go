package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	e "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	oldID := query.NewExactField[e.Article]("extension_articles", "id", codec.Signed[int64]())
	oldQuery := query.ForModel(query.Define[e.Article]("extension_articles", "id", []query.Column{{Name: "id"}}, func(database.Row) (e.Article, error) { panic("owner query must only compile") }, query.NewModelField("id", codec.Signed[int64](), func(a e.Article) int64 { return a.ID })))
	for _, test := range []struct {
		name    string
		current func(context.Context) (orm.Predicate[e.Article], error)
		legacy  func(context.Context) (query.Predicate[e.Article], error)
	}{
		{"translation", func(ctx context.Context) (orm.Predicate[e.Article], error) {
			return translations.Matching(ctx, texts, field, "en", "private-text")
		}, func(ctx context.Context) (query.Predicate[e.Article], error) {
			return field.Matching(ctx, texts, "en", "private-text")
		}},
		{"metadata", func(ctx context.Context) (orm.Predicate[e.Article], error) {
			return metadata.Matching(ctx, meta, key, "private-json")
		}, func(ctx context.Context) (query.Predicate[e.Article], error) {
			return key.Matching(ctx, meta, "private-json")
		}},
		{"attachment", func(ctx context.Context) (orm.Predicate[e.Article], error) {
			return attachments.Matching(ctx, files, collection.ForLocale("en"))
		}, func(ctx context.Context) (query.Predicate[e.Article], error) {
			return collection.ForLocale("en").Matching(ctx, files)
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
							ref := e.Owner.Reference(id)
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
					capture.output.Reset()
					before = capture.statements
					prior, oldErr := test.legacy(t.Context())
					if !oracleEqual(t, "goway/extension_matching/manager", capture.output.String(), currentSQL) || currentStatements != capture.statements-before || currentStatements != 2 {
						t.Fatal("matching SQL, binds or statement count changed", currentSQL, capture.output.String())
					}
					if (err == nil) != (oldErr == nil) || (mode == "corrupt" || mode == "overflow") && err == nil {
						t.Fatal("matching error parity", err, oldErr)
					}
					for _, kind := range []error{fault.Invalid, fault.Conflict, context.Canceled} {
						if errors.Is(err, kind) != errors.Is(oldErr, kind) {
							t.Fatal("matching error classification changed", err, oldErr)
						}
					}
					if closed != 2 {
						t.Fatal("matching rows were not closed", closed)
					}
					if err != nil {
						return
					}
					next := must(orm.Compile(e.Articles.Query().Where(e.Articles.Key().Eq(99), current)))
					old := must(oldQuery.Where(oldID.Eq(99), prior).Compile())
					if !oracleEqual(t, "goway/extension_matching/predicate", oracleSnapshot([]any{old.SQL(), old.Arguments()}), oracleSnapshot([]any{next.SQL(), next.Arguments()})) || next.SQL() != old.SQL() || !reflect.DeepEqual(next.Arguments(), old.Arguments()) {
						t.Fatal("owner predicate changed or lost caller scope", next.SQL(), old.SQL())
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
	legacyQuery := query.ForModel(query.Define[e.Article]("extension_articles", "id", []query.Column{{Name: "id"}}, func(database.Row) (e.Article, error) { panic("no SQL") }, query.NewModelField("id", codec.Signed[int64](), func(a e.Article) int64 { return a.ID })))
	identityError := (orm.ModelIdentity[e.Article, int64]{}).Validate().Error()
	legacy := extensions.DefineOwner("articles", query.IdentityOf(legacyQuery, query.NewExactField[e.Article]("extension_articles", "id", codec.Signed[int64]())))
	if _, err := translations.Matching(ctx, nil, translations.Define(legacy, "title", translations.Options{}), "en", "x"); err == nil || err.Error() != identityError {
		t.Fatal("legacy source was not rejected by ORM identity validation", err)
	}
	if _, err := metadata.Matching(ctx, nil, metadata.Define(legacy, "seo", 1, contract.StringJSON[string]()), "x"); err == nil || err.Error() != identityError {
		t.Fatal("legacy source was not rejected by ORM identity validation", err)
	}
	if _, err := attachments.Matching(ctx, nil, attachments.Define(legacy, "files", attachments.Policy{Disk: storage.DefineDisk("files"), Cardinality: attachments.Single, Accepted: []storage.MediaType{"text/plain"}})); err == nil || err.Error() != identityError {
		t.Fatal("legacy source was not rejected by ORM identity validation", err)
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
