package query

import "github.com/weiloon1234/Foundry-Go/fault"

// TransactionScope marks expression owners in transaction-required query composition.
// Source interfaces and execution checks preserve the transaction boundary as well.
type TransactionScope interface{ transactionScope() }

// TransactionAlias identifies one aliased record in transaction-required composition.
type TransactionAlias[A, R any] struct {
	_ [0]*A
	_ [0]*R
	_ [0]struct{ transaction bool }
}

func (TransactionAlias[A, R]) transactionScope() {}

// TransactionRecordSource carries a complete model or declared result and all
// nested locks. It cannot be supplied to ordinary alias, CTE or query sources.
type TransactionRecordSource[R any] interface{ transactionRecord() recordQuery[R] }

// TransactionProjectionSource owns fields for a transaction-required selection.
type TransactionProjectionSource[S TransactionScope] interface{ transactionProjection() projectionSource[S] }

// TransactionRecord lifts an ordinary complete record query into transaction
// composition. Use AsTransaction or TransactionCTE before selecting its fields.
type TransactionRecord[R any] struct{ record recordQuery[R] }

// TransactionOf explicitly lifts an ordinary complete record query. Existing
// filters, order, limits and complete generated metadata remain intact.
func TransactionOf[R any](source RecordQuerySource[R]) TransactionRecord[R] {
	if nilDescriptor(source) {
		return TransactionRecord[R]{record: recordQuery[R]{err: fault.New(fault.Invalid, "transaction record requires a source")}}
	}
	return TransactionRecord[R]{record: source.recordQuery()}
}
func (r TransactionRecord[R]) transactionRecord() recordQuery[R] { return r.record }

func (q LockedQuery[M]) transactionRecord() recordQuery[M] {
	r := q.query.recordQuery()
	r.node.locks = []lockSpec{q.lock}
	r.directSource = false
	return r
}
func (q LockedResult[S, R]) transactionRecord() recordQuery[R] {
	r := q.query.recordQuery()
	if len(q.locks) == 0 && r.err == nil {
		r.err = fault.New(fault.Invalid, "locked result requires at least one lock clause")
	}
	r.node.locks = q.locks
	r.directSource = false
	return r
}

// These private adapters reuse existing metadata and selection code without
// giving a public transaction source unrestricted source methods.
type transactionRecordAdapter[R any] struct{ record recordQuery[R] }

func (a transactionRecordAdapter[R]) recordQuery() recordQuery[R] { return a.record }

type transactionProjectionAdapter[S TransactionScope] struct{ source projectionSource[S] }

func (a transactionProjectionAdapter[S]) projectionSource() projectionSource[S] { return a.source }

func transactionProjection[S TransactionScope](source TransactionProjectionSource[S]) transactionProjectionAdapter[S] {
	if nilDescriptor(source) {
		return transactionProjectionAdapter[S]{source: projectionSource[S]{err: fault.New(fault.Invalid, "transaction selection requires a source")}}
	}
	return transactionProjectionAdapter[S]{source: source.transactionProjection()}
}
