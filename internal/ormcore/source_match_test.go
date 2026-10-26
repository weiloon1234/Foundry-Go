package ormcore

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type sourceCountRow struct {
	count int64
	err   error
}

func (r sourceCountRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[len(dest)-1].(*int64) = r.count
	return nil
}
func TestSourceMatchDecoderRejectsAmbiguityAndPreservesScanErrors(t *testing.T) {
	sentinel := errors.New("scan failed")
	for _, tc := range []struct {
		name string
		row  sourceCountRow
		want error
	}{
		{"one", sourceCountRow{count: 1}, nil}, {"ambiguous", sourceCountRow{count: 2}, database.TooManyRows}, {"zero", sourceCountRow{}, fault.Invalid}, {"scan", sourceCountRow{err: sentinel}, sentinel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (SourceMatchRow{Row: tc.row}).Scan(new(string))
			if !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
			if tc.name == "scan" && err != sentinel {
				t.Fatal("scan error identity changed")
			}
		})
	}
}
