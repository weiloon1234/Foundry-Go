package retrieval_read

import (
	"context"
	n "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Read(ctx context.Context, db *database.DB) ([]n.Member, error) {
	return orm.All(ctx, db, n.Members.Query())
}
func Observe(observer orm.RetrievalObserver[n.Member]) { _, _ = observer.Declare(n.LocalReads) }
