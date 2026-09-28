package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/challengestore"
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
		return challenge.Record{}, fault.New(fault.Invalid, "challenge issuance requires preparation")
	}
	var result challenge.Record
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, true)
		if err != nil {
			return err
		}
		if !present {
			return fault.New(fault.Internal, "challenge subject was not created")
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
		previous, err := entries(subject.Scope, subject.Key).First(ctx, tx)
		if err != nil {
			return err
		}
		if old, exists := previous.Get(); exists {
			if _, err := record(address, subject, old); err != nil {
				return err
			}
			if _, err := entries(subject.Scope, subject.Key).Delete(ctx, tx, old.ID); err != nil {
				return err
			}
		}
		id, err := model.NewID[challengestore.Entry]()
		if err != nil {
			return err
		}
		draft := challengestore.EntryDraft{}.SetID(id).SetScope(subject.Scope).SetSubjectKey(subject.Key).SetSecretHash(hash.Hex()).SetBindingHash(binding.Hex()).SetCreatedAt(now).SetExpiresAt(expiry)
		row, err := challengestore.QueryFoundryChallenges().Create(ctx, tx, draft)
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
