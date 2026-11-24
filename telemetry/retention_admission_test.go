package telemetry

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestEmptyRunProtectionQueryCompiles(t *testing.T) {
	now, err := temporal.NewDateTime(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := &storage{config: engineConfig()}
	for _, limit := range []int{1, maxPruneRuns} {
		query, err := s.emptyRunReclaimQuery(now, limit)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := orm.Compile(query); err != nil {
			t.Fatal(err)
		}
	}
}
