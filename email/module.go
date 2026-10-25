package email

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/emaildiag"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Module registers an owned mailer. Requires must name providers owning its
// borrowed transport and disks so sends drain before those resources close.
func Module(name foundation.ProviderID, key foundation.Key[*Mailer], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Mailer, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return emaildiag.Failure(Construction)
		}
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Mailer, error) {
			m, err := construct(r)
			if err != nil {
				return nil, err
			}
			if m == nil || m.done == nil {
				return nil, emaildiag.Failure(Construction)
			}
			return m, nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		m, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("email", func(ctx context.Context) error {
			err := m.Close(ctx)
			<-m.Done()
			return safeerror.Join("operation failed", err, m.Close(context.Background()))
		}); err != nil {
			return safeerror.Join("operation failed", err, m.Close(ctx))
		}
		return nil
	}}
}
