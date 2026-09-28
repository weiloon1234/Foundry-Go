package pagination

import (
	"math"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func navigation(page query.PageRequest, more bool, build func(query.PageRequest) (string, error)) (Links, error) {
	var links Links
	if more {
		if page.Number == math.MaxInt {
			return Links{}, fault.New(fault.Invalid, "next page exceeds the platform page-number range")
		}
		next := page
		next.Number++
		location, err := build(next)
		if err != nil {
			return Links{}, err
		}
		links.Next = value.Of(location)
	}
	if page.Number > 1 {
		previous := page
		previous.Number--
		location, err := build(previous)
		if err != nil {
			return Links{}, err
		}
		links.Previous = value.Of(location)
	}
	return links, nil
}
func pageItems[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return slices.Clone(items)
}
func numberedResponse[P, F, T any](request Request[P, F], page query.Page[T], build func(query.PageRequest) (string, error)) (NumberedResponse[T], error) {
	if err := page.Validate(); err != nil {
		return NumberedResponse[T]{}, err
	}
	if page.Number != request.Page.Number || page.Size != request.Page.Size {
		return NumberedResponse[T]{}, fault.New(fault.Invalid, "page result does not match its HTTP request")
	}
	links, err := navigation(request.Page, int64(page.Number) < page.Pages, build)
	if err != nil {
		return NumberedResponse[T]{}, err
	}
	return NumberedResponse[T]{Data: pageItems(page.Items), Meta: NumberedMeta{Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages}, Links: links}, nil
}
func simpleResponse[P, F, T any](request Request[P, F], page query.SimplePage[T], build func(query.PageRequest) (string, error)) (SimpleResponse[T], error) {
	if err := page.Validate(); err != nil {
		return SimpleResponse[T]{}, err
	}
	if page.Number != request.Page.Number || page.Size != request.Page.Size {
		return SimpleResponse[T]{}, fault.New(fault.Invalid, "page result does not match its HTTP request")
	}
	links, err := navigation(request.Page, page.HasMore, build)
	if err != nil {
		return SimpleResponse[T]{}, err
	}
	return SimpleResponse[T]{Data: pageItems(page.Items), Meta: SimpleMeta{Number: page.Number, Size: page.Size, HasMore: page.HasMore}, Links: links}, nil
}
