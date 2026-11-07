package developeroperations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/logging"
)

// doc-example:begin log-file-controls
func TailLog(ctx context.Context, owner *logging.Sink, id logging.FileID) (logging.FileChunk, error) {
	return owner.ReadFile(ctx, logging.FileRead{ID: id, Offset: -1, Limit: 65536})
}

func RotateLog(ctx context.Context, owner *logging.Sink, id logging.FileID) error {
	return owner.RotateFile(ctx, id)
}

func ClearLog(ctx context.Context, owner *logging.Sink, id logging.FileID) error {
	return owner.ClearFile(ctx, id)
}

func DeleteLogArchive(ctx context.Context, owner *logging.Sink, id logging.FileID) error {
	return owner.DeleteArchive(ctx, id)
}

// doc-example:end log-file-controls
