package typescript

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/internal/contractmeta"
)

type tableOperations struct {
	query, export, download string
	exportKind              string
}

func (r *renderer) tableBindings() map[contract.TableID]tableOperations {
	result := make(map[contract.TableID]tableOperations)
	for _, op := range r.document.HTTP {
		if op.TableOperation == nil {
			continue
		}
		binding := *op.TableOperation
		entry := result[binding.Table]
		switch binding.Kind {
		case contract.TableQuery:
			entry.query = op.Name
		case contract.TableTicketExport:
			entry.export, entry.exportKind = op.Name, "ticket"
		case contract.TableFileExport:
			entry.export, entry.exportKind = op.Name, "file"
		case contract.TableDownload:
			entry.download = op.Name
		}
		result[binding.Table] = entry
	}
	return result
}

func (r *renderer) tables() error {
	if len(r.document.Tables) == 0 {
		return nil
	}
	r.ref("CallOptions")
	bindings := r.tableBindings()
	for _, received := range []bool{false, true} {
		name := "TableRows"
		typeName := r.typeName
		if received {
			name, typeName = "TableReceivedRows", r.receivedName
		}
		fmt.Fprintf(&r.out, "\nexport interface %s {\n", name)
		for _, table := range r.document.Tables {
			fmt.Fprintf(&r.out, "  readonly %s: %s;\n", quote(string(table.ID)), typeName(table.Row))
		}
		r.out.WriteString("}\n")
	}
	r.out.WriteString("export type TableId = keyof TableRows;\nexport type TableRow<Id extends TableId> = TableRows[Id];\nexport type TableReceivedRow<Id extends TableId> = TableReceivedRows[Id];\n")
	if err := r.tablePageFamily(); err != nil {
		return err
	}
	for _, role := range []string{"Query", "Export", "Download"} {
		for _, field := range []string{"Inputs", "Outputs", "OperationNames", "Kinds"} {
			if role == "Query" && field == "Outputs" {
				continue
			}
			if field == "Kinds" && role != "Export" {
				continue
			}
			fmt.Fprintf(&r.out, "export interface Table%s%s {\n", role, field)
			for _, table := range r.document.Tables {
				binding := bindings[table.ID]
				op := binding.query
				if role == "Export" {
					op = binding.export
				}
				if role == "Download" {
					op = binding.download
				}
				if op == "" {
					continue
				}
				value := quote(op)
				switch field {
				case "Inputs":
					value = "Operations[" + quote(op) + "][\"request\"]"
				case "Outputs":
					value = "Operations[" + quote(op) + "][\"response\"]"
					if role == "Download" || binding.exportKind == "file" {
						value = r.ref("FileResult")
					}
				case "Kinds":
					value = quote(binding.exportKind)
				}
				fmt.Fprintf(&r.out, "  readonly %s: %s;\n", quote(string(table.ID)), value)
			}
			r.out.WriteString("}\n")
		}
		fmt.Fprintf(&r.out, "export type %sTableId = keyof Table%sInputs;\n", role, role)
	}
	r.out.WriteString("export type TableQueryOutputs = { readonly [Id in QueryTableId]: TablePageReceived<TableReceivedRows[Id]> };\n")
	r.out.WriteString("export type Tables = { readonly [Id in TableId]: TableMetadata<TableRows[Id]> & { readonly id: Id } };\n")
	for _, role := range []struct{ name, plural string }{{"Query", "Queries"}, {"Export", "Exports"}, {"Download", "Downloads"}} {
		fmt.Fprintf(&r.out, "export type Table%s = { readonly [Id in %sTableId]: (client: API, input: Table%sInputs[Id], options?: CallOptions) => Promise<Table%sOutputs[Id]> };\n", role.plural, role.name, role.name, role.name)
	}
	r.out.WriteString("\nexport const tables: Tables = /* @__PURE__ */ Object.freeze<Tables>({\n")
	for _, table := range r.document.Tables {
		r.renderTableEntry(table)
	}
	r.out.WriteString("});\n")
	for _, role := range []struct {
		plural    string
		operation func(tableOperations) string
	}{
		{"Queries", func(b tableOperations) string { return b.query }},
		{"Exports", func(b tableOperations) string { return b.export }},
		{"Downloads", func(b tableOperations) string { return b.download }},
	} {
		fmt.Fprintf(&r.out, "export const table%s: Table%s = /* @__PURE__ */ Object.freeze<Table%s>({\n", role.plural, role.plural, role.plural)
		for _, table := range r.document.Tables {
			op := role.operation(bindings[table.ID])
			if op == "" {
				continue
			}
			fmt.Fprintf(&r.out, "  %s(client: API, input: Operations[%s][\"request\"], options?: CallOptions) {\n    return client.%s(input, options);\n  },\n", quote(string(table.ID)), quote(op), op)
		}
		r.out.WriteString("});\n")
	}
	r.out.WriteString("export const tableExportKinds: TableExportKinds = /* @__PURE__ */ Object.freeze<TableExportKinds>({\n")
	for _, table := range r.document.Tables {
		if kind := bindings[table.ID].exportKind; kind != "" {
			fmt.Fprintf(&r.out, "  %s: %s,\n", quote(string(table.ID)), quote(kind))
		}
	}
	r.out.WriteString("});\n")
	return nil
}

func (r *renderer) tablePageFamily() error {
	// Use a distinct row parameter so fixed page fields never become generic,
	// even when a table's row is itself a pagination metadata type.
	rowID := contractmeta.TypeID(reflect.TypeFor[contractmeta.PageRow]())
	row := contract.DefineJSON[contractmeta.PageRow](contract.Schema{Root: rowID, Types: []contract.Type{{ID: rowID, Kind: contract.ObjectKind}}})
	schema, err := pagination.NumberedJSON(row).Description()
	if err != nil {
		return err
	}
	template := *r
	template.received = maps.Clone(r.received)
	template.received[rowID] = "Row"
	for _, typ := range schema.Types {
		if typ.Kind == contract.ArrayKind && typ.Element == rowID {
			template.received[typ.ID] = template.renderExpression(typ, true)
		}
	}
	for _, typ := range schema.Types {
		if typ.ID == schema.Root {
			fmt.Fprintf(&r.out, "export type TablePageReceived<Row> = %s;\n", template.renderExpression(typ, true))
		}
	}
	return nil
}

func (r *renderer) renderTableEntry(t manifest.Table) {
	fmt.Fprintf(&r.out, "  %s: Object.freeze<Tables[%s]>({\n", quote(string(t.ID)), quote(string(t.ID)))
	fmt.Fprintf(&r.out, "    id: %s,\n", quote(string(t.ID)))
	r.out.WriteString("    columns: [\n")
	for _, c := range t.Columns {
		r.renderTableColumn(c)
	}
	r.out.WriteString("    ],\n    defaultSort: [\n")
	for _, s := range t.DefaultSort {
		fmt.Fprintf(&r.out, "      { column: %s, direction: %s },\n", quote(s.Column), quote(string(s.Direction)))
	}
	fmt.Fprintf(&r.out, "    ],\n    exports: %t,\n", t.Exports)
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
