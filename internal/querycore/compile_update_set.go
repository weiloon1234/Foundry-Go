package querycore

import "strings"

// compileUpdateSet visits only declared columns, in model order, before binding
// any assignment value. Callers supply validated targets; their maps/slices are
// never reordered. The result slice replaces each envelope's existing SET buffer.
func compileUpdateSet(columns []Column, count int, value func(Column) (string, bool, error)) (string, error) {
	sets := make([]string, 0, count)
	for _, column := range columns {
		expression, present, err := value(column)
		if err != nil {
			return "", err
		}
		if present {
			sets = append(sets, Quoted(column.Name)+" = "+expression)
		}
	}
	return strings.Join(sets, ", "), nil
}
