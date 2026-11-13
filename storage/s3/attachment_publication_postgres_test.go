package s3

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// A real PostgreSQL store plus the real S3 SDK against the wire peer proves the
// public attachment transaction path above 64 MiB. The Compatible profile has
// explicit multipart conditions; no shipping R2 capability is bypassed here.
func TestPostgresLargeMultipartAttachmentTransactionLifecycle(t *testing.T) {
	f := extensiontest.Open(t, attachments.Migrations())
	disk, _, gate, peer := conditionalWireProfile(t, Compatible)
	registry, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	collection := attachments.Define(extensiontest.Members, "large-multipart", attachments.Policy{Disk: storage.DefineDisk("conditional-wire"), Cardinality: attachments.Multiple, MaxBytes: 512 << 20, AnyMedia: true})
	config := attachments.DefaultConfig()
	config.TempDirectory = t.TempDir()
	config.CleanupGrace = time.Minute
	start := func() *attachments.Manager {
		m, err := attachments.New(attachments.Dependencies{Store: f.Store, Disks: registry}, config, collection.Registration())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = m.Close(context.Background()) })
		return m
	}
	m := start()
	owner := extensions.ORMIdentity(extensiontest.Members).Reference(int64(1))
	const size int64 = 65 << 20
	prepare := func(content byte) attachments.Prepared[extensiontest.Member, int64] {
		p, err := collection.Prepare(t.Context(), m, owner, attachments.Upload{Source: io.LimitReader(conditionalByte(content), size)})
		if err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(config.TempDirectory)
		if err != nil || len(entries) != 0 {
			t.Fatal("Prepare retained its spool", err)
		}
		return p
	}
	state := func(p attachments.Prepared[extensiontest.Member, int64], want attachments.State) {
		s, err := m.Inspect(t.Context(), p.Operation())
		if err != nil || s.State != want {
			t.Fatal("unexpected durable intent", s.State, err)
		}
	}
	age := func(p attachments.Prepared[extensiontest.Member, int64]) {
		if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET updated_at=updated_at-interval '2 hours' WHERE id=$1`, p.Operation().String())
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	sweep := func() {
		if _, err := m.ReconcilePending(t.Context(), 20); err != nil {
			t.Fatal(err)
		}
	}
	first := prepare('a')
	state(first, attachments.Stored)
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := collection.AddIn(ctx, tx, m, owner, first)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	second := prepare('b')
	rollback := errors.New("test rollback")
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := collection.ReplaceIn(ctx, tx, m, owner, second); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	state(first, attachments.Ready)
	state(second, attachments.Stored)
	files, err := collection.List(t.Context(), m, owner)
	if err != nil || len(files) != 1 || files[0].ID().String() != first.Operation().String() {
		t.Fatal("rollback changed membership", err)
	}
	if err := m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	m = start()
	sweep()
	state(second, attachments.Stored) // Fresh reservation survives restart.
	age(second)
	sweep()
	state(second, attachments.CleanupPending) // StoredGrace then CleanupGrace.
	age(second)
	sweep()
	state(second, attachments.Cleaned)
	third := prepare('c')
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := collection.ReplaceIn(ctx, tx, m, owner, third)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	state(first, attachments.CleanupPending)
	state(third, attachments.Ready)
	age(first)
	sweep()
	state(first, attachments.Cleaned)
	// A lost storage acknowledgement is tracked and never published blindly.
	gate.mu.Lock()
	gate.lostComplete = true
	gate.mu.Unlock()
	uncertain, err := collection.Prepare(t.Context(), m, owner, attachments.Upload{Source: io.LimitReader(conditionalByte('d'), size)})
	gate.mu.Lock()
	gate.lostComplete = false
	gate.mu.Unlock()
	if err == nil || uncertain.IsZero() {
		t.Fatal("lost publication acknowledgement lost its durable intent")
	}
	state(uncertain, attachments.Uncertain)
	age(uncertain)
	sweep()
	state(uncertain, attachments.CleanupPending)
	age(uncertain)
	sweep()
	state(uncertain, attachments.Cleaned)
	files, err = collection.List(t.Context(), m, owner)
	if err != nil || len(files) != 1 {
		t.Fatal("uncertain upload changed membership", err)
	}
	if _, err := collection.Detach(t.Context(), m, owner, files[0].ID()); err != nil {
		t.Fatal(err)
	}
	state(third, attachments.CleanupPending)
	age(third)
	sweep()
	state(third, attachments.Cleaned)
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.invalid || len(peer.objects) != 0 || len(peer.uploads) != 0 {
		t.Fatal("lifecycle left published objects or staging")
	}
}
