package sqlcallback

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type batchContextConnection struct{ got context.Context }

func (*batchContextConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*batchContextConnection) Close() error                        { return nil }
func (*batchContextConnection) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c *batchContextConnection) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	c.got = ctx
	return batchContextRows{}, nil
}

type batchContextRows struct{}

func (batchContextRows) Columns() []string         { return []string{"n"} }
func (batchContextRows) Close() error              { return nil }
func (batchContextRows) Next([]driver.Value) error { return io.EOF }

func TestRowBatchCapabilityIsRemovedBeforeApplicationDriver(t *testing.T) {
	ctx, batch := WithRowBatch(t.Context())
	source := &batchContextConnection{}
	conn := &connection{source: source}
	got, err := conn.QueryContext(ctx, "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	stream := got.(*rows)
	if stream.batch != batch || rowBatch(source.got) != nil {
		t.Fatal("row lease escaped to driver context")
	}
	nested, err := conn.QueryContext(source.got, "SELECT 1", nil)
	if err != nil || nested.(*rows).batch != nil {
		t.Fatal("retained driver context borrowed a foreign row lease")
	}
	if batch.active.Load() {
		t.Fatal("lease active outside isolated collection")
	}
	if err, exited := batch.Run("collect", func() error {
		if !batch.active.Load() {
			t.Fatal("lease inactive inside collection")
		}
		if err := stream.Next(make([]driver.Value, 1)); err != io.EOF {
			t.Fatal("EOF semantics changed")
		}
		return nil
	}); err != nil || exited || batch.active.Load() {
		t.Fatal("row lease outlived collection")
	}
}

func TestRowBatchWaitsForDeferredApplicationExit(t *testing.T) {
	for _, exit := range []bool{false, true} {
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		_, batch := WithRowBatch(t.Context())
		go func() {
			err, _ := batch.Run("collect", func() error {
				return batch.call("iterate SQL rows", func() error {
					defer func() { close(entered); <-release }()
					if exit {
						runtime.Goexit()
					}
					panic("private batch panic")
				})
			})
			done <- err
		}()
		<-entered
		select {
		case <-done:
			t.Fatal("batch returned before application cleanup exited")
		default:
		}
		if !batch.active.Load() {
			t.Fatal("batch released containment while application still owned cleanup")
		}
		close(release)
		err := <-done
		err = safeerror.Reveal("collect failed", err)
		if !errors.Is(err, fault.Panicked) || batch.active.Load() {
			t.Fatal("batch exit classification or ownership changed")
		}
	}
}
