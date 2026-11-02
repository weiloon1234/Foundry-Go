package attachments

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresAttachmentRetirementInRollbackAndCommit(t *testing.T) {
	for _, clear := range []bool{false, true} {
		name := "detach"
		if clear {
			name = "clear"
		}
		t.Run(name, func(t *testing.T) {
			f := openAttachments(t)
			owner := member(t, 1)
			first := addText(t, f, testMultiple, owner, "first")
			second := addText(t, f, testMultiple, owner, "second")
			file := attachmentOf(t, first)
			other := attachmentOf(t, second)
			var deletes atomic.Int32
			f.backend.setDelete(func(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
				deletes.Add(1)
				return f.backend.Backend.Delete(ctx, key, options)
			})
			wantAffected := 1
			if clear {
				wantAffected = 2
			}
			rollback := errors.New("rollback retirement")
			for _, commit := range []bool{false, true} {
				err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
					var result ChangeResult
					var err error
					if clear {
						result, err = testMultiple.ClearIn(ctx, tx, f.manager, owner)
					} else {
						result, err = testMultiple.DetachIn(ctx, tx, f.manager, owner, file.ID())
					}
					if err != nil {
						return err
					}
					if result.Publication != Published || result.Affected != wantAffected || len(result.PendingCleanup) != wantAffected {
						return errors.New("retirement did not report pending commit and cleanup")
					}
					files, err := testMultiple.ListIn(ctx, tx, f.manager, owner)
					if err != nil {
						return err
					}
					if len(files) != 2-wantAffected || (!clear && files[0].ID() != other.ID()) {
						return errors.New("transaction did not see its retired membership")
					}
					visible, err := testMultiple.List(ctx, f.manager, owner)
					if err != nil || len(visible) != 2 {
						return errors.Join(errors.New("uncommitted retirement leaked"), err)
					}
					for _, key := range []storage.ObjectKey{file.key, other.key} {
						if _, err := f.disk.Stat(ctx, key, storage.ReadOptions{}); err != nil {
							return err
						}
					}
					if deletes.Load() != 0 {
						return errors.New("storage cleanup ran before commit")
					}
					if !commit {
						return rollback
					}
					return nil
				})
				if !commit {
					if !errors.Is(err, rollback) || deletes.Load() != 0 {
						t.Fatal("rollback cleaned storage", err)
					}
					if journal(t, f, first.Operation).State != string(Ready) || journal(t, f, second.Operation).State != string(Ready) {
						t.Fatal("rollback retired membership")
					}
					assertBody(t, f, testMultiple, owner, file, "first")
					assertBody(t, f, testMultiple, owner, other, "second")
					continue
				}
				if err != nil || int(deletes.Load()) != wantAffected {
					t.Fatal("committed cleanup did not run", err, deletes.Load())
				}
				if journal(t, f, first.Operation).State != string(Cleaned) {
					t.Fatal("committed retirement was not cleaned")
				}
				if _, err := f.disk.Stat(t.Context(), file.key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
					t.Fatal("retired object remains", err)
				}
				files, err := testMultiple.List(t.Context(), f.manager, owner)
				if err != nil || len(files) != 2-wantAffected {
					t.Fatal("committed membership", err)
				}
				if clear {
					if journal(t, f, second.Operation).State != string(Cleaned) {
						t.Fatal("clear left its second object")
					}
				} else {
					assertBody(t, f, testMultiple, owner, other, "second")
				}
			}
		})
	}
}

