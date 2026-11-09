package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

func TestPoolClosePreservesFailure(t *testing.T) {
	cause := errors.New("test connection cleanup failed")
	state := &driverState{closeHook: func() error { return cause }}
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	for range 2 {
		err := db.Close(t.Context())
		if !errors.Is(err, cause) || !errors.Is(err, database.QueryFailed) {
			t.Fatalf("close lost genuine failure: %v", err)
		}
	}
	if state.closed.Load() != state.connected.Load() {
		t.Fatal("close failure prevented connection cleanup")
	}
}
