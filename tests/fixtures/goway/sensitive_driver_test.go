package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This driver retains only the two sensitive fixture tables and records redacted
// SQL/binding shapes. Returned envelopes are the actual bound write values.
type sensitiveStore struct {
	rows       map[string]map[string][]driver.Value
	transcript strings.Builder
	readBatch  [][]driver.Value
}
type sensitiveConnector struct{ *sensitiveStore }

func (c sensitiveConnector) Driver() driver.Driver { return c }
func (c sensitiveConnector) Open(string) (driver.Conn, error) {
	return sensitiveConnection{c.sensitiveStore}, nil
}
func (c sensitiveConnector) Connect(context.Context) (driver.Conn, error) { return c.Open("") }

type sensitiveConnection struct{ *sensitiveStore }

func (s sensitiveConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (s sensitiveConnection) Close() error              { return nil }
func (s sensitiveConnection) Begin() (driver.Tx, error) { return transaction{}, nil }
func (s sensitiveConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return s.Begin()
}
func (s sensitiveConnection) Ping(context.Context) error { return nil }
func (s sensitiveConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("unexpected set-based SQL")
}

var sensitiveColumns = map[string][]string{"sensitive_vaults": {"id", "token", "backup", "settings", "optional"}, "sensitive_accounts": {"id", "digest", "backup"}}
var insertColumns = regexp.MustCompile(`INSERT INTO "[^"]+" \(([^)]+)\)`)
var keyColumn = regexp.MustCompile(`"id" = \$([0-9]+)`)
var updateColumns = regexp.MustCompile(`"([a-z_]+)" = \$[0-9]+`)

func (s sensitiveConnection) QueryContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	table := ""
	for name := range sensitiveColumns {
		if strings.Contains(sql, `"`+name+`"`) {
			table = name
			break
		}
	}
	if table == "" {
		return nil, errors.New("unexpected table")
	}
	columns := sensitiveColumns[table]
	if s.rows == nil {
		s.rows = map[string]map[string][]driver.Value{}
	}
	if s.rows[table] == nil {
		s.rows[table] = map[string][]driver.Value{}
	}
	fmt.Fprintln(&s.transcript, sql)
	for _, arg := range args {
		if str, ok := arg.Value.(string); ok && strings.HasPrefix(str, "fg1:") {
			fmt.Fprint(&s.transcript, "encrypted:string ")
		} else {
			fmt.Fprintf(&s.transcript, "%T ", arg.Value)
		}
	}
	fmt.Fprintln(&s.transcript)
	if strings.HasPrefix(sql, "SELECT") && s.readBatch != nil {
		return &rows{columns: columns, batch: s.readBatch}, nil
	}
	var row []driver.Value
	if strings.HasPrefix(sql, "INSERT") {
		found := insertColumns.FindStringSubmatch(sql)
		if len(found) != 2 {
			return nil, errors.New("unexpected insert")
		}
		row = make([]driver.Value, len(columns))
		names := strings.Split(found[1], ",")
		for i, name := range names {
			name = strings.Trim(strings.TrimSpace(name), `"`)
			for j, col := range columns {
				if col == name {
					row[j] = args[i].Value
				}
			}
		}
		s.rows[table][row[0].(string)] = row
	} else {
		match := keyColumn.FindStringSubmatch(sql)
		if len(match) != 2 {
			return nil, errors.New("missing key predicate")
		}
		position, err := strconv.Atoi(match[1])
		if err != nil || position < 1 || position > len(args) {
			return nil, errors.New("invalid key placeholder")
		}
		id, ok := args[position-1].Value.(string)
		if !ok {
			return nil, errors.New("missing string key")
		}
		row = s.rows[table][id]
		if strings.HasPrefix(sql, "UPDATE") && row != nil {
			sets := strings.SplitN(sql, " WHERE ", 2)[0]
			names := updateColumns.FindAllStringSubmatch(sets, -1)
			for i, name := range names {
				for j, col := range columns {
					if col == name[1] {
						row[j] = args[i].Value
					}
				}
			}
		}
	}
	batch := [][]driver.Value{}
	if row != nil {
		batch = [][]driver.Value{append([]driver.Value(nil), row...)}
	}
	return &rows{columns: columns, batch: batch}, nil
}
