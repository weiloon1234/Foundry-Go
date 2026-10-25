package goway_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	e "foundry.test/goway/models/extensions"
	old "foundry.test/goway/oldmodels/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func TestThinExtensionHandlesPreserveLegacyPoliciesAndUnboundBehavior(t *testing.T) {
	current := e.ArticleExtensions.From(slots.Runtime{})
	legacy := old.ArticleExtensions().From(slots.Runtime{})
	descriptions := []struct{ current, legacy extensions.SlotDescription }{
		{current.Title.Describe(), legacy.Title.Describe()}, {current.Cover.Describe(), legacy.Cover.Describe()},
		{current.Gallery.Describe(), legacy.Gallery.Describe()}, {current.SEO.Describe(), legacy.SEO.Describe()},
	}
	for _, pair := range descriptions {
		if !reflect.DeepEqual(pair.current, pair.legacy) || !oracleEqual(t, "extension_handles/descriptions", oracleSnapshot(pair.legacy), oracleSnapshot(pair.current)) {
			t.Fatal("policy drift", pair)
		}
	}
	ctx := context.Background()
	for _, pair := range []struct{ current, legacy error }{
		{current.Title.Save(ctx, e.Article{ID: 1}, nil), legacy.Title.Save(ctx, old.Article{ID: 1}, nil)},
		{current.Title.Sync(ctx, e.Article{ID: 1}, nil), legacy.Title.Sync(ctx, old.Article{ID: 1}, nil)},
		{current.Title.SaveIn(ctx, nil, e.Article{ID: 1}, nil), legacy.Title.SaveIn(ctx, nil, old.Article{ID: 1}, nil)},
		{current.SEO.Save(ctx, e.Article{ID: 1}, "private"), legacy.SEO.Save(ctx, old.Article{ID: 1}, "private")},
		{current.SEO.SaveIn(ctx, nil, e.Article{ID: 1}, "private"), legacy.SEO.SaveIn(ctx, nil, old.Article{ID: 1}, "private")},
	} {
		for _, kind := range []error{fault.Invalid, fault.Missing} {
			if !oracleEqual(t, "extension_handles/write_fault", oracleSnapshot(errors.Is(pair.legacy, kind)), oracleSnapshot(errors.Is(pair.current, kind))) {
				t.Fatal("write fault drift", pair)
			}
		}
	}
	newResult, newErr := current.Cover.Replace(ctx, e.Article{ID: 1}, attachments.Upload{})
	oldResult, oldErr := legacy.Cover.Replace(ctx, old.Article{ID: 1}, attachments.Upload{})
	if !oracleEqual(t, "extension_handles/replace_outcome", oracleSnapshot([]any{oldErr == nil, errors.Is(oldErr, fault.Missing), errors.Is(oldErr, fault.Invalid), oldResult.Publication, oldResult.Operation, oldResult.Attachment.IsSet()}), oracleSnapshot([]any{newErr == nil, errors.Is(newErr, fault.Missing), errors.Is(newErr, fault.Invalid), newResult.Publication, newResult.Operation, newResult.Attachment.IsSet()})) {
		t.Fatal("replace outcome parity")
	}

	if newErr == nil || oldErr == nil || errors.Is(newErr, fault.Missing) != errors.Is(oldErr, fault.Missing) || errors.Is(newErr, fault.Invalid) != errors.Is(oldErr, fault.Invalid) || newResult.Publication != oldResult.Publication || newResult.Operation != oldResult.Operation || newResult.Attachment.IsSet() != oldResult.Attachment.IsSet() {
		t.Fatal("replace fault drift", newErr, oldErr)
	}
	_, newErr = current.Gallery.Add(ctx, e.Article{ID: 1}, attachments.Upload{})
	_, oldErr = legacy.Gallery.Add(ctx, old.Article{ID: 1}, attachments.Upload{})
	if !oracleEqual(t, "extension_handles/add_outcome", oracleSnapshot([]any{oldErr == nil, errors.Is(oldErr, fault.Missing), errors.Is(oldErr, fault.Invalid)}), oracleSnapshot([]any{newErr == nil, errors.Is(newErr, fault.Missing), errors.Is(newErr, fault.Invalid)})) {
		t.Fatal("add outcome parity")
	}

	if newErr == nil || oldErr == nil || errors.Is(newErr, fault.Missing) != errors.Is(oldErr, fault.Missing) || errors.Is(newErr, fault.Invalid) != errors.Is(oldErr, fault.Invalid) {
		t.Fatal("add fault drift", newErr, oldErr)
	}
	if current.Title.Rule().Validate() == nil || current.Title.MergeRule().Validate() == nil {
		t.Fatal("unbound rules accepted")
	}
	for _, relation := range current.All() {
		if _, err := orm.Compile(e.Articles.Query().With(relation)); !errors.Is(err, fault.Missing) {
			t.Fatal("unbound relation accepted", err)
		}
	}
}

