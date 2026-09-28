package invalid

import "foundry.test/consumer/linkqueries"

func invalid() {
	_ = linkqueries.ForceDeleteArchiveUsing(linkqueries.QueryLinkArchives(), linkqueries.QueryLinkArchives())
}
