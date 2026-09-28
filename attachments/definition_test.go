package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

var testDisk = storage.DefineDisk("attachments")
var testSingle = Define(extensiontest.Members, "avatar", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"text/plain"}})
var testMultiple = Define(extensiontest.Members, "documents", Policy{Disk: testDisk, Cardinality: Multiple, MaxFiles: 3, Accepted: []storage.MediaType{"text/plain"}})
var testLocalized = Define(extensiontest.Members, "localized", Policy{Disk: testDisk, Cardinality: Multiple, MaxFiles: 3, Localized: true, Accepted: []storage.MediaType{"text/plain"}})
var testOther = Define(extensiontest.Others, "avatar", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"text/plain"}})

func TestCollectionSnapshotsPolicyAndRejectsAmbiguousContracts(t *testing.T) {
	accepted := []storage.MediaType{"text/plain"}
	policy := Policy{Disk: testDisk, Cardinality: Single, Accepted: accepted}
	collection := Define(extensiontest.Members, "sample", policy)
	accepted[0] = "image/png"
	if collection.Validate() != nil || collection.definition.policy.Accepted[0] != "text/plain" {
		t.Fatal("policy not frozen")
	}
	for _, bad := range []Policy{
		{Disk: testDisk, Cardinality: Single},
		{Disk: testDisk, Cardinality: Single, MaxFiles: 2, AnyMedia: true},
		{Disk: testDisk, Cardinality: Multiple, MaxFiles: MaxCollectionFiles + 1, AnyMedia: true},
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"text/plain; charset=utf-8"}},
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"text/plain", "text/plain"}},
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"text/plain"}, AnyMedia: true},
		{Disk: testDisk, Cardinality: Single, AnyMedia: true, MaxBytes: MaxUploadBytes + 1},
	} {
		if Define(extensiontest.Members, "bad", bad).Validate() == nil {
			t.Errorf("invalid policy accepted: %+v", bad)
		}
	}
	if testSingle.ForLocale("en").Validate() == nil || testLocalized.ForLocale("en-us").Validate() == nil || testLocalized.ForLocale("en").Registration().id != nil {
		t.Fatal("locale binding accepted invalid registration")
	}
	if (Collection[extensiontest.Member, int64]{}).Validate() == nil {
		t.Fatal("zero collection accepted")
	}
}

type trackedSource struct {
	*strings.Reader
	closed bool
}

func (s *trackedSource) Close() error { s.closed = true; return nil }
func TestUploadUsesBytesAndBoundsWithoutClosingBorrowedSource(t *testing.T) {
	source := &trackedSource{Reader: strings.NewReader("plain payload")}
	result, err := prepare(t.Context(), nil, testSingle.definition.policy, Upload{Source: source, OriginalName: "portrait.png", ContentType: "image/png"})
	if err != nil || source.closed || result.info.MediaType != "text/plain" || result.info.Size != 13 || result.digest != storage.SHA256(sha256.Sum256([]byte("plain payload"))) {
		t.Fatal("detected upload mismatch", err)
	}
	first, _ := io.ReadAll(result.open())
	first[0] = 'X'
	second, _ := io.ReadAll(result.open())
	if string(second) != "plain payload" {
		t.Fatal("prepared reader exposed snapshot")
	}
	for _, upload := range []Upload{{Source: strings.NewReader("x"), OriginalName: "../x"}, {Source: strings.NewReader("x"), OriginalName: "bad\x00"}, {Source: bytes.NewReader([]byte{0, 1, 2}), ContentType: "text/plain"}, {Source: nil}} {
		if _, err := prepare(t.Context(), nil, testSingle.definition.policy, upload); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	limited := testSingle.definition.policy
	limited.MaxBytes = 2
	if _, err := prepare(t.Context(), nil, limited, Upload{Source: strings.NewReader("long")}); !errors.Is(err, storage.LimitExceeded) {
		t.Fatal("input bound", err)
	}
	limited = testSingle.definition.policy
	limited.MaxStoredBytes = 2
	if _, err := prepare(t.Context(), nil, limited, Upload{Source: strings.NewReader("long")}); !errors.Is(err, storage.LimitExceeded) {
		t.Fatal("stored bound", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepare(ctx, nil, testSingle.definition.policy, Upload{Source: strings.NewReader("x")}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled reader accepted", err)
	}
}
func TestPropertiesAreExplicitBoundedObjects(t *testing.T) {
	for _, text := range []string{`null`, `[]`, `true`, `1`, `"x"`, `{"x":"` + strings.Repeat("a", MaxPropertiesBytes) + `"}`} {
		input, err := value.ParseJSON[json.RawMessage](text)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := normalizeProperties(input); err == nil {
			t.Fatal("non-object/oversized properties accepted")
		}
	}
	input, err := value.ParseJSON[json.RawMessage](`{"n":9007199254740993}`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := normalizeProperties(input)
	if err != nil {
		t.Fatal(err)
	}
	text, err := snapshot.Text()
	if err != nil || text != `{"n":9007199254740993}` {
		t.Fatal("properties lost integer precision", err)
	}
	if _, err := json.Marshal(Attachment[extensiontest.Member, int64]{}); err == nil {
		t.Fatal("implicit attachment serialization")
	}
}

type hostileError struct{}

func (hostileError) Error() string { return "hostile" }
func (hostileError) As(any) bool   { panic("classification extension") }
func TestTransactionOutcomeContainsHostileErrorClassification(t *testing.T) {
	if transactionOutcome(hostileError{}) != database.Unknown {
		t.Fatal("hostile classification treated as safe rollback")
	}
	if transactionOutcome(errors.New("veto")) != database.NoCommit || transactionOutcome(nil) != database.Committed {
		t.Fatal("ordinary outcome mismatch")
	}
}
