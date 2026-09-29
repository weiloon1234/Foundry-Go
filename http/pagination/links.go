package pagination

import (
	"errors"
	"math"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// navigationPolicy is an endpoint's opt-in first/last and page-window links.
type navigationPolicy struct {
	edges  bool
	window int
}

// navigation builds next/previous links and, when enabled, first/last and a
// window of numbered pages. last is the known final page of a numbered result;
// count-free pages have none. Links deeper than the endpoint's MaximumPage are
// omitted instead of failing the response.
func navigation(page query.PageRequest, more bool, last value.Optional[int64], policy navigationPolicy, build func(query.PageRequest) (string, error)) (Links, error) {
	link := func(number int) (string, bool, error) {
		target := page
		target.Number = number
		location, err := build(target)
		switch {
		case errors.Is(err, errBeyondMaximumPage):
			return "", false, nil
		case err != nil:
			return "", false, err
		}
		return location, true, nil
	}
	var links Links
	if more {
		if page.Number == math.MaxInt {
			return Links{}, fault.New(fault.Invalid, "next page exceeds the platform page-number range")
		}
		// The next page may exist but be deeper than the endpoint accepts.
		location, ok, err := link(page.Number + 1)
		if err != nil {
			return Links{}, err
		}
		if ok {
			links.Next = value.Of(location)
		}
	}
	if page.Number > 1 {
		location, ok, err := link(page.Number - 1)
		if err != nil {
			return Links{}, err
		}
		if ok {
			links.Previous = value.Of(location)
		}
	}
	if policy.edges {
		location, ok, err := link(1)
		if err != nil {
			return Links{}, err
		}
		if ok {
			links.First = value.Set(location)
		}
	}
	final, known := last.Get()
	if !known {
		return links, nil
	}
	// An empty result still has one (empty) page to link to.
	lastPage := int(min(max(final, 1), int64(math.MaxInt)))
	if policy.edges {
		location, ok, err := link(lastPage)
		if err != nil {
			return Links{}, err
		}
		if ok {
			links.Last = value.Set(location)
		}
	}
	if policy.window > 0 {
		from := max(page.Number-policy.window, 1)
		to := lastPage
		if page.Number <= lastPage-policy.window {
			to = page.Number + policy.window
		}
		links.Window = make([]PageLink, 0, max(to-from+1, 0))
		for number := from; number <= to; number++ {
			location, ok, err := link(number)
			if err != nil {
				return Links{}, err
			}
			if !ok {
				break
			}
			links.Window = append(links.Window, PageLink{Number: number, URL: location})
		}
	}
	return links, nil
}
func pageItems[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return slices.Clone(items)
}
func numberedResponse[P, F, T any](policy navigationPolicy) func(Request[P, F], query.Page[T], func(query.PageRequest) (string, error)) (NumberedResponse[T], error) {
	return func(request Request[P, F], page query.Page[T], build func(query.PageRequest) (string, error)) (NumberedResponse[T], error) {
		if err := page.Validate(); err != nil {
			return NumberedResponse[T]{}, err
		}
		if page.Number != request.Page.Number || page.Size != request.Page.Size {
			return NumberedResponse[T]{}, fault.New(fault.Invalid, "page result does not match its HTTP request")
		}
		links, err := navigation(request.Page, int64(page.Number) < page.Pages, value.Set(page.Pages), policy, build)
		if err != nil {
			return NumberedResponse[T]{}, err
		}
		return NumberedResponse[T]{Data: pageItems(page.Items), Meta: NumberedMeta{Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages}, Links: links}, nil
	}
}

// simpleResponse has no count, so it never emits a last link or page window.
func simpleResponse[P, F, T any](policy navigationPolicy) func(Request[P, F], query.SimplePage[T], func(query.PageRequest) (string, error)) (SimpleResponse[T], error) {
	return func(request Request[P, F], page query.SimplePage[T], build func(query.PageRequest) (string, error)) (SimpleResponse[T], error) {
		if err := page.Validate(); err != nil {
			return SimpleResponse[T]{}, err
		}
		if page.Number != request.Page.Number || page.Size != request.Page.Size {
			return SimpleResponse[T]{}, fault.New(fault.Invalid, "page result does not match its HTTP request")
		}
		links, err := navigation(request.Page, page.HasMore, value.Optional[int64]{}, policy, build)
		if err != nil {
			return SimpleResponse[T]{}, err
		}
		return SimpleResponse[T]{Data: pageItems(page.Items), Meta: SimpleMeta{Number: page.Number, Size: page.Size, HasMore: page.HasMore}, Links: links}, nil
	}
}
