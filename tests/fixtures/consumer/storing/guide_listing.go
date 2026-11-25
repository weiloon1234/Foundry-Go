package storing

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/storage"
)

// doc-example:begin storage-1
func listReports(ctx context.Context, disk *storage.Disk) error {
	prefix, err := storage.ParsePrefix("reports/")
	if err != nil {
		return err
	}
	page, err := disk.List(ctx, storage.ListOptions{Prefix: prefix, Limit: 100})
	if err != nil {
		return err
	}
	// Use page.Next unchanged with the same disk and prefix for the next page.
	_ = page
	return nil
}

// doc-example:end storage-1

// doc-example:begin delete-prefix-page
type prefixPageResult struct {
	storage.DeleteManyResult
	Skipped int
	Next    storage.Cursor
}

func deletePage(ctx context.Context, disk *storage.Disk, prefix storage.Prefix, cursor storage.Cursor) (prefixPageResult, error) {
	if prefix.String() == "" {
		return prefixPageResult{}, fmt.Errorf("a nonempty prefix is required")
	}
	page, err := disk.List(ctx, storage.ListOptions{Prefix: prefix, Cursor: cursor, Limit: storage.MaxBatchDelete})
	if err != nil {
		return prefixPageResult{}, err
	}
	result := prefixPageResult{Skipped: page.Skipped, Next: page.Next}
	keys := make([]storage.ObjectKey, len(page.Objects))
	for i, object := range page.Objects {
		keys[i] = object.Key
	}
	if len(keys) == 0 {
		return result, nil
	}
	result.DeleteManyResult, err = disk.DeleteMany(ctx, keys)
	return result, err
}

// doc-example:end delete-prefix-page
