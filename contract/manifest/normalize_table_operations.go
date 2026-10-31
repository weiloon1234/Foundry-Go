package manifest

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func normalizeTableOperations(d *Document, types typeIndex) error {
	tables := make(map[contract.TableID]Table, len(d.Tables))
	for _, table := range d.Tables {
		tables[table.ID] = table
	}
	type slot struct {
		table contract.TableID
		role  string
	}
	seen := make(map[slot]foundryhttp.RouteID)
	for _, op := range d.HTTP {
		if op.TableOperation == nil {
			continue
		}
		binding := *op.TableOperation
		fail := func(message string) error {
			return invalid(fmt.Sprintf("table %q role %q route %q: %s", binding.Table, binding.Kind, op.Route.ID, message))
		}
		table, exists := tables[binding.Table]
		if !exists {
			return fail("table is not registered")
		}
		role := ""
		switch binding.Kind {
		case contract.TableQuery:
			role = "query"
		case contract.TableTicketExport, contract.TableFileExport:
			role = "export"
		case contract.TableDownload:
			role = "download"
		default:
			return fail("unknown operation kind")
		}
		key := slot{table.ID, role}
		if previous, exists := seen[key]; exists {
			return fail(fmt.Sprintf("slot %q is already bound by route %q", role, previous))
		}
		seen[key] = op.Route.ID
		if binding.Kind != contract.TableTicketExport && binding.RequestField != "" {
			return fail("request_field is only valid for ticket exports")
		}
		if op.Status < 200 || op.Status >= 300 || op.Response == nil {
			return fail("requires a declared success response")
		}
		for _, status := range op.Statuses {
			if status < 200 || status >= 300 {
				return fail("requires successful response statuses")
			}
		}
		if binding.Kind != contract.TableQuery && !table.Exports {
			return fail("table exports are disabled")
		}
		jsonPayload := func(payload *Payload) bool {
			return payload != nil && payload.MediaType == "application/json" && payload.File == nil && types.has(payload.Type)
		}
		switch binding.Kind {
		case contract.TableQuery:
			if len(op.Statuses) != 0 {
				return fail("query requires a plain canonical page response, not a status-tagged response")
			}
			if !jsonPayload(op.Body) || op.Body.Type != table.Request {
				return fail(fmt.Sprintf("query must accept request schema %q", table.Request))
			}
			if !jsonPayload(op.Response) || op.Response.Type != table.Page {
				return fail(fmt.Sprintf("query must return page schema %q for row %q", table.Page, table.Row))
			}
		case contract.TableTicketExport:
			if !jsonPayload(op.Body) || !jsonPayload(op.Response) {
				return fail("ticket export requires JSON request and response schemas")
			}
			body := types[op.Body.Type]
			for range len(types) {
				if body.Kind != contract.AliasKind || body.Nullable {
					break
				}
				body = types[body.Element]
			}
			valid := false
			if body.Kind == contract.ObjectKind && !body.Nullable {
				for _, property := range body.Properties {
					if property.Name == binding.RequestField && property.Required && property.Type == table.Request && !types[property.Type].Nullable {
						valid = true
					}
				}
			}
			if !valid {
				return fail(fmt.Sprintf("body schema %q requires non-null field %q with request schema %q", op.Body.Type, binding.RequestField, table.Request))
			}
		case contract.TableFileExport, contract.TableDownload:
			if op.Response.File == nil {
				return fail("requires a file response")
			}
			if binding.Kind == contract.TableDownload && (op.Route.Method != foundryhttp.GET || op.Body != nil) {
				return fail("download requires GET without a body")
			}
		}
	}
	return nil
}
