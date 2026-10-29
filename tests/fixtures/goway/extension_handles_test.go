package goway_test

import "github.com/weiloon1234/Foundry-Go/validation"

import (
	"context"
	"errors"
	e "foundry.test/goway/models/extensions"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"

	"testing"
)

func TestThinExtensionHandlesPreserveLegacyPoliciesAndUnboundBehavior(t *testing.T) {
	current := e.ArticleExtensions.From(slots.Runtime{})

	descriptions := []struct{ current extensions.SlotDescription }{
		{current.Title.Describe()}, {current.Cover.Describe()},
		{current.Gallery.Describe()}, {current.SEO.Describe()},
	}
	for _, pair := range descriptions {
		if !oracleEqual(t, "extension_handles/descriptions", oracleSnapshot(pair.current)) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
	ctx := context.Background()
	for _, pair := range []struct{ current error }{
		{current.Title.Save(ctx, e.Article{ID: 1}, nil)},
		{current.Title.Sync(ctx, e.Article{ID: 1}, nil)},
		{current.Title.SaveIn(ctx, nil, e.Article{ID: 1}, nil)},
		{current.SEO.Save(ctx, e.Article{ID: 1}, "private")},
		{current.SEO.SaveIn(ctx, nil, e.Article{ID: 1}, "private")},
	} {
		for _, kind := range []error{fault.Invalid, fault.Missing} {
			if !oracleEqual(t, "extension_handles/write_fault", oracleSnapshot(errors.Is(pair.current, kind))) {
				t.Fatal("frozen legacy comparison differs")
			}
		}
	}
	newResult, newErr := current.Cover.Replace(ctx, e.Article{ID: 1}, attachments.Upload{})

	if !oracleEqual(t, "extension_handles/replace_outcome", oracleSnapshot([]any{newErr == nil, errors.Is(newErr, fault.Missing), errors.Is(newErr, fault.Invalid), newResult.Publication, newResult.Operation, newResult.Attachment.IsSet()})) {
		t.Fatal("frozen legacy comparison differs")
	}

	if newErr == nil {
		t.Fatal("unbound write accepted")
	}
	_, newErr = current.Gallery.Add(ctx, e.Article{ID: 1}, attachments.Upload{})

	if !oracleEqual(t, "extension_handles/add_outcome", oracleSnapshot([]any{newErr == nil, errors.Is(newErr, fault.Missing), errors.Is(newErr, fault.Invalid)})) {
		t.Fatal("frozen legacy comparison differs")
	}

	if newErr == nil {
		t.Fatal("unbound write accepted")
	}
	if validation.ValidateRule(current.Title.Rule()) == nil || validation.ValidateRule(current.Title.MergeRule()) == nil {
		t.Fatal("unbound rules accepted")
	}
	for _, relation := range current.All() {
		if _, err := orm.Compile(e.Articles.Query().With(relation)); !errors.Is(err, fault.Missing) {
			t.Fatal("unbound relation accepted", err)
		}
	}
}
