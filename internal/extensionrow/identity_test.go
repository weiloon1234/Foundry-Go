package extensionrow

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

type memberOwner = extensions.Owner[extensiontest.Member, int64]

// stored is the row identity a store writes for owner's member id.
func stored(t *testing.T, owner memberOwner, id int64, parts ...string) Identity {
	t.Helper()
	subject, err := owner.Subject(owner.Reference(id))
	if err != nil {
		t.Fatal(err)
	}
	input := append([]string{subject.Scope, subject.Key}, parts...)
	return Identity{Key: extensions.Digest(input...), Owner: string(owner.Name()), Scope: subject.Scope, SubjectKey: subject.Key, Identity: subject.Identity}
}

// Every validator must reach the decision full validation reaches: the fast
// path only skips re-deriving an identity that equals the expected one.
func TestRowsDecideAsFullValidation(t *testing.T) {
	owner := extensiontest.Members
	references := []model.Reference[extensiontest.Member, int64]{owner.Reference(1), owner.Reference(2)}
	batch, err := For(owner, references)
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range references {
		subject, err := owner.Subject(reference)
		if err != nil {
			t.Fatal(err)
		}
		if text, err := subject.Identity.Text(); err != nil || batch.expected[subject.Key] != text {
			t.Fatal("expected identity text differs from the stored snapshot, so stored rows miss the fast path", err)
		}
	}
	subject, err := owner.Subject(owner.Reference(1))
	if err != nil {
		t.Fatal(err)
	}
	one, two := stored(t, owner, 1, "title", "en"), stored(t, owner, 2, "title", "en")
	swapped, claimed, rekeyed, rescoped, renamed := one, two, one, one, one
	swapped.Identity = two.Identity
	claimed.SubjectKey = one.SubjectKey
	rekeyed.Key = two.Key
	rescoped.Scope = extensiontest.Others.Scope()
	renamed.Owner = string(extensiontest.Others.Name())
	cases := map[string]struct {
		row   Identity
		parts []string
		valid bool
	}{
		"current":          {one, []string{"title", "en"}, true},
		"batch member":     {two, []string{"title", "en"}, true},
		"outside batch":    {stored(t, owner, 3, "title", "en"), []string{"title", "en"}, true},
		"other parts":      {one, []string{"title", "ms"}, false},
		"swapped identity": {swapped, []string{"title", "en"}, false},
		"claimed subject":  {claimed, []string{"title", "en"}, false},
		"row key":          {rekeyed, []string{"title", "en"}, false},
		"scope":            {rescoped, []string{"title", "en"}, false},
		"owner name":       {renamed, []string{"title", "en"}, false},
	}
	for validator, rows := range map[string]Rows[extensiontest.Member, int64]{"batch": batch, "subject": ForSubject(owner, subject), "unknown": Unknown(owner)} {
		for name, test := range cases {
			err := rows.Validate(test.row, test.parts...)
			if test.valid != (err == nil) || !test.valid && !errors.Is(err, fault.Invalid) {
				t.Errorf("%s/%s: valid=%v, got %v", validator, name, test.valid, err)
			}
		}
	}
}

// A row recorded under the declared storage model before a table rename has
// a different identity text, so it takes the full path and stays readable.
func TestRowsValidateRowsRecordedBeforeARename(t *testing.T) {
	original := extensions.DefineOwner("members", extensiontest.MemberIdentity("extension_members"))
	pinned := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{StorageModel: "extension_members"})
	row := stored(t, original, 7, "seo")
	rows, err := For(pinned, []model.Reference[extensiontest.Member, int64]{pinned.Reference(7)})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := row.Identity.Text(); rows.expected[row.SubjectKey] == text {
		t.Fatal("a pre-rename identity matched the current identity text")
	}
	if err := rows.Validate(row, "seo"); err != nil {
		t.Fatal("pre-rename row rejected", err)
	}
	if err := rows.Validate(row, "other"); !errors.Is(err, fault.Invalid) {
		t.Fatal("pre-rename row accepted with other parts", err)
	}
}

func TestRowsBoundTheirBatch(t *testing.T) {
	owner := extensiontest.Members
	references := make([]model.Reference[extensiontest.Member, int64], query.MaxIdentityBatch+1)
	for i := range references {
		references[i] = owner.Reference(int64(i + 1))
	}
	if _, err := For(owner, references); !errors.Is(err, fault.Invalid) {
		t.Fatal("an oversized batch was accepted", err)
	}
	if _, err := For(owner, references[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := For(owner, []model.Reference[extensiontest.Member, int64]{{}}); err == nil {
		t.Fatal("a zero reference was accepted")
	}
}
