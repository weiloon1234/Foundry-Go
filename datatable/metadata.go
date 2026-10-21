package datatable

import (
	"encoding/json"
	"slices"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

type columnMetadataDeclaration struct {
	name       string
	label      i18n.MessageKey
	sortable   bool
	searchable bool
	exportable bool
	filter     *filterMetadataDeclaration
	cellInfo   *FilterInfo
	err        error
}

type filterMetadataDeclaration struct {
	name       string
	label      i18n.MessageKey
	info       FilterInfo
	hasBuilder bool
	err        error
}

type tableMetadata struct {
	info     Description
	byColumn map[string]int
	byFilter map[string]filterLocation
}

type filterLocation struct {
	column bool
	index  int
}

func (m *tableMetadata) initialize(id TableID, row contract.Schema, describeScalar func(string) (contract.ScalarProperty, error), columns []columnMetadataDeclaration, filters []filterMetadataDeclaration, defaultSort []Sort, exportsEnabled bool) error {
	m.byColumn = make(map[string]int, len(columns))
	m.byFilter = make(map[string]filterLocation, len(columns)+len(filters))
	if err := contract.RejectPasswordOutput(row, "table row"); err != nil {
		return err
	}
	request, err := RequestJSON().Description()
	if err != nil {
		return err
	}
	m.info = Description{ID: id, Row: row, Request: request, DefaultSort: slices.Clone(defaultSort), Exports: exportsEnabled}
	exports := 0
	var searchPhase FilterPhase
	for _, c := range columns {
		if c.err != nil {
			return c.err
		}
		if !validName(c.name) || c.label.Validate() != nil {
			return invalid("invalid datatable column")
		}
		if _, exists := m.byColumn[c.name]; exists {
			return fault.New(fault.Duplicate, "datatable column is repeated")
		}
		scalar, err := describeScalar(c.name)
		if err != nil {
			return invalid("datatable column must name a declared scalar DTO property")
		}
		info := ColumnInfo{Name: c.name, Label: c.label, Value: scalar, Sortable: c.sortable, Searchable: c.searchable, Exportable: c.exportable}
		if c.filter != nil {
			f := *c.filter
			if err := m.addFilter(f, filterLocation{column: true, index: len(m.info.Columns)}); err != nil {
				return err
			}
			if !sameScalar(scalar.Value, f.info) {
				return invalid("column filter scalar differs from its DTO property")
			}
			copy, err := cloneFilterInfo(f.info)
			if err != nil {
				return err
			}
			info.Filter = &copy
		}
		if c.searchable {
			if c.filter == nil {
				return invalid("searchable columns require a declared contains filter")
			}
			info.SearchOperator = searchOperator(c.filter.info)
			if info.SearchOperator == "" {
				return invalid("searchable columns require a declared contains filter")
			}
			// Global search ORs every searchable column; one OR cannot span
			// the WHERE and HAVING phases, so reject the table, not a request.
			if searchPhase != "" && searchPhase != c.filter.info.Phase {
				return invalid("searchable columns cannot mix WHERE and HAVING phases")
			}
			searchPhase = c.filter.info.Phase
		}
		if c.exportable {
			if c.cellInfo == nil || !sameScalar(scalar.Value, *c.cellInfo) {
				return invalid("column export scalar differs from its DTO property")
			}
			exports++
		}
		m.byColumn[c.name] = len(m.info.Columns)
		m.info.Columns = append(m.info.Columns, info)
	}
	for _, f := range filters {
		if _, exists := m.byColumn[f.name]; exists {
			return fault.New(fault.Duplicate, "extra filter overlaps a displayed column")
		}
		if err := m.addFilter(f, filterLocation{index: len(m.info.Filters)}); err != nil {
			return err
		}
		copy, err := cloneFilterInfo(f.info)
		if err != nil {
			return err
		}
		m.info.Filters = append(m.info.Filters, NamedFilterInfo{Name: f.name, Label: f.label, Filter: copy})
	}
	if exportsEnabled && exports == 0 {
		return invalid("export-enabled table requires exportable columns")
	}
	return m.validateSort(defaultSort)
}
func (m *tableMetadata) addFilter(f filterMetadataDeclaration, location filterLocation) error {
	if f.err != nil {
		return f.err
	}
	if !validName(f.name) || f.label.Validate() != nil || !f.hasBuilder {
		return invalid("invalid datatable filter")
	}
	if _, exists := m.byFilter[f.name]; exists {
		return fault.New(fault.Duplicate, "datatable filter is repeated")
	}
	m.byFilter[f.name] = location
	return nil
}

func (m *tableMetadata) filter(name string) (*FilterInfo, bool) {
	location, ok := m.byFilter[name]
	if !ok {
		return nil, false
	}
	if location.column {
		return m.info.Columns[location.index].Filter, true
	}
	return &m.info.Filters[location.index].Filter, true
}

func (m *tableMetadata) description() (Description, error) {
	data, err := json.Marshal(m.info)
	if err != nil {
		return Description{}, err
	}
	var result Description
	err = json.Unmarshal(data, &result)
	return result, err
}

func (m *tableMetadata) validateSort(sorts []Sort) error {
	return validateSortColumns(sorts, func(name string) bool {
		index, ok := m.byColumn[name]
		return ok && m.info.Columns[index].Sortable
	})
}

type requestBudget struct{ nodes, bytes int }

func (b *requestBudget) text(text string, maximum int) error {
	if len(text) > maximum || !utf8.ValidString(text) || len(text) > MaxRequestBytes-b.bytes {
		return requestInvalid("datatable request exceeds its text bounds")
	}
	b.bytes += len(text)
	return nil
}
func (m *tableMetadata) validateRequest(request Request, config Config) (query.PageRequest, error) {
	page, err := request.page()
	if err != nil {
		return page, err
	}
	if page.Size > config.MaxPageSize || (page.Number-1) > config.MaxOffset/page.Size {
		return page, requestInvalid("datatable page exceeds its configured bound")
	}
	if err := m.validateSort(request.Sort); err != nil {
		return page, foundryhttp.BadRequest.WithCause(err)
	}
	budget := requestBudget{}
	for _, sort := range request.Sort {
		if err := budget.text(sort.Column, 128); err != nil {
			return page, err
		}
	}
	if err := budget.text(request.Search, MaxScalarBytes); err != nil {
		return page, err
	}
	// Complete structural validation precedes all scalar/extension callbacks.
	for _, filter := range request.Filters {
		if err := m.validateFilter(filter, 0, &budget); err != nil {
			return page, err
		}
	}
	return page, nil
}
func (m *tableMetadata) validateFilter(filter Filter, depth int, budget *requestBudget) error {
	if depth > MaxFilterDepth || budget.nodes >= MaxFilters {
		return requestInvalid("datatable filter tree exceeds its structural bound")
	}
	budget.nodes++
	if err := budget.text(filter.Column, 128); err != nil {
		return err
	}
	if len(filter.Values) > MaxFilterValues {
		return requestInvalid("too many filter values")
	}
	for _, text := range filter.Values {
		if err := budget.text(text, MaxScalarBytes); err != nil {
			return err
		}
	}
	switch filter.Op {
	case All, Any, Not:
		if filter.Column != "" || len(filter.Values) != 0 || len(filter.Children) == 0 || filter.Op == Not && len(filter.Children) != 1 {
			return requestInvalid("invalid datatable filter group")
		}
		for _, child := range filter.Children {
			if err := m.validateFilter(child, depth+1, budget); err != nil {
				return err
			}
		}
	default:
		info, ok := m.filter(filter.Column)
		if !ok || len(filter.Children) != 0 || !slices.Contains(info.Operators, filter.Op) {
			return requestInvalid("invalid or undeclared datatable filter")
		}
		return filterArity(filter.Op, len(filter.Values))
	}
	return nil
}
