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
	if _, err := query.SelectValue(QueryProfileRecords().Where(f.Active.Eq(true)), query.Sum(f.Score).Value()).Compile(); err != nil {
		return err
	}
	_ = RecordDraft{}.SetName("profile").SetPayload([]byte("owned"))
	return nil
}

type resourceLeft struct{}
type resourceRight struct{}

// CheckJoinedFields exercises the same generated field capabilities in a
// self-join, including the nullable side's text, binary and ordered values.
func CheckJoinedFields() error {
	left := query.As[resourceLeft](QueryProfileRecords(), "left_records")
	right := query.As[resourceRight](QueryProfileRecords(), "right_records")
	joined := query.LeftJoin(left, right, query.On(RecordFieldsAt(left.Scope()).ID, RecordFieldsAt(right.Scope()).ID))
	fields := RecordFieldsAt(query.LeftScope(joined, left.Scope()))
	nullable := RecordNullableFieldsAt(query.NullableRightScope(joined, right.Scope()))
	caption := query.Coalesce(nullable.Name, fields.Name)
	text := query.SelectValue(joined, query.Lower(caption).Value()).Where(fields.Active.Eq(true), nullable.Score.IsNotNull()).OrderBy(caption.Asc(), fields.ID.Asc())
	if _, err := text.Compile(); err != nil {
		return err
	}
	_, err := query.SelectValue(joined, query.Coalesce(nullable.Payload, fields.Payload).Value()).Compile()
	return err
}
