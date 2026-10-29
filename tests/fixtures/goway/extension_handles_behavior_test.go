package goway_test

import "github.com/weiloon1234/Foundry-Go/validation"

import (
	"errors"
	e "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
	"reflect"
	"strings"
	"testing"
)

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
	for _, err := range []error{title.Validate(), cover.Validate(), gallery.Validate(), seo.Validate(), validation.ValidateRule(title.Rule()), validation.ValidateRule(title.MergeRule())} {
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
