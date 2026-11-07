package storing

import (
	"context"
	"io"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin store-and-open
func StoreAndCopy(ctx context.Context, disk *storage.Disk, input io.Reader, destination io.Writer) error {
	key, err := storage.ParseKey("reports/annual.pdf")
	if err != nil {
		return err
	}
	if _, err := disk.Put(ctx, key, input, storage.PutOptions{ContentType: "application/pdf", Condition: storage.IfAbsent()}); err != nil {
		return err
	}
	body, _, err := disk.Open(ctx, key, storage.ReadOptions{})
	if err != nil {
		return err
	}
	defer body.Close()
	_, err = io.Copy(destination, body)
	return err
}

// doc-example:end store-and-open

// doc-example:begin deferred-storage-errors
func AuthorizedStream(disk *storage.Disk, key storage.ObjectKey, authorize func(context.Context) error) foundryhttp.Stream {
	return foundryhttp.StreamFrom(func(ctx context.Context) (foundryhttp.StreamContent, error) {
		if err := authorize(ctx); err != nil {
			return foundryhttp.StreamContent{}, err
		}
		body, info, err := disk.Open(ctx, key, storage.ReadOptions{})
		if err != nil {
			return foundryhttp.StreamContent{}, storagehttp.ResponseError(err)
		}
		return foundryhttp.StreamContent{Body: body, MediaType: foundryhttp.MediaType(info.Object.ContentType), Length: value.Set(info.Length)}, nil
	}).WithDisposition(foundryhttp.DispositionAttachment)
}

// doc-example:end deferred-storage-errors
