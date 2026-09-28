package attachments

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type commitConnector struct {
	driver.Connector
	mode *atomic.Int32
}

func (c commitConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &commitConnection{Conn: conn, mode: c.mode}, nil
}

type commitConnection struct {
	driver.Conn
	mode *atomic.Int32
}

func (c *commitConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &commitTransaction{Tx: tx, mode: c.mode}, nil
}
func (c *commitConnection) QueryContext(ctx context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, sql, args)
}
func (c *commitConnection) ExecContext(ctx context.Context, sql string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, sql, args)
}
func (c *commitConnection) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *commitConnection) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}
func (c *commitConnection) IsValid() bool {
	if r, ok := c.Conn.(driver.Validator); ok {
		return r.IsValid()
	}
	return true
}
func (c *commitConnection) CheckNamedValue(v *driver.NamedValue) error {
	if r, ok := c.Conn.(driver.NamedValueChecker); ok {
		return r.CheckNamedValue(v)
	}
	return driver.ErrSkip
}

type commitTransaction struct {
	driver.Tx
	mode *atomic.Int32
}

func (t *commitTransaction) Commit() error {
	switch t.mode.Swap(0) {
	case 1:
		if err := t.Tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected lost commit acknowledgement")
	case 2:
		if err := t.Tx.Rollback(); err != nil {
			return err
		}
		return errors.New("injected unknown commit after rollback")
	default:
		return t.Tx.Commit()
	}
}

func TestPostgresAttachmentAmbiguousCommitDoesNotDeleteEitherCandidateBlindly(t *testing.T) {
	for _, mode := range []int32{1, 2} {
		t.Run(map[int32]string{1: "committed", 2: "rolled-back"}[mode], func(t *testing.T) {
			var next atomic.Int32
			armed := false
			collection := Define(extensiontest.Members, "ambiguous", testSingle.definition.policy, Hook[extensiontest.Member, int64]{AfterStored: func(context.Context, *database.Tx, Attachment[extensiontest.Member, int64]) error {
				if armed {
					next.Store(mode)
				}
				return nil
			}})
			f := openAttachmentsWithConnector(t, func(base driver.Connector) driver.Connector { return commitConnector{Connector: base, mode: &next} }, collection.Registration())
			owner := member(t, 1)
			initial := addText(t, f, collection, owner, "old")
			old := attachmentOf(t, initial)
			armed = true
			result, err := collection.Replace(t.Context(), f.manager, owner, uploadText("new"))
			if !errors.Is(err, database.CommitUnknown) || result.Publication != PublicationUnknown || result.Attachment.IsSet() {
				t.Fatal("ambiguous commit presented as known", err)
			}
			candidate := journal(t, f, result.Operation)
			key, err := storage.ParseKey(candidate.ObjectKey)
			if err != nil {
				t.Fatal(err)
			}
			for _, objectKey := range []storage.ObjectKey{old.key, key} {
				if _, err := f.disk.Stat(t.Context(), objectKey, storage.ReadOptions{}); err != nil {
					t.Fatal("ambiguous commit caused blind cleanup", err)
				}
			}
			files, err := collection.List(t.Context(), f.manager, owner)
			if err != nil || len(files) != 1 {
				t.Fatal("durable publication state", err)
			}
			if mode == 1 {
				if candidate.State != string(Ready) || files[0].ID() == old.ID() {
					t.Fatal("committed row disappeared")
				}
				state, err := f.manager.Reconcile(t.Context(), initial.Operation)
				if err != nil || state.State != Cleaned {
					t.Fatal("durable old-object retry", err)
				}
				assertBody(t, f, collection, owner, files[0], "new")
			} else {
				if candidate.State != string(Stored) || files[0].ID() != old.ID() {
					t.Fatal("rolled back ownership changed")
				}
				if _, err := f.manager.Reconcile(t.Context(), result.Operation); err == nil {
					t.Fatal("fresh stored intent reclaimed")
				}
				if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
					_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET updated_at=$1 WHERE id=$2`, time.Now().UTC().Add(-10*time.Minute), result.Operation.String())
					return err
				}); err != nil {
					t.Fatal(err)
				}
				state, err := f.manager.Reconcile(t.Context(), result.Operation)
				if err != nil || state.State != Cleaned {
					t.Fatal("abandoned stored candidate not reclaimed", err)
				}
				assertBody(t, f, collection, owner, old, "old")
			}
		})
	}
}
