package querycore

import (
	"context"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
)

// Fragments are authored by the compiler, not extracted from SQL. NUL markers
// cannot occur in validated SQL identifiers or schema literals. Each fragment
// retains both renderings; binding/scoping callbacks execute only once.
type statementFragment struct{ sql, shape string }
type statementFragments struct{ values []statementFragment }

// BeginStatement re-evaluates diagnostic scope for every compilation, including
// reused compilers. An inactive execution follows the ordinary renderer without
// fragment state or a shape allocation. Inspect-only compiles may have no context.
func (c *Compiler) BeginStatement(ctx context.Context) {
	c.ScopeContext = ctx
	c.fragments = nil
	c.opaque = false
	if ctx != nil && statementmeta.Active(ctx) {
		c.TrackShape()
	}
}

func (c *Compiler) TrackShape() {
	if c.fragments == nil {
		c.fragments = &statementFragments{}
	}
}
func (c *Compiler) fragment(sql, shape string) string {
	if c.fragments == nil {
		return sql
	}
	// Resolve child fragments now: expansion is flat and cannot interpret SQL as
	// markers. In particular a bound string is never fed to this rendering path.
	value := statementFragment{c.render(sql, false), c.render(shape, true)}
	i := len(c.fragments.values)
	c.fragments.values = append(c.fragments.values, value)
	return "\x00" + strconv.Itoa(i) + "\x00"
}
func (c *Compiler) render(template string, shape bool) string {
	if c.fragments == nil || !strings.ContainsRune(template, 0) {
		return template
	}
	var out strings.Builder
	for {
		start := strings.IndexByte(template, 0)
		if start < 0 {
			out.WriteString(template)
			break
		}
		out.WriteString(template[:start])
		template = template[start+1:]
		end := strings.IndexByte(template, 0)
		if end < 0 {
			panic("querycore: incomplete compiler fragment")
		}
		i, err := strconv.Atoi(template[:end])
		if err != nil || i < 0 || i >= len(c.fragments.values) {
			panic("querycore: invalid compiler fragment")
		}
		f := c.fragments.values[i]
		if shape {
			out.WriteString(f.shape)
		} else {
			out.WriteString(f.sql)
		}
		template = template[end+1:]
	}
	return out.String()
}
func (c *Compiler) Statement(template string) Statement {
	s := Statement{SQLText: c.render(template, false), Bindings: c.Arguments}
	if c.fragments != nil && !c.opaque {
		s.Shape = c.render(template, true)
	}
	return s
}
func (s Statement) Context(ctx context.Context) context.Context {
	return statementmeta.With(ctx, s.SQLText, s.Shape)
}

// Envelope preserves the compiler-owned shape when a framework executor adds a
// fixed count/explain envelope. Callers are internal framework packages only.
func (s Statement) Envelope(prefix, suffix string) Statement {
	s.SQLText = prefix + s.SQLText + suffix
	if s.Shape != "" {
		s.Shape = prefix + s.Shape + suffix
	}
	return s
}

// rowsFragment collapses equivalent VALUES rows but retains distinct ordered
// signatures (including DEFAULT positions and representation casts).
func (c *Compiler) rowsFragment(rows []string) string {
	sql := strings.Join(rows, ", ")
	if c.fragments == nil {
		return sql
	}
	seen := make(map[string]bool)
	var shapes []string
	for _, row := range rows {
		shape := c.render(row, true)
		if !seen[shape] {
			seen[shape] = true
			shapes = append(shapes, shape)
		}
	}
	return c.fragment(sql, strings.Join(shapes, ", "))
}
