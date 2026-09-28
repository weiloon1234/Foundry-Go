package query

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

func orderedCTEs(root selectNode) ([]*cteNode, error) {
	names := make(map[string]*cteNode)
	tables := make(map[string]bool)
	active, visited := make(map[*cteNode]bool), make(map[*cteNode]bool)
	var order []*cteNode
	var walk selectWalk
	var self *recursiveReference
	selfCount := 0
	walk.source = func(s tableSource, depth int) {
		if s.self != nil {
			if s.self != self {
				walk.err = fault.New(fault.Invalid, "recursive reference is outside its owning step")
			} else if walk.recursiveRestriction != "" {
				walk.err = fault.New(fault.Invalid, "recursive reference is not allowed in "+walk.recursiveRestriction)
			} else {
				selfCount++
				if selfCount > 1 {
					walk.err = fault.New(fault.Invalid, "recursive step must reference its working table exactly once")
				}
			}
			return
		}
		if s.cte == nil {
			if s.query == nil && s.set == nil {
				tables[s.table] = true
			}
			return
		}
		d := s.cte
		if !sqlname.Valid(d.name) || d.materialization > cteNotMaterialized || len(d.columns) == 0 || len(d.columns) != len(d.query.selections) {
			walk.err = fault.New(fault.Invalid, "invalid CTE definition")
			return
		}
		if other := names[d.name]; other != nil && other != d {
			walk.err = fault.New(fault.Invalid, "statement repeats a CTE name with different definitions")
			return
		}
		if active[d] {
			walk.err = fault.New(fault.Invalid, "cyclic CTE dependency requires an explicit recursive query")
			return
		}
		if visited[d] {
			return
		}
		names[d.name], active[d] = d, true
		// Dependencies define independent queries. They cannot capture the
		// working table of a CTE that happens to reference them.
		previousSelf, previousCount, previousRestriction := self, selfCount, walk.recursiveRestriction
		self, selfCount, walk.recursiveRestriction = nil, 0, ""
		walk.selectNode(d.query, depth+1)
		if r := d.recursion; r != nil && walk.err == nil {
			if r.reference == nil || r.reference.name != d.name || !slices.Equal(r.reference.columns, d.columns) || len(r.step.selections) != len(d.columns) || r.operator > unionAllSet || d.materialization == cteNotMaterialized {
				walk.err = fault.New(fault.Invalid, "invalid recursive CTE definition or materialization")
			} else {
				self, selfCount = r.reference, 0
				walk.selectNode(r.step, depth+1)
				if walk.err == nil && selfCount != 1 {
					walk.err = fault.New(fault.Invalid, "recursive step must reference its working table exactly once")
				}
			}
		}
		self, selfCount, walk.recursiveRestriction = previousSelf, previousCount, previousRestriction
		active[d], visited[d] = false, true
		order = append(order, d)
	}
	walk.selectNode(root, 0)
	if walk.err != nil {
		return nil, walk.err
	}
	for name := range names {
		if tables[name] {
			return nil, fault.New(fault.Invalid, "CTE name shadows a physical table reference; choose another CTE name or qualify the table")
		}
	}
	return order, nil
}

// Definitions are statement-local and independent of row correlation. References
// in nested SELECTs use this registry without inheriting surrounding row fields.
func (c *compiler) compileCTEs(node selectNode) (string, error) {
	definitions, err := orderedCTEs(node)
	if err != nil {
		return "", err
	}
	if len(definitions) == 0 {
		return "", nil
	}
	c.ctes = make(map[string]*cteNode, len(definitions))
	parts := make([]string, 0, len(definitions))
	recursive := false
	for _, d := range definitions {
		body, err := c.cteBody(d)
		if err != nil {
			return "", err
		}
		columns := make([]string, len(d.columns))
		for i, column := range d.columns {
			if !sqlname.Valid(column.Name) {
				return "", fault.New(fault.Invalid, "invalid CTE output column")
			}
			columns[i] = quoted(column.Name)
		}
		mode := ""
		switch d.materialization {
		case cteMaterialized:
			mode = "MATERIALIZED "
		case cteNotMaterialized:
			mode = "NOT MATERIALIZED "
		}
		parts = append(parts, quoted(d.name)+" ("+strings.Join(columns, ", ")+") AS "+mode+"("+body+")")
		c.ctes[d.name] = d
		recursive = recursive || d.recursion != nil
	}
	prefix := "WITH "
	if recursive {
		prefix = "WITH RECURSIVE "
	}
	return prefix + strings.Join(parts, ", ") + " ", nil
}

func (c *compiler) cteBody(d *cteNode) (string, error) {
	if d.recursion == nil {
		return c.selectSQL(d.query)
	}
	r := d.recursion
	previous := c.self
	c.self = r.reference
	defer func() { c.self = previous }()
	// Emit the UNION directly at the recursive definition's top level. The
	// ordinary SetQuery wrapper SELECT would not be a valid recursive body.
	return c.setSQL(setNode{left: d.query, right: r.step, operator: r.operator}, d.columns)
}
func (c *compiler) compileSelect(node selectNode) (string, error) {
	prefix, err := c.compileCTEs(node)
	if err != nil {
		return "", err
	}
	body, err := c.selectSQL(node)
	if err != nil {
		return "", err
	}
	return prefix + body, nil
}
