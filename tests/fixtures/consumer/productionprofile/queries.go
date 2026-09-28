package productionprofile

import "github.com/weiloon1234/Foundry-Go/database/query"

// Check exercises ordinary generated drafts and typed query composition without
// opening a database, listener, queue, or application resource.
func Check() error {
	f := RecordFields()
	base := QueryProfileRecords().Where(f.Active.Eq(true)).Where(f.Score.Gte(10)).OrderBy(f.Score.Desc(), f.ID.Asc()).Limit(25)
	if _, err := base.Compile(); err != nil {
		return err
	}
	if _, err := query.SelectValue(QueryProfileRecords().Where(f.Active.Eq(true)), f.Score.Sum().Value()).Compile(); err != nil {
		return err
	}
	_ = RecordDraft{}.SetName("profile").SetPayload([]byte("owned"))
	return nil
}
