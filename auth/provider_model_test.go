package auth_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type recoveryModel struct{ ID int64 }

func (m recoveryModel) reference() model.Reference[recoveryModel, int64] {
	return model.NewReference[recoveryModel]("recovery_provider_members", m.ID, codec.Signed[int64]())
}
func (m recoveryModel) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }
func TestProviderChecksExistingModelWithoutSecondLookup(t *testing.T) {
	m := recoveryModel{ID: 42}
	for _, mode := range []string{"allowed", "disabled", "error", "panic", "goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("eligibility failed")
			provider := auth.DefineProvider("recovery.members", recoveryModel{}.reference(), func(context.Context, int64) (value.Optional[recoveryModel], error) {
				t.Error("unwanted lookup")
				return value.Optional[recoveryModel]{}, nil
			}, func(context.Context, recoveryModel) (bool, error) {
				switch mode {
				case "disabled":
					return false, nil
				case "error":
					return false, cause
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					cancel()
				}
				return true, nil
			})
			reference, err := provider.CheckModel(ctx, m)
			if mode == "allowed" {
				if err != nil || reference.Key() != 42 {
					t.Fatal("model check", err)
				}
				return
			}
			if err == nil || reference.Key() != 0 {
				t.Fatal("failure returned reference")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if mode == "disabled" && !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("eligibility rejection", err)
			}
		})
	}
}
