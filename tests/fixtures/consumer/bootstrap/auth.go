package bootstrap

import (
	"context"
	"crypto/subtle"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// StaticProof is an acceptance-only credential verifier, supplied explicitly by
// the test/operator. Production applications use persistent session/token services.
func staticProof[M model.Identifiable, K any](expected secret.String, reference model.Reference[M, K]) func(context.Context, secret.String) (value.Optional[auth.Proof[M, K]], error) {
	return func(ctx context.Context, actual secret.String) (value.Optional[auth.Proof[M, K]], error) {
		if err := ctx.Err(); err != nil {
			return value.Optional[auth.Proof[M, K]]{}, err
		}
		if subtle.ConstantTimeCompare([]byte(expected.Reveal()), []byte(actual.Reveal())) != 1 {
			return value.Optional[auth.Proof[M, K]]{}, nil
		}
		proof, err := auth.NewProof(reference, auth.Authenticated)
		if err != nil {
			return value.Optional[auth.Proof[M, K]]{}, err
		}
		return value.Set(proof), nil
	}
}
func withinSchema(ctx context.Context, db *database.DB, schema string, run func(*database.Tx) error) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('search_path',quote_ident($1),true)", schema); err != nil {
			return err
		}
		return run(tx)
	})
}
func actors(settings Settings, db *database.DB) (http.GuardBinding[Member], http.GuardBinding[Operator], error) {
	memberProvider := auth.DefineProvider("bootstrap.members", (Member{}).FoundryReference(), func(ctx context.Context, id model.ID[Member]) (result value.Optional[Member], err error) {
		err = withinSchema(ctx, db, settings.Schema, func(tx *database.Tx) error { result, err = QueryBootstrapMembers().Find(ctx, tx, id); return err })
		return
	}, func(context.Context, Member) (bool, error) { return true, nil })
	operatorProvider := auth.DefineProvider("bootstrap.operators", (Operator{}).FoundryReference(), func(ctx context.Context, id model.ID[Operator]) (result value.Optional[Operator], err error) {
		err = withinSchema(ctx, db, settings.Schema, func(tx *database.Tx) error { result, err = QueryBootstrapOperators().Find(ctx, tx, id); return err })
		return
	}, func(context.Context, Operator) (bool, error) { return true, nil })
	member := auth.DefineGuard("bootstrap.member", memberProvider, auth.DefineStrategy("member.cookie", staticProof(settings.MemberToken, (Member{ID: settings.MemberID}).FoundryReference())))
	operator := auth.DefineGuard("bootstrap.operator", operatorProvider, auth.DefineStrategy("operator.bearer", staticProof(settings.OperatorToken, (Operator{ID: settings.OperatorID}).FoundryReference())))
	registry, err := auth.NewRegistry(auth.DefaultConfig(), member.Registration(), operator.Registration())
	if err != nil {
		return http.GuardBinding[Member]{}, http.GuardBinding[Operator]{}, err
	}
	cookie := http.DefineCookie("bootstrap_member", http.SecretCookie(), http.DefaultCookieOptions())
	browser, err := http.NewCookieAuthentication(registry, http.CSRFConfig{}, http.CookieCredential("member.cookie", cookie))
	if err != nil {
		return http.GuardBinding[Member]{}, http.GuardBinding[Operator]{}, err
	}
	api, err := http.NewAuthentication(registry, http.BearerCredential("operator.bearer"))
	if err != nil {
		return http.GuardBinding[Member]{}, http.GuardBinding[Operator]{}, err
	}
	members, err := http.BindGuard(browser, member)
	if err != nil {
		return members, http.GuardBinding[Operator]{}, err
	}
	operators, err := http.BindGuard(api, operator)
	return members, operators, err
}
