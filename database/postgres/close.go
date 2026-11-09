package postgres

import (
	"database/sql"
	"database/sql/driver"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weiloon1234/Foundry-Go/internal/closeerror"
)

// Close releases the physical connection even when PostgreSQL already ended
// the session. TLS close_notify can fail after pgx has closed the socket.
func (c *protectedConnection) Close() error {
	err := c.Conn.Close()
	if closedConnectionOnly(err) {
		return nil
	}
	return err
}

// Apply this only to physical connection close, never queries or transactions.
// Unavailable also includes authentication and resource failures, so it is not
// sufficient evidence. Every leaf must identify a disconnected connection.
func closedConnectionOnly(err error) bool {
	return closeerror.OnlyDisconnected(err, func(current error) bool {
		if current == sql.ErrConnDone || current == driver.ErrBadConn {
			return true
		}
		if server, ok := current.(*pgconn.PgError); ok && server != nil {
			switch server.Code {
			case "57P01", "57P02", "57P03":
				return true
			}
		}
		return false
	})
}
