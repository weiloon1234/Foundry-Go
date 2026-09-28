package generate

import "fmt"

type projectionKind uint8

const (
	ordinaryProjection projectionKind = iota
	correlatedProjection
	transactionProjection
	transactionCorrelatedProjection
)

func projectionKinds() []projectionKind {
	return []projectionKind{ordinaryProjection, correlatedProjection, transactionProjection, transactionCorrelatedProjection}
}

// Discovery and emission use the same public factory names.
func (k projectionKind) names(model string) (selection, project, builder string) {
	prefix := [...]string{"", "Correlated", "Transaction", "TransactionCorrelated"}[k]
	return "Select" + prefix + model, "Project" + prefix + model, model + prefix + "Project"
}

type projectionShape struct{ builder, declaration, arguments, scope, source, result, project string }

func (k projectionKind) shape(model, query, scope, outer, inner string) projectionShape {
	_, _, builder := k.names(model)
	shape := projectionShape{builder: builder, declaration: scope + " any", arguments: scope, scope: scope,
		source: fmt.Sprintf("%s.ProjectionSource[%s]", query, scope), result: fmt.Sprintf("%s.ProjectionQuery[%s,%s]", query, scope, model), project: "Project"}
	switch k {
	case correlatedProjection, transactionCorrelatedProjection:
		prefix := ""
		shape.declaration, shape.arguments = outer+","+inner+" any", outer+","+inner
		if k == transactionCorrelatedProjection {
			prefix = "Transaction"
			shape.declaration = outer + "," + inner + " " + query + ".TransactionScope"
		}
		shape.scope = fmt.Sprintf("%s.%sCorrelation[%s,%s]", query, prefix, outer, inner)
		shape.source = fmt.Sprintf("%s.%sCorrelatedSource[%s,%s]", query, prefix, outer, inner)
		shape.result = fmt.Sprintf("%s.%sCorrelatedRecordQuery[%s,%s,%s]", query, prefix, outer, inner, model)
		shape.project = "Project" + prefix + "Correlated"
	case transactionProjection:
		shape.declaration = scope + " " + query + ".TransactionScope"
		shape.source = fmt.Sprintf("%s.TransactionProjectionSource[%s]", query, scope)
		shape.result = fmt.Sprintf("%s.TransactionQuery[%s,%s]", query, scope, model)
		shape.project = "ProjectTransaction"
	}
	return shape
}

func projectionSymbols(model string) []string {
	symbols := []string{model + "FieldSet", model + "Fields", model + "Projection", model + "Selection",
		model + "ScopedFieldSet", model + "NullableFieldSet", model + "FieldsAt", model + "NullableFieldsAt"}
	for _, kind := range projectionKinds() {
		selection, project, builder := kind.names(model)
		symbols = append(symbols, selection, builder, project)
	}
	return symbols
}
