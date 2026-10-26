package ormcore

import (
	"database/sql"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

type transferJoinedRow struct {
	calls, failAt int
	err           error
}

func (r *transferJoinedRow) Scan(ds ...any) error {
	r.calls++
	if r.calls == r.failAt {
		return r.err
	}
	for i, d := range ds {
		if e := d.(sql.Scanner).Scan(int64(i + 1)); e != nil {
			return e
		}
	}
	return nil
}
func TestRelationRowPartitionsAndValidation(t *testing.T) {
	row := &transferJoinedRow{}
	var target, pivot int64
	if e := (RelationRow{Row: row, Count: 1, Total: 2}).Scan(codec.Signed[int64]().Scan(&target)); e != nil {
		t.Fatal(e)
	}
	if e := (RelationRow{Row: row, Start: 1, Count: 1, Total: 2}).Scan(codec.Signed[int64]().Scan(&pivot)); e != nil || target != 1 || pivot != 2 || row.calls != 2 {
		t.Fatal(target, pivot, row.calls, e)
	}
	sentinel := errors.New("pivot decoder failed")
	row = &transferJoinedRow{failAt: 1, err: sentinel}
	if e := (RelationRow{Row: row, Start: 1, Count: 1, Total: 2}).Scan(codec.Signed[int64]().Scan(&pivot)); !errors.Is(e, sentinel) {
		t.Fatal("scan identity", e)
	}
	var raw sql.RawBytes
	if e := (RelationRow{Row: row, Count: 1, Total: 1}).Scan(&raw); !errors.Is(e, fault.Invalid) {
		t.Fatal("borrowed bytes", e)
	}
	if e := (RelationRow{Row: row, Count: 2, Total: 2}).Scan(codec.Signed[int64]().Scan(&target)); !errors.Is(e, fault.Invalid) {
		t.Fatal("arity", e)
	}
}
