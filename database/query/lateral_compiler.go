package query

// Lateral FROM inputs observe preceding rows before this SELECT's grouping.
// Ordinary derived sources continue to compile with no outer visibility.
func (c *compiler) joinSourceSQL(join joinNode) (string, error) {
	if join.lateral == nil {
		return c.sourceSQL(join.source)
	}
	outer, err := c.correlationScope(join.lateral, nil, false)
	if err != nil {
		return "", err
	}
	text, err := c.selectSQLWithOuter(*join.source.query, outer)
	return "LATERAL (" + text + ") AS " + quoted(join.source.alias), err
}
