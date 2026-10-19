package querycore

// Lateral FROM inputs observe preceding rows before this SELECT's grouping.
// Ordinary derived sources continue to compile with no outer visibility.
func (c *Compiler) JoinSourceSQL(join JoinNode) (string, error) {
	if join.Lateral == nil {
		return c.SourceSQL(join.Source)
	}
	outer, err := c.CorrelationScope(join.Lateral, nil, false)
	if err != nil {
		return "", err
	}
	text, err := c.SelectSQLWithOuter(*join.Source.Query, outer)
	return "LATERAL (" + text + ") AS " + Quoted(join.Source.Alias), err
}
