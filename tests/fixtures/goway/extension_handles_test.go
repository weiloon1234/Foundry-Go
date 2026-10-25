package goway_test

import (
	"context"
	"errors"
	e "foundry.test/goway/models/extensions"
	old "foundry.test/goway/oldmodels/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"reflect"
	"testing"
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
