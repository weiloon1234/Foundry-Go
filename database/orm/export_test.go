package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type scopeTenant struct{}

var tenantMissing = errors.New("missing private tenant")

func tenantContext(ctx context.Context, tenant int64) context.Context {
	return context.WithValue(ctx, scopeTenant{}, tenant)
}

type rawRecord struct {
	ID   int64
	Name string
}

func rawRecords() Table[rawRecord, int64] {
	return Define("raw_records", func(b Builder[rawRecord], m *rawRecord) Table[rawRecord, int64] {
		TextOf(b, &m.Name)
		return TableOf(b, &m.ID)
	})
}

type secureRecord struct {
	ID     model.ID[secureRecord]
	Secret encrypted.Text
	Backup value.Nullable[encrypted.Text]
}
type secureTable struct {
	Table[secureRecord, model.ID[secureRecord]]
	Secret Encrypted[secureRecord, encrypted.Text]
	Backup NullableEncrypted[secureRecord, encrypted.Text]
}

func secureRecords() secureTable { return secureRecordsAt("secure_records") }
func secureRecordsAt(name string) secureTable {
	return Define(name, func(b Builder[secureRecord], m *secureRecord) secureTable {
		return secureTable{TableOf(b, &m.ID), EncryptedTextOf(b, &m.Secret), NullableEncryptedTextOf(b, &m.Backup)}
	})
}

type uniqueTransferExecutor struct {
	database.Executor
	calls int
	sql   string
	args  []any
	cause error
}

func (e *uniqueTransferExecutor) Query(_ context.Context, s string, args ...any) (*database.Rows, error) {
	e.calls++
	e.sql = s
	e.args = append([]any(nil), args...)
	return nil, e.cause
}

type writeParent struct {
	ID    int64
	Code  int64
	Links relation.Through[writeTarget, writePivot]
}
type writeTarget struct {
	ID   int64
	Code int64
}
type writePivot struct {
	ID        int64
	ParentID  int64
	TargetID  int64
	Role      string
	DeletedAt value.Nullable[temporal.DateTime]
}
type writeParentTable struct {
	Table[writeParent, int64]
	ID    Key[writeParent, int64]
	Code  Number[writeParent, int64]
	Links Through[writeParent, writeTarget, writePivot]
}
type writeTargetTable struct {
	Table[writeTarget, int64]
	ID   Key[writeTarget, int64]
	Code Number[writeTarget, int64]
}
type writePivotTable struct {
	Table[writePivot, int64]
	ID        Key[writePivot, int64]
	ParentID  Number[writePivot, int64]
	TargetID  Number[writePivot, int64]
	Role      Text[writePivot, string]
	DeletedAt SoftDelete[writePivot, temporal.DateTime]
}

func writeRelationTables(opts ...Option) (writeParentTable, writeTargetTable, writePivotTable, ThroughRelation[writeParent, writeTarget, writePivot]) {
	a := Define("write_parents", func(b Builder[writeParent], m *writeParent) writeParentTable {
		t := TableOf(b, &m.ID)
		return writeParentTable{t, t.Key(), NumberOf(b, &m.Code), ThroughOf(b, &m.Links)}
	})
	z := Define("write_targets", func(b Builder[writeTarget], m *writeTarget) writeTargetTable {
		t := TableOf(b, &m.ID)
		return writeTargetTable{t, t.Key(), NumberOf(b, &m.Code)}
	})
	p := Define("write_pivots", func(b Builder[writePivot], m *writePivot) writePivotTable {
		t := TableOf(b, &m.ID)
		return writePivotTable{t, t.Key(), NumberOf(b, &m.ParentID), NumberOf(b, &m.TargetID), TextOf(b, &m.Role), SoftDeleteOf(b, &m.DeletedAt)}
	}, opts...)
	return a, z, p, ManyToManyOf(a.Links, KeyExpr(a.ID), NumberExpr(p.ParentID), NumberExpr(p.TargetID), KeyExpr(z.ID))
}

func pivotTestRows(n int) [][]driver.Value {
	out := make([][]driver.Value, n)
	for i := range out {
		out[i] = []driver.Value{int64(100 + i), int64(1), int64(10 + i), "member", nil}
	}
	return out
}

func pivotTestReply(n int) func(string) ([]string, [][]driver.Value) {
	return func(s string) ([]string, [][]driver.Value) {
		if strings.HasPrefix(s, "SELECT EXISTS(") {
			return []string{"exists"}, [][]driver.Value{{true}}
		}
		if strings.HasPrefix(s, "SELECT COUNT(*) FROM (") {
			return []string{"count"}, [][]driver.Value{{int64(n)}}
		}
		if strings.Contains(s, `FROM "write_parents"`) {
			return make([]string, 2), [][]driver.Value{{int64(1), int64(91)}}
		}
		if strings.Contains(s, `FROM "write_targets"`) {
			rows := make([][]driver.Value, n)
			for i := range rows {
				rows[i] = []driver.Value{int64(10 + i), int64(81 + i)}
			}
			return make([]string, 2), rows
		}
		return make([]string, 5), pivotTestRows(n)
	}
}

func ConflictError[M any](c Conflict[M]) error { return c.state.err }

func ConflictUpdateError[M any](u ConflictUpdate[M]) error { return u.state.err }

func NewTestWriteError[M any](cause error, candidate M) *WriteError[M] {
	return &WriteError[M]{cause: cause, candidate: candidate}
}

func NewTestAssignment[M any](err error) Assignment[M] {
	return Assignment[M]{err: err}
}
