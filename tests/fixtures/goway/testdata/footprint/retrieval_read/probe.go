package retrieval_read

import (
	"context"
	n "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Read(ctx context.Context, db *database.DB) ([]n.Member, error) {
	return n.Members.Query().All(ctx, db)
}
func Observe(observer orm.RetrievalObserver[n.Member]) { _, _ = observer.Declare(n.LocalReads) }
