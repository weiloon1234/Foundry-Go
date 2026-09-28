package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func wrong(b pubsub.Backend, key cache.EntryKey) {
	_, _ = b.Publish(context.Background(), key, []byte("x"))
}
