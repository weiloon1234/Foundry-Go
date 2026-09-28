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
		switch server.Code {
		case "23505":
			detail.Code = database.UniqueViolation
		case "23503":
			detail.Code = database.ForeignKeyViolation
		case "23502":
			detail.Code = database.NotNullViolation
		case "23514":
			detail.Code = database.CheckViolation
		case "40001":
			detail.Code = database.SerializationFailure
		case "40P01":
			detail.Code = database.Deadlock
		case "57014":
			detail.Code = database.Canceled
		case "57P01", "57P02", "57P03", "53300":
			detail.Code = database.Unavailable
		}
		if strings.HasPrefix(server.Code, "08") || strings.HasPrefix(server.Code, "28") {
			detail.Code = database.Unavailable
		}
		switch server.Code {
		case "23000", "23001", "23502", "23503", "23505", "23514", "23P01", "40000", "40001", "40002", "40P01":
			detail.CommitRejected = true
		}
		return detail
	}
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
