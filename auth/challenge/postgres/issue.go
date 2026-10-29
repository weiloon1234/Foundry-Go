package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/challengestore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func (b *Backend) Issue(ctx context.Context, address challenge.Address, identity model.Identity, hash challenge.Digest, lifetime time.Duration, prepare func(context.Context, *database.Tx) (challenge.Binding, error)) (challenge.Record, error) {
	if err := validateCredential(address, hash); err != nil {
		return challenge.Record{}, err
	}
	if _, err := address.SubjectKey(identity); err != nil {
		return challenge.Record{}, err
	}
	if err := challenge.ValidateLifetime(lifetime); err != nil {
		return challenge.Record{}, err
	}
	if prepare == nil {
		return challenge.Record{}, safeerror.NewFault(fault.Invalid, "challenge issuance requires preparation")
	}
	var result challenge.Record
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, true)
		if err != nil {
			return err
		}
		if !present {
			return safeerror.NewFault(fault.Internal, "challenge subject was not created")
		}
		binding, err := prepare(ctx, tx)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		expiry, err := temporal.NewDateTime(now.UTC().Add(lifetime))
		if err != nil {
			return err
		}
		candidate := challenge.Record{Address: address, Subject: identity, Hash: hash, Binding: binding, CreatedAt: now, ExpiresAt: expiry}
		if err := candidate.Validate(address); err != nil {
			return err
		}
		previous, err := orm.First(ctx, tx, entries(subject.Scope, subject.Key))
		if err != nil {
			return err
		}
		if old, exists := previous.Get(); exists {
			if _, err := record(address, subject, old); err != nil {
				return err
			}
			if _, err := orm.Delete(ctx, tx, entries(subject.Scope, subject.Key), challengestore.Entries.ID, old.ID); err != nil {
				return err
			}
		}
		id, err := model.NewIDAt[challengestore.Entry](time.Now())
		if err != nil {
			return err
		}
		draft := []orm.Assignment[challengestore.Entry]{
			orm.KeySet(challengestore.Entries.ID, id),
			orm.TextSet(challengestore.Entries.Scope, subject.Scope),
			orm.TextSet(challengestore.Entries.SubjectKey, subject.Key),
			orm.TextSet(challengestore.Entries.SecretHash, hash.Hex()),
			orm.TextSet(challengestore.Entries.BindingHash, binding.Hex()),
			orm.TimeSet(challengestore.Entries.CreatedAt, now),
			orm.TimeSet(challengestore.Entries.ExpiresAt, expiry),
		}
		row, err := orm.Insert(ctx, tx, challengestore.Entries.Table, draft...)
		if err != nil {
			return err
		}
		result, err = record(address, subject, row)
		return err
	})
	if err != nil {
		return challenge.Record{}, err
	}
	return result, nil
}
