package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/challengestore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func lockSubject(ctx context.Context, tx *database.Tx, address challenge.Address, identity model.Identity, create bool) (challengestore.Subject, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return challengestore.Subject{}, false, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return challengestore.Subject{}, false, err
	}
	if create {
		data, err := value.NewJSON(identity)
		if err != nil {
			return challengestore.Subject{}, false, err
		}
		draft := []orm.Assignment[challengestore.Subject]{challengestore.Subjects.Key().Set(key), challengestore.Subjects.Scope.Set(scope), challengestore.Subjects.Identity.Set(data)}
		if _, err := orm.Upsert(ctx, tx, challengestore.Subjects.Query(), orm.OnConflict(orm.ConflictKey(challengestore.Subjects.Key())).DoNothing(), draft...); err != nil {
			return challengestore.Subject{}, false, err
		}
	}
	return subjectByKey(ctx, tx, address, key)
}
func subjectByKey(ctx context.Context, tx *database.Tx, address challenge.Address, key string) (challengestore.Subject, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return challengestore.Subject{}, false, err
	}
	found, err := challengestore.Subjects.Query().Where(challengestore.Subjects.Scope.Eq(scope)).Where(challengestore.Subjects.Key().Eq(key)).ForUpdate().First(ctx, tx)
	if err != nil {
		return challengestore.Subject{}, false, err
	}
	subject, present := found.Get()
	if !present {
		return challengestore.Subject{}, false, nil
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return challengestore.Subject{}, false, err
	}
	expected, err := address.SubjectKey(identity)
	if err != nil {
		return challengestore.Subject{}, false, err
	}
	if subject.Key != key || key != expected || subject.Scope != scope {
		return challengestore.Subject{}, false, fault.New(fault.Invalid, "stored challenge subject is inconsistent")
	}
	return subject, true, nil
}
func record(address challenge.Address, subject challengestore.Subject, row challengestore.Entry) (challenge.Record, error) {
	scope, err := address.Key()
	if err != nil {
		return challenge.Record{}, err
	}
	if row.ID.IsZero() || subject.Scope != scope || row.Scope != scope || row.SubjectKey != subject.Key {
		return challenge.Record{}, fault.New(fault.Invalid, "stored challenge does not match its subject")
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return challenge.Record{}, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return challenge.Record{}, err
	}
	if key != subject.Key {
		return challenge.Record{}, fault.New(fault.Invalid, "stored challenge identity is inconsistent")
	}
	hash, err := challenge.ParseDigest(row.SecretHash)
	if err != nil {
		return challenge.Record{}, err
	}
	binding, err := challenge.ParseBinding(row.BindingHash)
	if err != nil {
		return challenge.Record{}, err
	}
	result := challenge.Record{Address: address, Subject: identity, Hash: hash, Binding: binding, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
	return result, result.Validate(address)
}
