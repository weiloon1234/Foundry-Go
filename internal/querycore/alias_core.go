package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"reflect"
)

// AliasNode preserves source windows and filters inside their SELECT boundary.
func AliasNode(node SelectNode, columns []Column, direct bool, alias string, identity reflect.Type, err error) JoinedNode {
	if !sqlname.Valid(alias) {
		return JoinedNode{Err: fault.New(fault.Invalid, "alias requires a record query and valid name")}
	}
	if err != nil {
		return JoinedNode{Err: err}
	}
	table := TableSource{Alias: alias, Columns: columns, Query: &node}
	if direct && len(node.Locks) == 0 {
		table = node.Source
		table.Alias = alias
	}
	return JoinedNode{Node: SelectNode{Source: table}, Aliases: map[string]reflect.Type{alias: identity}}
}
