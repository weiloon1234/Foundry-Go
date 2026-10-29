package goway_test

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"strings"
	"testing"
)

func TestRelationWriteMorphDefaultsAndSingleStatements(t *testing.T) {
	for _, mode := range []string{"attach", "detach", "force", "morph"} {
		t.Run(mode, func(t *testing.T) {
			g := &goldenCapture{reply: pivotWriteReply}
			db := g.open(t.Context())
			defer db.Close(context.Background())
			var err error
			switch mode {
			case "attach":
				_, err = orm.Attach(t.Context(), db, j.Peers, j.Parent{ID: 1}, j.Target{ID: 10}, orm.KeySet(j.Pivots.ID, 100), orm.ColumnSet(j.Pivots.Kind, "parent"), orm.TextSet(j.Pivots.Role, "member"))
			case "morph":
				_, err = orm.Attach(t.Context(), db, j.MorphPeers, j.Parent{ID: 1}, j.Target{ID: 10}, orm.KeySet(j.Pivots.ID, 100), orm.TextSet(j.Pivots.Role, "member"))
			case "detach":
				_, err = orm.Detach(t.Context(), db, j.Peers, j.Parent{ID: 1}, j.Target{ID: 10})
			case "force":
				_, err = orm.ForceDetach(t.Context(), db, j.Peers.WithTrashedPivot(), j.Pivots.Soft, j.Parent{ID: 1}, j.Target{ID: 10})
			}
			if err != nil {
				t.Fatal(err, g.output.String())
			}
			want := 3
			if mode == "attach" || mode == "morph" {
				want = 4
			}
			if g.statements != want {
				t.Fatal(g.statements, want)
			}
			if mode == "morph" && !strings.Contains(g.output.String(), oracleSnapshot("parent")) {
				t.Fatal("missing morph default")
			}
		})
	}
}