func TestPostgresAttachmentFirstInSeesOwnPublicationAndClear(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	initial := addText(t, f, testSingle, owner, "old")
	old := attachmentOf(t, initial)
	prepared, err := testSingle.Prepare(t.Context(), f.manager, owner, uploadText("new"))
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback publication and clear")
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		result, err := testSingle.ReplaceIn(ctx, tx, f.manager, owner, prepared)
		if err != nil {
			return err
		}
		published, ok := result.Attachment.Get()
		first, err := testSingle.FirstIn(ctx, tx, f.manager, owner)
		got, exists := first.Get()
		if err != nil || !ok || !exists || got.ID() != published.ID() || got.ID() == old.ID() {
			return errors.Join(errors.New("FirstIn did not see the uncommitted replacement"), err)
		}
		if _, err := testSingle.ClearIn(ctx, tx, f.manager, owner); err != nil {
			return err
		}
		first, err = testSingle.FirstIn(ctx, tx, f.manager, owner)
		if err != nil || first.IsSet() {
			return errors.Join(errors.New("FirstIn did not see the uncommitted clear"), err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	assertBody(t, f, testSingle, owner, old, "old")
	if journal(t, f, prepared.Operation()).State != string(Stored) {
		t.Fatal("rollback did not restore prepared upload")
	}
}

func TestPostgresAttachmentInRequiresStoreTransaction(t *testing.T) {
	f := openAttachments(t)
	other := openAttachments(t)
	owner := member(t, 1)
	file := attachmentOf(t, addText(t, f, testSingle, owner, "kept"))
	check := func(ctx context.Context, tx *database.Tx) error {
		_, clearErr := testSingle.ClearIn(ctx, tx, f.manager, owner)
		_, detachErr := testSingle.DetachIn(ctx, tx, f.manager, owner, file.ID())
		_, listErr := testSingle.ListIn(ctx, tx, f.manager, owner)
		_, firstErr := testSingle.FirstIn(ctx, tx, f.manager, owner)
		for _, err := range []error{clearErr, detachErr, listErr, firstErr} {
			if !errors.Is(err, fault.Invalid) {
				return errors.Join(errors.New("accepted a nil or foreign transaction"), err)
			}
		}
		return nil
	}
	if err := check(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := other.Store.Write(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	assertBody(t, f, testSingle, owner, file, "kept")
}

func TestPostgresAttachmentRetirementInCleanupFailureIsAfterCommit(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	initial := addText(t, f, testSingle, owner, "old")
	file := attachmentOf(t, initial)
	f.backend.setDelete(func(context.Context, storage.ObjectKey, storage.DeleteOptions) error {
		return errors.New("cleanup unavailable")
	})
	err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := testSingle.ClearIn(ctx, tx, f.manager, owner)
		return err
	})
	if err == nil || transactionOutcome(err) != database.Committed {
		t.Fatal("cleanup failure lost committed outcome", err)
	}
	files, err := testSingle.List(t.Context(), f.manager, owner)
	if err != nil || len(files) != 0 || journal(t, f, initial.Operation).State != string(CleanupPending) {
		t.Fatal("cleanup failure restored membership or lost recovery intent", err)
	}
	if _, err := f.disk.Stat(t.Context(), file.key, storage.ReadOptions{}); err != nil {
		t.Fatal("failed cleanup lost stored object", err)
	}
	f.backend.setDelete(nil)
	status, err := f.manager.Reconcile(t.Context(), initial.Operation)
	if err != nil || status.State != Cleaned {
		t.Fatal("cleanup could not recover", err)
	}
}

func TestPostgresAttachmentClearInAndReplaceInHoldOwnerLockUntilCommit(t *testing.T) {
	for _, firstReplace := range []bool{false, true} {
		for _, nextReplace := range []bool{false, true} {
			name := "clear"
			if firstReplace {
				name = "replace"
			}
			name += "/then-"
			if nextReplace {
				name += "replace"
			} else {
				name += "clear"
			}
			t.Run(name, func(t *testing.T) {
				f := openAttachments(t)
				owner := member(t, 1)
				addText(t, f, testSingle, owner, "old")
				first, err := testSingle.Prepare(t.Context(), f.manager, owner, uploadText("first"))
				if err != nil {
					t.Fatal(err)
				}
				next, err := testSingle.Prepare(t.Context(), f.manager, owner, uploadText("next"))
				if err != nil {
					t.Fatal(err)
				}
				runCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				writerPID := make(chan int, 1)
				done := make(chan error, 1)
				started := false
				err = f.Store.Write(runCtx, func(ctx context.Context, tx *database.Tx) error {
					if firstReplace {
						if _, err := testSingle.ReplaceIn(ctx, tx, f.manager, owner, first); err != nil {
							return err
						}
					} else if _, err := testSingle.ClearIn(ctx, tx, f.manager, owner); err != nil {
						return err
					}
					var blocker int
					if err := database.ScanOne(ctx, tx, `SELECT pg_backend_pid()`, nil, &blocker); err != nil {
						return err
					}
					started = true
					go func() {
						done <- f.Store.Write(runCtx, func(ctx context.Context, tx *database.Tx) error {
							var pid int
							if err := database.ScanOne(ctx, tx, `SELECT pg_backend_pid()`, nil, &pid); err != nil {
								return err
							}
							writerPID <- pid
							if nextReplace {
								_, err := testSingle.ReplaceIn(ctx, tx, f.manager, owner, next)
								return err
							}
							_, err := testSingle.ClearIn(ctx, tx, f.manager, owner)
							return err
						})
					}()
					var pid int
					select {
					case pid = <-writerPID:
					case <-ctx.Done():
						return ctx.Err()
					}
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					for {
						var blocked bool
						if err := database.ScanOne(ctx, tx, `SELECT $1::integer = ANY(pg_blocking_pids($2::integer))`, []any{blocker, pid}, &blocked); err != nil {
							return err
						}
						if blocked {
							return nil
						}
						select {
						case <-ticker.C:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				})
				if err != nil {
					cancel()
				}
				if started {
					if nextErr := <-done; nextErr != nil {
						t.Error("concurrent mutation failed", nextErr)
					}
				}
				if err != nil {
					t.Fatal("mutation did not hold the owner lock until commit", err)
				}
				files, err := testSingle.List(t.Context(), f.manager, owner)
				if err != nil {
					t.Fatal(err)
				}
				if nextReplace {
					if len(files) != 1 {
						t.Fatal("serialized replacement missing")
					}
					assertBody(t, f, testSingle, owner, files[0], "next")
				} else if len(files) != 0 {
					t.Fatal("serialized clear left membership")
				}
			})
		}
	}
}
