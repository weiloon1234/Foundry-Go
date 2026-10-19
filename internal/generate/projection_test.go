package generate

import (
	"reflect"
	"strings"
	"testing"
)

const projectionSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database/query"
 "github.com/weiloon1234/Foundry-Go/value"
)
type FoundryScope string
//foundry:model table=users primary=ID
type User struct { ID int; Name FoundryScope; Nickname value.Nullable[string] }
//foundry:projection
type Summary struct { Name FoundryScope; Nickname value.Nullable[string] }
func Report()query.ProjectionQuery[User,Summary] {
 return SelectSummary(QueryUsers(),SummarySelection[User]{Name:query.Value(UserFields().Name),Nickname:query.Value(UserFields().Nickname)})
}
type summaryAlias struct{}
func TemporalValues(){
 fields:=UserFields()
 q:=QueryUsers()
 instant:=query.FromUnixMillis(fields.ID)
 year:=query.Year(query.LocalAt(instant,query.UTCZone()))
 _=query.SelectValue(q,year.Value()).Where(year.Gte(1970))
 _=query.SelectValue(q,query.TruncateInstant(query.TransactionTime(q),query.TimestampDay,query.UTCZone()).Value())
}
func Derived(){
 source:=query.As[summaryAlias](Report(),"summary")
 fields:=SummaryFieldsAt(source.Scope())
 _=ProjectSummary(source).SelectName(query.Value(fields.Name)).SelectNickname(query.Value(fields.Nickname)).Query().Where(fields.Name.Eq(FoundryScope("Ada")))
 _=query.SelectValue(source,query.Value(fields.Name)).Where(fields.Name.Ne(FoundryScope("Grace")))
}
type childAlias struct{}
func LateralRecords(){
 outer:=query.As[summaryAlias](QueryUsers(),"parent")
 inner:=query.As[childAlias](QueryUsers(),"child")
 c:=query.Correlate(outer,inner)
 parent:=UserFieldsAt(query.OuterScope(c,outer.Scope()))
 fields:=UserFieldsAt(query.InnerScope(c,inner.Scope()))
 c=c.Where(query.Less(fields.ID,parent.ID))
 selected:=ProjectCorrelatedSummary(c).SelectName(query.Value(fields.Name)).SelectNickname(query.Value(fields.Nickname)).Query().OrderBy(fields.ID.Desc()).Limit(1)
 lateral:=query.AsLateral[struct{Summary bool}](selected,"latest")
 joined:=query.LeftJoinLateral(outer,lateral)
 nullable:=SummaryNullableFieldsAt(query.NullableRightScope(joined,lateral.Scope()))
 _=query.SelectValue(joined,query.Value(nullable.Name))
 record:=query.AsLateral[struct{Record bool}](query.SelectCorrelatedRecord(c,query.InnerScope(c,inner.Scope())),"child_record")
 complete:=query.CrossJoinLateral(outer,record)
 _=query.SelectRecord(complete,query.RightScope(complete,record.Scope()))
}
func CommonTables(){
 definition:=query.CTE("summaries",Report()).Materialized()
 source:=query.As[summaryAlias](definition,"summary")
 fields:=SummaryFieldsAt(source.Scope())
 _=query.SelectValue(source,query.Value(fields.Name)).Where(fields.Name.Eq(FoundryScope("Ada")))
}
func SetOperations(){
 combined:=Report().UnionAll(Report())
 fields:=SummaryFieldsAt(combined.Scope())
 _=combined.Where(fields.Name.Eq(FoundryScope("Ada"))).OrderBy(fields.Name.Asc())
 values:=query.SelectValue(combined,query.Value(fields.Name))
 joined:=values.Union(values)
 _=joined.OrderBy(joined.Value().Asc()).Limit(2)
}
func WholeRecords(){
 users:=QueryUsers()
 _=query.SelectRecord(users,users.Scope())
 summary:=query.As[summaryAlias](Report(),"summary")
 _=query.SelectRecord(summary,summary.Scope()).Union(Report())
 child:=query.As[childAlias](QueryUsers(),"child")
 parent:=query.As[summaryAlias](users,"parent")
 joined:=query.InnerJoin(child,parent,query.On(UserFieldsAt(child.Scope()).ID,UserFieldsAt(parent.Scope()).ID))
 _=query.SelectRecord(joined,query.LeftScope(joined,child.Scope())).Union(users)
}
func DistinctReads(){
 fields:=UserFields()
 _=QueryUsers().Distinct().Where(fields.ID.Eq(1))
 _=QueryUsers().DistinctOn(fields.Name.Group()).OrderBy(fields.Name.Asc(),fields.ID.Desc())
 _=Report().Distinct().OrderBy(fields.Name.Asc())
 _=query.SelectValue(QueryUsers(),query.Value(fields.Name)).Distinct()
}
func WindowReads(){
 fields:=UserFields()
 window:=query.WindowFor(QueryUsers()).PartitionBy(fields.Name.Group()).OrderBy(fields.ID.Asc()).RowsBetween(query.UnboundedPreceding(),query.CurrentRow())
 _=query.SelectValue(QueryUsers(),query.RowNumber(window))
 _=query.SelectValue(QueryUsers(),query.CountField(fields.ID).Over(window))
 _=SelectSummary(QueryUsers(),SummarySelection[User]{Name:query.LagOr(query.Value(fields.Name),1,FoundryScope("unknown"),window),Nickname:query.LeadNullable(query.Value(fields.Nickname),1,window)})
}
func FilteredReads(){
 fields:=UserFields()
 count:=query.Count[User]().Filter(fields.ID.Gt(1))
 _=query.SelectValue(QueryUsers(),count.Value()).Having(count.Gt(0))
 _=query.SelectValue(QueryUsers(),query.Min(fields.ID).Filter(fields.ID.Gt(1)).Value()).Having(query.Min(fields.ID).Filter(fields.ID.Gt(1)).Gt(2))
 _=query.SelectValue(QueryUsers(),count.Over(query.WindowFor(QueryUsers())))
}
func ConditionalReads(){
 fields:=UserFields()
 display:=query.Coalesce(fields.Nickname,query.Param(fields.Nickname, "unknown"))
 label:=query.When(fields.ID.Gt(1),query.Param(fields.Name, FoundryScope("many"))).Else(fields.Name)
 _=SelectSummary(QueryUsers().Where(display.Ne("")),SummarySelection[User]{Name:label.Value(),Nickname:query.NullIf(display,query.Param(fields.Nickname, "unknown")).Value()})
 _=query.SelectValue(QueryUsers(),query.When(fields.ID.Gt(1),fields.ID).ElseNull().Value())
 count:=query.Count[User]()
 _=query.SelectValue(QueryUsers(),query.WhenValue(count.Gt(1),count.Value()).Else(count.Param(0).Value()))
}
func ComputedKeys(){
 fields:=UserFields()
 display:=query.Coalesce(fields.Nickname,query.Param(fields.Nickname, "unknown"))
 _=query.SelectValue(QueryUsers(),display.Value()).GroupBy(display.Group())
 _=QueryUsers().DistinctOnValues(display.Value().Key()).OrderBy(display.Asc())
 count:=query.Count[User]()
 _=query.SelectValue(QueryUsers(),query.RowNumber(query.WindowFor(QueryUsers()).PartitionByValues(count.Value().Key())))
 values:=query.SelectValue(QueryUsers(),query.Value(fields.ID))
 combined:=values.UnionAll(values)
 _=combined.DistinctOnValues(combined.Value().Key()).OrderBy(combined.Value().Asc())
}
func WindowExtensions(){
 fields:=UserFields()
 base:=query.WindowFor(QueryUsers()).PartitionBy(fields.Name.Group()).Named("names")
 rangeWindow:=query.NumericRange(base,query.Value(fields.ID))
 frame:=rangeWindow.Between(rangeWindow.Preceding(2),rangeWindow.CurrentRow()).Named("recent")
 _=query.SelectValue(QueryUsers(),query.Count[User]().Over(frame))
 nullable:=query.When(fields.ID.Gt(1),fields.ID).ElseNull()
 nr:=query.NullableNumericRange(query.WindowFor(QueryUsers()),nullable.Value())
 _=query.SelectValue(QueryUsers(),query.RowNumber(nr.Between(nr.CurrentRow(),nr.Following(2))))
}
func ScalarOperations(){
 fields:=UserFields()
 shifted:=query.Add(fields.ID,query.Param(fields.ID, 2))
 _=QueryUsers().Where(shifted.Gte(3)).OrderBy(shifted.Desc())
 _=query.SelectValue(QueryUsers(),query.DecimalOf(fields.ID).Value())
 _=query.SelectValue(QueryUsers(),query.ConcatWSNullable("|",query.NullableRow(query.Lower(fields.Name)),fields.Nickname).Value())
 count:=query.Count[User]()
 calculated:=query.AddValue(count.Value(),count.Param(1).Value())
 _=query.SelectValue(QueryUsers(),calculated).Having(query.OrderValue(calculated).Gt(1))
}
func ValueComparisons(){
 f:=UserFields()
 _=QueryUsers().Where(query.Less(query.Add(f.ID,query.Param(f.ID, 1)),query.Param(f.ID, 5)))
 count:=query.Count[User]()
 _=query.SelectValue(QueryUsers(),count.Value()).Having(query.GreaterValue(count.Value(),count.Param(0).Value()))
 a,b:=query.As[struct{a bool}](QueryUsers(),"a"),query.As[struct{b bool}](QueryUsers(),"b")
 af,bf:=UserFieldsAt(a.Scope()),UserFieldsAt(b.Scope())
 joined:=query.InnerJoin(a,b,query.OnLess(query.Add(af.ID,query.Param(af.ID, 1)),bf.ID))
 _=query.SelectRecord(joined,query.LeftScope(joined,a.Scope()))
 cross:=query.CrossJoin(a,b)
 _=query.SelectRecord(cross,query.RightScope(cross,b.Scope()))
 scalar:=query.ScalarRowQuery(QueryUsers(),query.SelectValue(QueryUsers(),query.Value(f.ID)).Limit(1))
 _=QueryUsers().Where(query.Equal(query.NullableRow(f.ID),scalar))
}
func PagedReports()(query.Page[Summary],error){
 return Report().OrderBy(UserFields().Name.Asc()).Paginate(nil,nil,query.PageRequest{Number:1,Size:2})
}
func SimpleModels()(query.SimplePage[User],error){
 return QueryUsers().SimplePaginate(nil,nil,query.PageRequest{Number:1,Size:2})
}
func SimpleReports()(query.SimplePage[Summary],error){
 return Report().OrderBy(UserFields().Name.Asc()).SimplePaginate(nil,nil,query.PageRequest{Number:1,Size:2})
}
func CursorReports()(query.CursorPage[Summary],error){
 cursor:=query.CursorFor(Report())
 fields:=SummaryFieldsAt(cursor.Scope())
 return cursor.OrderBy(fields.Nickname.Desc()).UniqueBy(fields.Name.Group()).Paginate(nil,nil,query.CursorRequest[Summary]{Size:2})
}
func CursorValues()(query.CursorPage[value.Nullable[string]],error){
 cursor:=query.ValueCursorFor(query.SelectValue(QueryUsers(),query.Value(UserFields().Nickname)).Distinct())
 return cursor.OrderBy(cursor.Value().Asc()).UniqueBy(cursor.Key()).Paginate(nil,nil,query.CursorRequest[value.Nullable[string]]{Size:2})
}
func ModelChunks()error{
 if err:=QueryUsers().Chunk(nil,nil,2,func([]User)error{return nil});err!=nil{return err}
 if err:=QueryUsers().ChunkByID(nil,nil,2,func([]User)error{return nil});err!=nil{return err}
 if err:=QueryUsers().EachChunked(nil,nil,2,func(User)error{return nil});err!=nil{return err}
 return QueryUsers().EachByID(nil,nil,2,func(User)error{return nil})
}
func TypedUpserts(){
 policy:=query.OnConflict(UserFields().ID).DoUpdate(UserFields().Name.Incoming(),UserFields().Nickname.SetNull())
 _,_=QueryUsers().Upsert(nil,nil,UserDraft{}.SetID(1).SetName(FoundryScope("Ada")),policy)
 _,_=QueryUsers().CreateMany(nil,nil,[]UserDraft{{}})
 _,_=QueryUsers().UpsertMany(nil,nil,[]UserDraft{{}},query.OnConflict[User]().DoNothing())
}
func TypedLocks(){
 _,_=QueryUsers().ForUpdate().SkipLocked().Where(UserFields().ID.Eq(1)).Find(nil,nil,1)
 _,_=QueryUsers().ForNoKeyUpdate().NoWait().RequireFind(nil,nil,1)
 _,_=Report().ForShare().Of(QueryUsers().Scope()).Wait().All(nil,nil)
 _,_=query.SelectValue(QueryUsers(),query.Value(UserFields().Name)).ForKeyShare().First(nil,nil)
}
func TransactionRecords(){
 source:=query.AsTransaction[summaryAlias](query.TransactionCTE("claimed",QueryUsers().ForUpdate()),"claim")
 fields:=UserFieldsAt(source.Scope())
 selected:=ProjectTransactionSummary(source).SelectName(query.Value(fields.Name)).SelectNickname(query.Value(fields.Nickname)).Query()
 _,_=selected.All(nil,nil)
 nested:=query.AsTransaction[childAlias](query.TransactionCTE("summaries",selected),"summary")
 _,_=query.SelectTransactionRecord(nested,nested.Scope()).Count(nil,nil)
}
func TransactionCorrelatedRecords(){
 parent:=query.AsTransaction[summaryAlias](query.TransactionOf(QueryUsers()),"parent")
 child:=query.AsTransaction[childAlias](query.TransactionOf(QueryUsers()),"child")
 c:=query.TransactionCorrelate(parent,child)
 p:=UserFieldsAt(query.OuterScope(c,parent.Scope()))
 fields:=UserFieldsAt(query.InnerScope(c,child.Scope()))
 c=c.Where(query.Equal(fields.ID,p.ID))
 selected:=ProjectTransactionCorrelatedSummary(c).SelectName(query.Value(fields.Name)).SelectNickname(query.Value(fields.Nickname)).Query().ForUpdate().Of(query.InnerScope(c,child.Scope()))
 lateral:=query.AsTransactionLateral[struct{locked bool}](selected,"selected")
 joined:=query.TransactionLeftJoinLateral(parent,lateral)
 nullable:=SummaryNullableFieldsAt(query.NullableRightScope(joined,lateral.Scope()))
 _,_=query.SelectTransactionValue(joined,query.Value(nullable.Name)).All(nil,nil)
}
func RecursiveTables(){
 definition:=query.RecursiveCTE("summary_tree",Report(),func(self query.RecursiveSelf[Summary]) query.RecordQuerySource[Summary] {
  parent:=query.As[summaryAlias](self,"parent")
  child:=query.As[childAlias](Report(),"child")
  joined:=query.InnerJoin(child,parent,query.On(SummaryFieldsAt(child.Scope()).Name,SummaryFieldsAt(parent.Scope()).Name))
  return query.SelectRecord(joined,query.LeftScope(joined,child.Scope()))
 })
 source:=query.As[summaryAlias](definition,"result")
 _=query.SelectRecord(source,source.Scope())
}
func Correlated(){
 child:=query.As[childAlias](QueryUsers(),"child")
 link:=query.Correlate(QueryUsers(),child)
 parent:=UserFieldsAt(query.OuterScope(link,QueryUsers().Scope()))
 fields:=UserFieldsAt(query.InnerScope(link,child.Scope()))
 values:=query.SelectCorrelatedValue(link,query.Value(fields.ID)).Where(fields.ID.EqColumn(parent.ID)).Limit(1)
 _=QueryUsers().Where(values.Exists())
 _=query.SelectValue(QueryUsers(),query.CorrelatedScalarQuery(values))
}
`

func TestFreshProjectionGeneration(t *testing.T) {
	dir := fixture(t, projectionSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["summary_foundry.gen.go"]
	for _, want := range []string{"func SelectSummary[FoundryScope1 any]", "ProjectionQuery[FoundryScope1, Summary]", "ProjectionDefinition[Summary]", "func SelectTransactionSummary[FoundryScope1 ", ".TransactionScope]", "TransactionQuery[FoundryScope1, Summary]", "func ProjectTransactionCorrelatedSummary[", "TransactionCorrelatedRecordQuery["} {
		if !strings.Contains(output, want) {
			t.Fatal("projection did not generate safe concrete declarations", want)
		}
	}
	if strings.Contains(output, "SummaryDraft") || strings.Contains(output, "FoundryQuery()") {
		t.Fatal("projection gained persisted model methods")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("projection output is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidProjectionDeclarationsDoNotPublish(t *testing.T) {
	for _, source := range []string{
		strings.Replace(projectionSource, "//foundry:projection", "//foundry:projection table=users", 1),
		strings.Replace(projectionSource, "type Summary struct { Name FoundryScope; Nickname value.Nullable[string] }", "type Summary struct{}", 1),
		strings.Replace(projectionSource, "type Summary struct { Name FoundryScope; Nickname value.Nullable[string] }", "type Summary struct { Name FoundryScope; Nickname string }", 1),
		strings.Replace(projectionSource, "type Summary struct { Name FoundryScope; Nickname value.Nullable[string] }", "type Summary struct { Name FoundryScope `foundry:\"default=database\"`; Nickname value.Nullable[string] }", 1),
		strings.Replace(projectionSource, "type Summary struct { Name FoundryScope; Nickname value.Nullable[string] }", "type Summary struct { Name FoundryScope `foundry:\"name=display\"`; Nickname value.Nullable[string] }", 1),
		strings.Replace(projectionSource, "type Summary struct { Name FoundryScope; Nickname value.Nullable[string] }", "type Summary struct { Name FoundryScope `foundry:\"-\"`; Nickname value.Nullable[string] }", 1),
		strings.Replace(projectionSource, "Name:query.Value(UserFields().Name)", "Name:query.Value(UserFields().ID)", 1),
		strings.Replace(projectionSource, "SummaryFieldsAt(source.Scope())", "UserFieldsAt(source.Scope())", 1),
		strings.Replace(projectionSource, "fields.Name.Eq(FoundryScope(\"Ada\"))", "UserFields().Name.Eq(FoundryScope(\"Ada\"))", 1),
		projectionSource + "\nfunc SummaryFieldsAt(){}\n",
		projectionSource + "\nfunc ProjectCorrelatedSummary(){}\n",
		projectionSource + "\n//foundry:projection\ntype CorrelatedSummary struct { Name string }\n",
		projectionSource + "\nfunc ProjectTransactionSummary(){}\n",
		projectionSource + "\n//foundry:projection\ntype TransactionSummary struct { Name string }\n",
		projectionSource + "\nfunc ProjectTransactionCorrelatedSummary(){}\n",
		projectionSource + "\n//foundry:projection\ntype TransactionCorrelatedSummary struct { Name string }\n",
	} {
		dir := fixture(t, source)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("invalid projection generated")
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("invalid projection published output")
		}
	}
}

func TestProjectionAndModelDecoderLocalsDoNotShadowConsumerTypes(t *testing.T) {
	dir := fixture(t, `package sample
type item string
type row string
type fields string
//foundry:model table=records primary=ID
type Record struct{ID int;First item;Second row;Third fields}
//foundry:projection
type Result struct{First item;Second row;Third fields}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}
