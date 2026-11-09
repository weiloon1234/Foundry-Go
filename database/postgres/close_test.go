package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
)

func TestPostgresConnectionCloseClassification(t *testing.T) {
	for _, cause := range []error{
		io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, net.ErrClosed,
		sql.ErrConnDone, driver.ErrBadConn, syscall.ECONNRESET, syscall.EPIPE,
		&pgconn.PgError{Code: "57P01"}, &pgconn.PgError{Code: "57P02"}, &pgconn.PgError{Code: "57P03"},
		&net.OpError{Op: "write", Net: "tcp", Err: &os.SyscallError{Syscall: "write", Err: syscall.ECONNRESET}},
	} {
		for _, wrapped := range []error{cause, fmt.Errorf("close_notify: %w", cause), errors.Join(io.EOF, cause)} {
			if !closedConnectionOnly(wrapped) {
				t.Fatalf("disconnect remained a close failure: %T", cause)
			}
			if closedConnectionOnly(errors.Join(wrapped, errors.New("cleanup failed"))) {
				t.Fatalf("disconnect hid a genuine failure: %T", cause)
			}
		}
	}
	for _, cause := range []error{
		nil, context.Canceled, context.DeadlineExceeded, os.ErrDeadlineExceeded,
		syscall.EACCES, syscall.ECONNREFUSED, pgx.ErrTxClosed,
		errors.New("conn closed"), errors.New("connection reset by peer"),
		&pgconn.PgError{Code: "53300"}, &pgconn.PgError{Code: "28000"},
		&pgconn.PgError{Code: "57014"}, &pgconn.PgError{Code: "XX000"},
		&pgconn.PgError{Code: "57P04"}, (*pgconn.PgError)(nil),
		database.NewError("close", database.Unavailable),
		fault.Wrap(fault.Internal, "cleanup failed", io.EOF),
		&errorgraphtest.Error{Mode: "cycle"}, &errorgraphtest.Error{Mode: "panic"},
	} {
		if closedConnectionOnly(cause) || closedConnectionOnly(errors.Join(io.EOF, cause, context.DeadlineExceeded)) {
			t.Fatalf("genuine or uninspectable failure was ignored: %T", cause)
		}
	}
	deep := error(io.EOF)
	for range 70 {
		deep = fmt.Errorf("wrapped: %w", deep)
	}
	if closedConnectionOnly(deep) {
		t.Fatal("incomplete close inspection was accepted")
	}
	if closedConnectionOnly(&errorgraphtest.Error{Mode: "goexit"}) {
		t.Fatal("Goexit during close inspection was accepted")
	}
}
