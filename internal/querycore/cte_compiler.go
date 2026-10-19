package querycore

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

func OrderedCTEs(root SelectNode) ([]*CteNode, error) {
	names := make(map[string]*CteNode)
	tables := make(map[string]bool)
	active, visited := make(map[*CteNode]bool), make(map[*CteNode]bool)
	var order []*CteNode
	var walk SelectWalk
	var self *RecursiveReference
	selfCount := 0
	walk.Source = func(s TableSource, depth int) {
		if s.Self != nil {
			if s.Self != self {
				walk.Err = fault.New(fault.Invalid, "recursive reference is outside its owning step")
			} else if walk.RecursiveRestriction != "" {
				walk.Err = fault.New(fault.Invalid, "recursive reference is not allowed in "+walk.RecursiveRestriction)
			} else {
				selfCount++
				if selfCount > 1 {
					walk.Err = fault.New(fault.Invalid, "recursive step must reference its working table exactly once")
				}
			}
			return
		}
		if s.Cte == nil {
			if s.Query == nil && s.Set == nil {
				tables[s.Table] = true
			}
			return
		}
		d := s.Cte
		if !sqlname.Valid(d.Name) || d.Materialization > CteNotMaterialized || len(d.Columns) == 0 || len(d.Columns) != len(d.Query.Selections) {
			walk.Err = fault.New(fault.Invalid, "invalid CTE definition")
			return
		}
		if other := names[d.Name]; other != nil && other != d {
			walk.Err = fault.New(fault.Invalid, "statement repeats a CTE name with different definitions")
			return
		}
		if active[d] {
			walk.Err = fault.New(fault.Invalid, "cyclic CTE dependency requires an explicit recursive query")
			return
		}
		if visited[d] {
			return
		}
		names[d.Name], active[d] = d, true
		// Dependencies define independent queries. They cannot capture the
		// working table of a CTE that happens to reference them.
		previousSelf, previousCount, previousRestriction := self, selfCount, walk.RecursiveRestriction
		self, selfCount, walk.RecursiveRestriction = nil, 0, ""
		walk.SelectNode(d.Query, depth+1)
		if r := d.Recursion; r != nil && walk.Err == nil {
			if r.Reference == nil || r.Reference.Name != d.Name || !slices.Equal(r.Reference.Columns, d.Columns) || len(r.Step.Selections) != len(d.Columns) || r.Operator > UnionAllSet || d.Materialization == CteNotMaterialized {
				walk.Err = fault.New(fault.Invalid, "invalid recursive CTE definition or materialization")
			} else {
				self, selfCount = r.Reference, 0
				walk.SelectNode(r.Step, depth+1)
				if walk.Err == nil && selfCount != 1 {
					walk.Err = fault.New(fault.Invalid, "recursive step must reference its working table exactly once")
				}
			}
		}
		self, selfCount, walk.RecursiveRestriction = previousSelf, previousCount, previousRestriction
		active[d], visited[d] = false, true
		order = append(order, d)
	}
	walk.SelectNode(root, 0)
	if walk.Err != nil {
		return nil, walk.Err
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
func (c *Compiler) CompileCTEs(node SelectNode) (string, error) {
	definitions, err := OrderedCTEs(node)
	if err != nil {
		return "", err
	}
	if len(definitions) == 0 {
		return "", nil
	}
	c.Ctes = make(map[string]*CteNode, len(definitions))
	parts := make([]string, 0, len(definitions))
	recursive := false
	for _, d := range definitions {
		body, err := c.CteBody(d)
		if err != nil {
			return "", err
		}
		columns := make([]string, len(d.Columns))
		for i, column := range d.Columns {
			if !sqlname.Valid(column.Name) {
				return "", fault.New(fault.Invalid, "invalid CTE output column")
			}
			columns[i] = Quoted(column.Name)
		}
		mode := ""
		switch d.Materialization {
		case CteMaterialized:
			mode = "MATERIALIZED "
		case CteNotMaterialized:
			mode = "NOT MATERIALIZED "
		}
		parts = append(parts, Quoted(d.Name)+" ("+strings.Join(columns, ", ")+") AS "+mode+"("+body+")")
		c.Ctes[d.Name] = d
		recursive = recursive || d.Recursion != nil
	}
	prefix := "WITH "
	if recursive {
		prefix = "WITH RECURSIVE "
	}
	return prefix + strings.Join(parts, ", ") + " ", nil
}

func (c *Compiler) CteBody(d *CteNode) (string, error) {
	if d.Recursion == nil {
		return c.SelectSQL(d.Query)
	}
	r := d.Recursion
	previous := c.Self
	c.Self = r.Reference
	defer func() { c.Self = previous }()
	// Emit the UNION directly at the recursive definition's top level. The
	// ordinary SetQuery wrapper SELECT would not be a valid recursive body.
	return c.SetSQL(SetNode{Left: d.Query, Right: r.Step, Operator: r.Operator}, d.Columns)
}

func (c *Compiler) CompileSelect(node SelectNode) (string, error) {
	prefix, err := c.CompileCTEs(node)
	if err != nil {
		return "", err
	}
	body, err := c.SelectSQL(node)
	if err != nil {
		return "", err
	}
	return prefix + body, nil
}
