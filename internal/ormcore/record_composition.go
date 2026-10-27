package ormcore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// CommonTable keeps the declaration identity and deferred checks with its definition.
type CommonTable struct {
	Definition *querycore.CteNode
	Record     RecordSource
}

func NewCommonTable(name string, r RecordSource) CommonTable {
	r.Source.Err = safeerror.Join("operation failed", r.Source.Err, ValidateRecordSource(r, r.Record.Type, r.Transaction))
	if !sqlname.Valid(name) {
		r.Source.Err = safeerror.Join("operation failed", r.Source.Err, Invalid("CTE requires a valid name and record query"))
	}
	return CommonTable{Definition: &querycore.CteNode{Name: name, Query: r.Source.Node, Columns: slices.Clone(r.Record.Columns)}, Record: r}
}

func Materialize(c CommonTable, mode querycore.CteMaterialization) CommonTable {
	if c.Definition != nil && c.Definition.Materialization != mode {
		d := *c.Definition
		d.Materialization = mode
		c.Definition = &d
	}
	return c
}

func CommonTableRecord(c CommonTable) RecordSource {
	r := c.Record
	if c.Definition == nil {
		r.Source.Err = safeerror.Join("operation failed", r.Source.Err, Invalid("CTE requires a definition"))
		return r
	}
	d := c.Definition
	r.Source.Node = querycore.SelectNode{Source: querycore.TableSource{Cte: d, Columns: d.Columns}, Selections: querycore.ColumnSelections(d.Name, d.Columns)}
	r.Source.Scope = nil
	r.Source.Aliases = nil
	r.Source.Owners = nil
	r.Direct = true
	return r
}

// RecursiveRecord is a non-executable working table; the compiler owns its lifetime.
func RecursiveRecord(name string, anchor RecordSource) (RecordSource, *querycore.RecursiveReference) {
	ref := &querycore.RecursiveReference{Name: name, Columns: slices.Clone(anchor.Record.Columns)}
	r := anchor
	r.Source = Source{Node: querycore.SelectNode{Source: querycore.TableSource{Self: ref, Columns: ref.Columns}, Selections: querycore.ColumnSelections(name, ref.Columns)}, Err: anchor.Source.Err, Checks: AppendChecks(anchor.Source.Checks)}
	r.Direct = true
	return r, ref
}

func CompleteRecursion(c CommonTable, step RecordSource, ref *querycore.RecursiveReference, operator querycore.SetOperator) CommonTable {
	c.Record.Source.Err = safeerror.Join("operation failed", c.Record.Source.Err, CompatibleRecordSources(c.Record, step))
	c.Record.Source.Checks = AppendChecks(c.Record.Source.Checks, step.Source.Checks...)
	c.Record.Source.Checks = AppendChecks(c.Record.Source.Checks, step.Record.Checks...)
	if c.Definition != nil {
		d := *c.Definition
		d.Recursion = &querycore.RecursiveCTE{Reference: ref, Step: step.Source.Node, Operator: operator}
		c.Definition = &d
	}
	return c
}

func CompatibleRecordSources(a, b RecordSource) error {
	err := querycore.CompatibleRecords(
		querycore.RecordLayout{Node: a.Source.Node, Columns: a.Record.Columns, HasDecoder: a.Record.Scan != nil, Err: ValidateRecordSource(a, a.Record.Type, a.Transaction)},
		querycore.RecordLayout{Node: b.Source.Node, Columns: b.Record.Columns, HasDecoder: b.Record.Scan != nil, Err: ValidateRecordSource(b, a.Record.Type, a.Transaction)},
	)
	return err
}

func CombineRecords(a, b RecordSource, operator querycore.SetOperator, scope reflect.Type) RecordSource {
	err := CompatibleRecordSources(a, b)
	if operator > querycore.ExceptAllSet {
		err = safeerror.Join("operation failed", err, Invalid("invalid set operator"))
	}
	r := RecordSource{Record: a.Record, Transaction: a.Transaction, Direct: true}
	r.Source = Source{Scope: scope, Node: querycore.SelectNode{Source: querycore.TableSource{Set: &querycore.SetNode{Left: a.Source.Node, Right: b.Source.Node, Operator: operator}, Alias: querycore.SetAlias, Columns: slices.Clone(a.Record.Columns)}, Selections: querycore.ColumnSelections(querycore.SetAlias, a.Record.Columns)}, Aliases: map[string]reflect.Type{querycore.SetAlias: scope}, Owners: map[string]any{querycore.SetAlias: a.Record.Owner}, Err: err, Checks: AppendChecks(a.Source.Checks, b.Source.Checks...)}
	r.Source.Checks = AppendChecks(r.Source.Checks, b.Record.Checks...)
	return r
}

func SetRecord(r RecordSource) RecordSource {
	if r.Source.Node.Source.Set == nil {
		r.Source.Err = safeerror.Join("operation failed", r.Source.Err, Invalid("set requires two complete inputs"))
	}
	r.Direct = DirectRecord(r.Source)
	return r
}
