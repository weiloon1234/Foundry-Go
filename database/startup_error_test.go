package database_test

import (
	"errors"
	"fmt"
	foundry "github.com/weiloon1234/Foundry-Go"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type startupPrivateError struct {
	mode  string
	calls int
}

func (e *startupPrivateError) Error() string {
	e.calls++
	if e.mode == "exit" {
		runtime.Goexit()
	}
	if e.mode == "panic" {
		panic("private startup")
	}
	return "private startup"
}
func (e *startupPrivateError) Format(fmt.State, rune) { _ = e.Error() }
func TestAdapterFactoriesNeverFormatApplicationErrors(t *testing.T) {
	for _, mode := range []string{"private", "panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			for _, route := range []string{"primary", "read"} {
				t.Run(route, func(t *testing.T) {
					cause := &startupPrivateError{mode: mode}
					factory := func() (database.Adapter, error) { return database.Adapter{}, cause }
					var err error
					if route == "read" {
						config := database.DefaultPoolConfig()
						config.MaxOpen, config.MaxIdle = 2, 1
						_, err = database.Prepare(database.Adapter{Connector: connector{&driverState{}}}, config, database.WithReadPool(factory, config, 4))
					} else {
						key := foundation.NewKey[*database.DB]("test.database")
						_, err = foundry.New().Register(database.Module("test.database", key, factory, database.DefaultPoolConfig())).Build(t.Context())
					}
					var retained *startupPrivateError
					if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
						t.Fatal("factory cause lost")
					}
					_ = fmt.Sprintf("%v %+v %#v", err, err, err)
					if cause.calls != 0 {
						t.Fatal("factory error formatted")
					}
				})
			}
		})
	}
}