func TestExtensionHandlesDoNotExposeLegacyFamilies(t *testing.T) {
	for _, handle := range []any{e.ArticleExtensions.Title, e.ArticleExtensions.Cover, e.ArticleExtensions.Gallery, e.ArticleExtensions.SEO} {
		typ := reflect.TypeOf(handle)
		if typ.NumMethod() > 25 {
			t.Fatal("handle exceeds R1 method budget", typ, typ.NumMethod())
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous {
				t.Fatal("embedded descriptor", typ, field.Name)
			}
			if field.Type.Kind() == reflect.Array && field.Type.Len() == 0 {
				continue
			}
			if field.Type.Kind() != reflect.Pointer || strings.Contains(field.Type.Elem().Name(), "[") {
				t.Fatal("generic retained field", typ, field.Type)
			}
		}
		for _, name := range []string{"Field", "Collection", "Key"} {
			if _, ok := typ.MethodByName(name); ok {
				t.Fatal("legacy descriptor escape", typ, name)
			}
		}
	}
	single, multiple := reflect.TypeOf(e.ArticleExtensions.Cover), reflect.TypeOf(e.ArticleExtensions.Gallery)
	if single.ConvertibleTo(multiple) || multiple.ConvertibleTo(single) {
		t.Fatal("single/multiple capabilities convertible")
	}
	if _, ok := single.MethodByName("Add"); ok {
		t.Fatal("single collection has add capability")
	}
	if _, ok := multiple.MethodByName("Add"); !ok {
		t.Fatal("multiple collection lost add capability")
	}
}

func TestZeroExtensionHandlesFailClosed(t *testing.T) {
	title := translations.TextHandle[e.Article, int64]{}
	cover := attachments.OneHandle[e.Article, int64]{}
	gallery := attachments.ManyHandle[e.Article, int64]{}
	seo := metadata.ValueHandle[e.Article, int64, string]{}
	for _, err := range []error{title.Validate(), cover.Validate(), gallery.Validate(), seo.Validate(), title.Rule().Validate(), title.MergeRule().Validate()} {
		if err == nil {
			t.Fatal("zero handle accepted")
		}
	}
	for _, rel := range []orm.Relation[e.Article]{title.Relation(e.Articles.Title), cover.Relation(e.Articles.Cover), gallery.Relation(e.Articles.Gallery), seo.Relation(e.Articles.SEO)} {
		if _, err := orm.Compile(e.Articles.Query().With(rel)); err == nil {
			t.Fatal("zero relation accepted")
		}
	}
	if title.From(nil).Validate() == nil || seo.From(nil).Validate() == nil {
		t.Fatal("binding revived zero handle")
	}
}

func TestThinExtensionBindingKeepsUnboundDeclarationImmutable(t *testing.T) {
	// Binding borrows manager identities; planning does not acquire resources or
	// execute them, so these managers need no database/storage setup here.
	bound := e.ArticleExtensions.From(slots.Runtime{Translations: &translations.Manager{}, Attachments: &attachments.Manager{}, Metadata: &metadata.Manager{}})
	for _, rel := range bound.All() {
		if _, err := orm.Compile(e.Articles.Query().With(rel)); err != nil {
			t.Fatal("bound loader lost its callback", err)
		}
	}
	for _, descriptors := range []e.Descriptors{e.ArticleExtensions, bound.From(slots.Runtime{})} {
		for _, rel := range descriptors.All() {
			if _, err := orm.Compile(e.Articles.Query().With(rel)); !errors.Is(err, fault.Missing) {
				t.Fatal("binding mutated declaration or lost unbound state", err)
			}
		}
	}
}
