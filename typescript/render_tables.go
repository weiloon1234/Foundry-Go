package typescript

import (
	"fmt"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func (r *renderer) tables() error {
	if len(r.document.Tables) == 0 {
		return nil
	}

	tableIDs := make([]string, len(r.document.Tables))
	for i, t := range r.document.Tables {
		tableIDs[i] = quote(string(t.ID))
	}
	fmt.Fprintf(&r.out, "\nexport type TableId = %s;\n\n", strings.Join(tableIDs, " | "))

	r.out.WriteString("export type TableRow<Id extends TableId> =\n")
	for _, t := range r.document.Tables {
		fmt.Fprintf(&r.out, "  Id extends %s ? %s :\n", quote(string(t.ID)), r.typeName(t.Row))
	}
	r.out.WriteString("  never;\n\n")

	r.out.WriteString("export type TableReceivedRow<Id extends TableId> =\n")
	for _, t := range r.document.Tables {
		fmt.Fprintf(&r.out, "  Id extends %s ? %s :\n", quote(string(t.ID)), r.receivedName(t.Row))
	}
	r.out.WriteString("  never;\n\n")

	r.out.WriteString("export const tables = /* @__PURE__ */ Object.freeze({\n")
	for _, t := range r.document.Tables {
		queryOp, exportOp, err := r.tableOperations(t)
		if err != nil {
			return err
		}
		rowType := r.typeName(t.Row)
		r.renderTableEntry(t, queryOp, exportOp, rowType)
	}
	r.out.WriteString("} as const);\n")
	return nil
}

// Exact route identities declare the table binding. Shared schemas and client
// names cannot identify a table: every table uses the same request contract.
func (r *renderer) tableOperations(table manifest.Table) (queryOp string, exportOp string, err error) {
	invalid := func(message string) (string, string, error) {
		return "", "", safeerror.NewFault(fault.Invalid, fmt.Sprintf("table %q: %s", table.ID, message))
	}
	for _, op := range r.document.HTTP {
		id := string(op.Route.ID)
		switch id {
		case string(table.ID) + ".query", string(table.ID) + ":query":
			if queryOp != "" {
				return invalid("multiple query routes declare this table")
			}
			if op.Body == nil || op.Body.Type != table.Request || op.Body.MediaType != "application/json" {
				return invalid("query route must accept the table request as JSON")
			}
			if op.Response == nil || op.Response.MediaType != "application/json" || !r.tableResponseRow(op.Response.Type, table.Row) {
				return invalid("query route must return a page of the declared table row")
			}
			queryOp = op.Name
		case string(table.ID) + ".export", string(table.ID) + ".csv", string(table.ID) + ":export", string(table.ID) + ":csv":
			if exportOp != "" {
				return invalid("multiple export routes declare this table")
			}
			if !table.Exports || op.Response == nil || op.Response.File == nil {
				return invalid("export route requires an exportable table and a file response")
			}
			if op.Body != nil && (op.Body.Type != table.Request || op.Body.MediaType != "application/json") {
				return invalid("export route body must accept the table request as JSON")
			}
			exportOp = op.Name
		}
	}
	return queryOp, exportOp, nil
}

func (r *renderer) tableResponseRow(id, row contract.TypeID) bool {
	types := make(map[contract.TypeID]contract.Type, len(r.document.Types))
	for _, typ := range r.document.Types {
		types[typ.ID] = typ
	}
	// Alias traversal is bounded by the number of schemas, including cycles.
	resolve := func(id contract.TypeID) contract.Type {
		for range len(types) + 1 {
			typ := types[id]
			if typ.Kind != contract.AliasKind {
				return typ
			}
			if typ.Nullable {
				return contract.Type{}
			}
			id = typ.Element
		}
		return contract.Type{}
	}
	page := resolve(id)
	if page.Kind != contract.ObjectKind || page.Nullable {
		return false
	}
	for _, property := range page.Properties {
		if property.Name == "data" && property.Required {
			items := resolve(property.Type)
			return items.Kind == contract.ArrayKind && items.Element == row
		}
	}
	return false
}

func (r *renderer) renderTableEntry(t manifest.Table, queryOp, exportOp, rowType string) {
	typeArgs := rowType
	if queryOp != "" {
		typeArgs += ", " + quote(queryOp)
	}
	if queryOp == "" && exportOp != "" {
		typeArgs += ", never"
	}
	if exportOp != "" {
		typeArgs += ", " + quote(exportOp)
	}
	fmt.Fprintf(&r.out, "  %s: createTableDescriptor<%s>({\n", quote(string(t.ID)), typeArgs)
	fmt.Fprintf(&r.out, "    id: %s,\n", quote(string(t.ID)))
	if queryOp != "" {
		fmt.Fprintf(&r.out, "    queryOperation: %s,\n", quote(queryOp))
	}
	if exportOp != "" {
		fmt.Fprintf(&r.out, "    exportOperation: %s,\n", quote(exportOp))
	}
	r.out.WriteString("    columns: [\n")
	for _, c := range t.Columns {
		r.renderTableColumn(c)
	}
	r.out.WriteString("    ],\n")
	r.out.WriteString("    defaultSort: [\n")
	for _, s := range t.DefaultSort {
		fmt.Fprintf(&r.out, "      { column: %s, direction: %s },\n", quote(s.Column), quote(string(s.Direction)))
	}
	r.out.WriteString("    ],\n")
	if t.Exports {
		r.out.WriteString("    exports: true,\n")
	} else {
		r.out.WriteString("    exports: false,\n")
	}
	if queryOp != "" {
		fmt.Fprintf(&r.out, "    query(client: API, input: Operations[%s][\"request\"], options?: CallOptions): Promise<Operations[%s][\"response\"]> {\n", quote(queryOp), quote(queryOp))
		fmt.Fprintf(&r.out, "      return client[%s](input, options);\n", quote(queryOp))
		r.out.WriteString("    },\n")
	}
	if exportOp != "" {
		fmt.Fprintf(&r.out, "    export(client: API, input: Operations[%s][\"request\"], options?: CallOptions): Promise<Operations[%s][\"response\"]> {\n", quote(exportOp), quote(exportOp))
		fmt.Fprintf(&r.out, "      return client[%s](input, options);\n", quote(exportOp))
		r.out.WriteString("    },\n")
	}
	r.out.WriteString("  }),\n")
}

func (r *renderer) renderTableColumn(c datatable.ColumnInfo) {
	kind := string(c.Kind)
	if kind == "" {
		kind = string(columnKindFromType(c.Value.Value))
	}
	fmt.Fprintf(&r.out, "      {\n")
	fmt.Fprintf(&r.out, "        name: %s,\n", quote(c.Name))
	fmt.Fprintf(&r.out, "        label: %s,\n", quote(string(c.Label)))
	fmt.Fprintf(&r.out, "        kind: %s,\n", quote(kind))
	if c.Value.Value.Nullable {
		r.out.WriteString("        nullable: true,\n")
	} else {
		r.out.WriteString("        nullable: false,\n")
	}
	if c.Sortable {
		r.out.WriteString("        sortable: true,\n")
	} else {
		r.out.WriteString("        sortable: false,\n")
	}
	if c.Searchable {
		r.out.WriteString("        searchable: true,\n")
	} else {
		r.out.WriteString("        searchable: false,\n")
	}
	if c.Exportable {
		r.out.WriteString("        exportable: true,\n")
	} else {
		r.out.WriteString("        exportable: false,\n")
	}
	if c.Filter != nil && len(c.Filter.Operators) > 0 {
		ops := make([]string, len(c.Filter.Operators))
		for i, op := range c.Filter.Operators {
			ops[i] = quote(string(op))
		}
		fmt.Fprintf(&r.out, "        operators: [%s],\n", strings.Join(ops, ", "))
	} else {
		r.out.WriteString("        operators: [],\n")
	}
	if kind == string(datatable.ColumnEnum) {
		fmt.Fprintf(&r.out, "        enum: %s,\n", r.typeName(c.Value.Value.ID))
	}
	fmt.Fprintf(&r.out, "      },\n")
}

func columnKindFromType(val contract.Type) datatable.ColumnKind {
	if len(val.Cases) > 0 {
		return datatable.ColumnEnum
	}
	switch val.Kind {
	case contract.BooleanKind:
		return datatable.ColumnBoolean
	case contract.IntegerKind:
		return datatable.ColumnInteger
	case contract.NumberKind:
		return datatable.ColumnDecimal
	}
	switch val.Format {
	case contract.DecimalFormat:
		return datatable.ColumnDecimal
	case contract.DateFormat:
		return datatable.ColumnDate
	case contract.DateTimeFormat:
		return datatable.ColumnDateTime
	case contract.LocalDateTimeFormat:
		return datatable.ColumnLocalDateTime
	case contract.TimeFormat:
		return datatable.ColumnTime
	default:
		return datatable.ColumnText
	}
}
