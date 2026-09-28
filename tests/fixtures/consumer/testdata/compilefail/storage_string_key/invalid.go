package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func invalid(disk *storage.Disk, key string) {
	_, _ = disk.Stat(context.Background(), key, storage.ReadOptions{})
}
