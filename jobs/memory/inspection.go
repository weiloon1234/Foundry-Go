package memory

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

func (b *Backend) JobList(ctx context.Context, key jobs.Key, options jobs.ListOptions) (jobs.Page, error) {
	if err := options.Validate(); err != nil {
		return jobs.Page{}, err
	}
	_, release, err := b.begin(ctx, key)
	if err != nil {
		return jobs.Page{}, err
	}
	defer release()
	var ids []jobs.ExecutionID
	for at := range b.entries {
		if at.queue == key && (options.After.IsZero() || at.id.String() > options.After.String()) {
			ids = append(ids, at.id)
		}
	}
	slices.SortFunc(ids, func(a, b jobs.ExecutionID) int { return strings.Compare(a.String(), b.String()) })
	result := jobs.Page{}
	for i, id := range ids {
		record := b.entries[address{key, id}].record
		if options.Matches(record) {
			record.History = slices.Clone(record.History)
			result.Records = append(result.Records, record)
		}
		if len(result.Records) == options.Limit || i+1 == jobs.ListScanLimit {
			if i+1 < len(ids) {
				result.Next = id
			}
			break
		}
	}
	return result, nil
}
