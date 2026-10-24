package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/prune"
)

type prunePrivateError struct{ formatted int }

func (e *prunePrivateError) Error() string {
	e.formatted++
	return "private stored member"
}

func TestORMPruneRegistryPreservesHookFailureIdentityAndRedaction(t *testing.T) {
	cause := &prunePrivateError{}
	f := n.Declare(func() orm.Hooks[n.Member] {
		return orm.Hooks[n.Member]{Deleting: func(context.Context, *database.Tx, n.Member) error { return cause }}
	})
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	g := &goldenCapture{reply: func(string, []driver.NamedValue) ([]string, [][]driver.Value) {
		return []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, [][]driver.Value{{int64(1), "private stored member", nil, []byte{1}, now, now, nil}}
	}}
	db := g.open(t.Context())
	defer db.Close(t.Context())
	registry, err := prune.New(prune.ORMSoftModel("app.members", f.Query(), f.Soft, prune.Lifecycle))
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Run(t.Context(), db, prune.Options{BatchSize: 2, MaxBatches: 1})
	var typed *prunePrivateError
	if !errors.Is(err, cause) || !errors.As(err, &typed) || typed != cause || result.StoppedAt == nil || len(result.Counts) != 1 || result.Counts[0].Removed != 0 {
		t.Fatal("registry lost failure identity or reported unconfirmed removals")
	}
	for _, rendered := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(rendered, "private stored member") {
			t.Fatal("registry exposed model text")
		}
	}
	if cause.formatted != 0 || strings.Contains(g.output.String(), "DELETE FROM") {
		t.Fatal("hook failure was formatted or reached deletion")
	}
}
