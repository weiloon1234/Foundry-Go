package database

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const savepointCleanupTimeout = 5 * time.Second

// Savepoint creates an explicit nested scope. Its callback must use the supplied
// child Tx. The parent rejects other operations until this scope finishes.
// Success releases the savepoint, merging callbacks into the parent; it does not
// commit. Failure rolls back this scope and discards its after-commit callbacks.
// If rollback/release fails, the outer transaction is poisoned and cannot commit
// even when application code ignores the savepoint error.
func (tx *Tx) Savepoint(ctx context.Context, fn func(*Tx) error) error {
	if fn == nil {
		return fault.New(fault.Invalid, "savepoint callback is nil")
	}
	release, err := tx.enter()
	if err != nil {
		return err
	}
	defer release()
	scope, cancel := tx.operationContext(ctx)
	defer cancel()
	name := "foundry_sp_" + strconv.FormatUint(tx.control.sequence.Add(1), 10)
	if _, err := execute(scope, tx.raw, tx.classify, tx.owner.instrumented(PrimaryPool), "SAVEPOINT "+name, nil); err != nil {
		return err
	}
	child := &Tx{owner: tx.owner, raw: tx.raw, scope: &operationScope{ctx: scope, cancel: tx.control.cancel}, classify: tx.classify, control: tx.control, state: TxActive, observers: tx.observers, timeSource: tx.timeSource}
	err = invokeScope("savepoint callback", func() error { return fn(child) })
	err = errors.Join(err, child.finishScope())
	if err == nil {
		err = scope.Err()
	}
	if err != nil {
		cleanupErr := tx.rollbackSavepoint(ctx, name)
		if cleanupErr == nil {
			child.setState(TxRolledBack)
		} else {
			child.setState(TxUnknown)
			tx.control.poison(cleanupErr)
		}
		return errors.Join(tx.classify.scoped("savepoint", err), cleanupErr)
	}
	if _, err := execute(scope, tx.raw, tx.classify, tx.owner.instrumented(PrimaryPool), "RELEASE SAVEPOINT "+name, nil); err != nil {
		child.setState(TxUnknown)
		tx.control.poison(err)
		return err
	}
	child.setState(TxReleased)
	tx.mu.Lock()
	tx.after = append(tx.after, child.callbacks()...)
	tx.mu.Unlock()
	return nil
}

func (tx *Tx) rollbackSavepoint(ctx context.Context, name string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), savepointCleanupTimeout)
	defer cancel()
	if _, err := execute(cleanup, tx.raw, tx.classify, tx.owner.instrumented(PrimaryPool), "ROLLBACK TO SAVEPOINT "+name, nil); err != nil {
		return err
	}
	_, err := execute(cleanup, tx.raw, tx.classify, tx.owner.instrumented(PrimaryPool), "RELEASE SAVEPOINT "+name, nil)
	return err
}
