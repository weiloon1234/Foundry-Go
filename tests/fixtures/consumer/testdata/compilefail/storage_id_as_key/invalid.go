package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func invalid(disk *storage.Disk, id storage.DiskID) {
	_, _ = disk.Stat(context.Background(), id, storage.ReadOptions{})
}
