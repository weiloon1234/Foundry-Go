package postgres

import (
	"errors"
	"io"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weiloon1234/Foundry-Go/database"
)

// sqlStateCodes maps exact PostgreSQL SQLSTATE values. Class fallbacks follow
// in classify; unknown completion states stay uncertain, including 40003.
var sqlStateCodes = map[string]database.Code{
	"23505": database.UniqueViolation,
	"23503": database.ForeignKeyViolation,
	"23502": database.NotNullViolation,
	"23514": database.CheckViolation,
	"23P01": database.ExclusionViolation,
	"23001": database.RestrictViolation,
	"40001": database.SerializationFailure,
	"40P01": database.Deadlock,
	"55P03": database.LockNotAvailable,
	"55006": database.ObjectInUse,
	"57014": database.QueryCanceled,
	"25006": database.ReadOnlyTransaction,
	"25P03": database.IdleInTransactionTimeout,
	"22001": database.StringDataRightTruncation,
	"22P02": database.InvalidTextRepresentation,
	"22003": database.NumericValueOutOfRange,
	"22007": database.InvalidDatetimeFormat,
	"22008": database.DatetimeFieldOverflow,
	"22012": database.DivisionByZero,
	"53300": database.Unavailable,
	"57P01": database.Unavailable,
	"57P02": database.Unavailable,
	"57P03": database.Unavailable,
	"57P04": database.Unavailable,
	"57P05": database.Unavailable,
}

// Classifications use PostgreSQL SQLSTATE, never localized server messages.
// Unknown completion states stay uncertain, including class 40's 40003.
func classify(err error) database.Detail {
	detail := database.Detail{Code: database.QueryFailed}
	if errors.Is(err, pgx.ErrTxCommitRollback) {
		detail.CommitRejected = true
		return detail
	}
	if errors.Is(err, pgx.ErrTxClosed) {
		detail.Code = database.Closed
		return detail
	}
	var server *pgconn.PgError
	if errors.As(err, &server) {
		detail.SQLState, detail.Constraint = server.Code, server.ConstraintName
		if code, ok := sqlStateCodes[server.Code]; ok {
			detail.Code = code
		} else {
			switch sqlStateClass(server.Code) {
			case "08", "28":
				detail.Code = database.Unavailable
			case "22":
				detail.Code = database.DataException
			case "53":
				detail.Code = database.InsufficientResources
			}
		}
		switch server.Code {
		case "23000", "23001", "23502", "23503", "23505", "23514", "23P01", "40000", "40001", "40002", "40P01":
			detail.CommitRejected = true
		}
		detail.StatementRejected = statementRejected(server.Code)
		return detail
	}
	// pgconn reports requests that were never written to the server as safe to
	// retry; such a statement cannot have committed outside a transaction.
	detail.StatementRejected = pgconn.SafeToRetry(err)
	var network net.Error
	if errors.As(err, &network) {
		detail.Code = database.Unavailable
		if network.Timeout() {
			detail.Code = database.DeadlineExceeded
		}
	} else if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		detail.Code = database.Unavailable
	}
	return detail
}

func sqlStateClass(code string) string {
	if len(code) < 2 {
		return ""
	}
	return code[:2]
}

// statementRejected allows only SQLSTATE classes that abort a statement before
// its implicit transaction completes. Connection, cancellation, resource,
// system and unknown-completion classes stay uncertain after sending.
func statementRejected(code string) bool {
	if code == "40003" || code == "25P03" {
		return false
	}
	switch sqlStateClass(code) {
	case "0A", "21", "22", "23", "25", "26", "27", "2B", "2D", "2F", "34", "38", "39", "3B", "3D", "3F", "40", "42", "44", "54", "55", "P0":
		return true
	}
	return strings.HasPrefix(code, "0L") || strings.HasPrefix(code, "0P")
}
