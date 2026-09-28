package database

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type observerBinding struct{}

type observerContribution struct {
	db          *DB
	declaration lifecycle.Declaration
}

// RegisterObserver contributes a model-owned observer to a database Module.
// This shared registration boundary retains the model/hook pair. Dependencies
// are resolved once during application construction; the resulting factory is
// retained for per-operation hook construction. Neither stage acquires a pool
// connection. Names are unique within the selected database, across models.
func RegisterObserver[M, H any](r *foundation.Registrar, pool foundation.Key[*DB], observer lifecycle.Observer[M, H], construct func(foundation.Resolver) (func() H, error)) error {
	if err := observer.Validate(); err != nil {
		return err
	}
	if construct == nil {
		return fault.New(fault.Invalid, "model observer needs a dependency constructor")
	}
	key := foundation.NewKey[observerContribution](fmt.Sprintf("foundry.database.observer.%q.%q", pool.Name(), observer.Name()))
	return foundation.Factory(r, key, func(resolver foundation.Resolver) (observerContribution, error) {
		db, err := foundation.Resolve(resolver, pool)
		if err != nil {
			return observerContribution{}, err
		}
		db.mu.Lock()
		managed := db.managedObservers
		db.mu.Unlock()
		if !managed {
			return observerContribution{}, fault.New(fault.Invalid, "registered observers require a database Module; prepared pools use BindObservers")
		}
		factory, err := construct(resolver)
		if err != nil {
			return observerContribution{}, err
		}
		declaration, err := observer.Declare(factory)
		return observerContribution{db: db, declaration: declaration}, err
	})
}

// registerObserverBinding uses the existing eager construction graph after the
// pure DB constructor. Observer constructors may depend on that DB without a
// DB -> observers -> DB cycle. Binding finishes before any provider can boot.
func registerObserverBinding(r *foundation.Registrar, pool foundation.Key[*DB]) error {
	key := foundation.NewKey[observerBinding](fmt.Sprintf("foundry.database.observers.%q", pool.Name()))
	return foundation.Factory(r, key, func(resolver foundation.Resolver) (observerBinding, error) {
		db, err := foundation.Resolve(resolver, pool)
		if err != nil {
			return observerBinding{}, err
		}
		contributions, err := foundation.ResolveAll[observerContribution](resolver)
		if err != nil {
			return observerBinding{}, err
		}
		var declarations []lifecycle.Declaration
		for _, contribution := range contributions {
			if contribution.db == db {
				declarations = append(declarations, contribution.declaration)
			}
		}
		set, err := lifecycle.NewObservers(declarations...)
		if err != nil {
			return observerBinding{}, err
		}
		return observerBinding{}, db.bindObservers(set, true)
	})
}

// BindObservers attaches an immutable set to a directly prepared pool exactly
// once, before Start. Database Modules bind provider contributions automatically
// during Build instead. Binding never starts resources or invokes hook factories.
func (db *DB) BindObservers(set lifecycle.Observers) error { return db.bindObservers(set, false) }

func (db *DB) bindObservers(set lifecycle.Observers, managed bool) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closing || db.startAttempted || db.observersBound {
		return fault.New(fault.Closed, "database observer binding is frozen")
	}
	if db.managedObservers != managed {
		return fault.New(fault.Invalid, "database Module owns its observer binding")
	}
	db.observers, db.observersBound = set, true
	return nil
}

// Observers returns this database's immutable observer set. A directly prepared
// pool may bind it before Start; running pools retain the same set for life.
func (db *DB) Observers() lifecycle.Observers {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.observers
}

// Observers retains the owning database's declarations on this session.
func (s *Session) Observers() lifecycle.Observers { return s.observers }

// Observers retains the owning database's declarations on transactions and
// nested savepoints. Custom transactors must use this supplied Tx's ownership.
func (tx *Tx) Observers() lifecycle.Observers { return tx.observers }
