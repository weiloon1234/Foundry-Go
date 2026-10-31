package orm

import (
	"go/types"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestPublicMethodSurface pins exported fields and methods, and sealed interface
// methods, independently of receiver placement and promotion. Update only for an approved API change.
func TestPublicMethodSurface(t *testing.T) {
	var out strings.Builder
	for _, dir := range []string{".", "./rows", "./expr"} {
		pkg := surfacePackageIn(t, dir, "")
		for _, name := range pkg.Scope().Names() {
			obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || !obj.Exported() {
				continue
			}
			typ := types.Unalias(obj.Type())
			switch typ.Underlying().(type) {
			case *types.Struct, *types.Interface:
			default:
				continue
			}
			recordMethodSurface(&out, pkg.Name()+"."+name, typ)
		}
		for _, name := range map[string][]string{
			".":      {"BoolOf", "FoundryModel", "FoundryOrderBy", "FoundryWhere", "FoundryRelated", "ValueOf", "Pivot", "WhereRaw", "FoundryRecord", "FoundryLockedRecord", "WithRelationWriteLimit", "Attach", "AttachMany", "Detach", "ForceDetach", "DetachMany", "DetachAll", "Sync", "SyncWithoutDetaching", "Toggle", "UpdatePivot", "Update", "Delete", "UpdateEach", "DeleteEach", "RestoreEach", "ForceDeleteEach", "UUIDLess", "UUIDLessOrEqual", "UUIDGreater", "UUIDGreaterOrEqual", "IdentityOf", "SlotOf", "FoundrySlot", "Keyed", "Unique", "RelatedUnique", "InsertEach", "ParseCursor", "CreateEach", "CreateOrFirst", "FirstOrCreate", "UpdateOrCreate", "PruneBatch", "ForcePruneBatch", "Explain", "ExplainAnalyze", "ExplainLocked", "ExplainAnalyzeLocked", "FoundryStatement", "FoundryPredicate", "FoundryRelationPredicate", "FoundryExtension", "Lookup", "FoundryRelationFilter", "FoundryModelRead", "FoundryRelatedValue", "FoundryRelationOrder", "FoundryRelationPivotOrder"},
			"./rows": {"BoolOf", "FromModel", "GroupBy", "Having", "OrderBy", "Distinct", "DistinctOn", "DistinctOnValues", "MapSelected", "As", "Scope", "On", "InnerJoin", "LeftJoin", "RightJoin", "FullJoin", "CrossJoin", "LeftScope", "RightScope", "NullableLeftScope", "NullableRightScope", "LeftNullableScope", "OnEqual", "OnNotEqual", "OnLess", "OnLessNullable", "OnLessOrEqual", "OnLessOrEqualNullable", "OnGreater", "OnGreaterNullable", "OnGreaterOrEqual", "OnGreaterOrEqualNullable", "OnLike", "OnLikeNullable", "OnDistinctFrom", "OnNotDistinctFrom", "OnAnd", "OnOr", "From", "FromRows", "SelectScalar", "WhereScalar", "ExistsQuery", "InQuery", "InNullableQuery", "ScalarRowQuery", "ScalarNullableRowQuery", "Correlate", "ModelScope", "OuterScope", "InnerScope", "OuterNullableScope", "InnerNullableScope", "SelectCorrelatedScalar", "CorrelatedScalarRowQuery", "CorrelatedScalarNullableRowQuery", "InCorrelatedQuery", "InNullableCorrelatedQuery", "SelectCorrelatedRecord", "ProjectCorrelated", "AsLateral", "LateralScope", "CrossJoinLateral", "InnerJoinLateral", "LeftJoinLateral", "At", "AtNullable", "AtNullableValue", "RecordOf", "ModelRecord", "AsRecord", "SelectRecord", "Limit", "Offset", "TransactionOf", "LockedRecord", "AsTransaction", "TransactionScope", "SelectTransaction", "SelectTransactionRecord", "CTE", "TransactionCTE", "RecursiveCTE", "RecursiveAllCTE", "Values", "Union", "UnionValues", "UnionAll", "UnionAllValues", "Intersect", "IntersectValues", "IntersectAll", "IntersectAllValues", "Except", "ExceptValues", "ExceptAll", "ExceptAllValues", "CursorFor", "ValueCursorFor", "Read", "ReadScalar", "ReadRecord", "ReadSet", "ReadValues", "SelectTransactionValue", "Target", "LockRows", "UpdateLock", "NoKeyUpdateLock", "ShareLock", "KeyShareLock", "ForUpdate", "ForNoKeyUpdate", "ForShare", "ForKeyShare", "ConflictRowsOf", "OnConflictKeys", "ConflictTargetWhere", "ConflictWhereRows", "SetConflictValue", "ConflictScalarRowQuery", "ConflictScalarNullableRowQuery", "MapInsert", "MapUpdate", "MatchSource", "MatchNullableSource", "InsertFrom", "UpdateFrom", "DeleteUsing", "ForceDeleteUsing", "ProjectTransactionCorrelated", "SelectTransactionCorrelatedRecord", "SelectTransactionCorrelatedScalar", "AsTransactionLateral", "TransactionCorrelate", "TransactionCorrelatedScalarNullableRowQuery", "TransactionCorrelatedScalarRowQuery", "TransactionCrossJoin", "TransactionCrossJoinLateral", "TransactionExistsQuery", "TransactionFullJoin", "TransactionInCorrelatedQuery", "TransactionInNullableCorrelatedQuery", "TransactionInNullableQuery", "TransactionInQuery", "TransactionInnerJoin", "TransactionInnerJoinLateral", "TransactionInnerNullableScope", "TransactionInnerScope", "TransactionLateralScope", "TransactionLeftJoin", "TransactionLeftJoinLateral", "TransactionOuterNullableScope", "TransactionOuterScope", "TransactionRightJoin", "TransactionScalarNullableRowQuery", "TransactionScalarOf", "TransactionScalarRowQuery", "TransactionSubqueryOf", "TransactionValueOf", "ExplainCursor", "ExplainAnalyzeCursor", "ExplainValueCursor", "ExplainAnalyzeValueCursor", "Explain", "ExplainAnalyze", "ExplainTransaction", "ExplainAnalyzeTransaction", "ExplainTransactionValue", "ExplainAnalyzeTransactionValue", "ReorderUnwindowed", "OrderScalar", "LimitScalar", "OffsetScalar", "HavingCorrelatedRecord", "DistinctCorrelatedRecord", "DistinctOnCorrelatedScalar", "CompileRead", "PreflightRead", "EachRead", "GroupByScalar", "HavingScalar", "DistinctScalar", "DistinctOnScalar", "DistinctOnScalarValues", "DistinctRecord", "DistinctSet", "DistinctValues", "GroupByCorrelatedScalar", "DistinctOnCorrelatedScalarValues", "OrderCorrelatedScalar", "DistinctOnValueSet", "WithValue", "DistinctOnRecord", "CompileCursor", "PreflightCursor", "CompileValueCursor", "PreflightValueCursor"},
			"./expr": {"AsSelected", "ParamOf", "Filter", "Key", "AscValue", "DescValue", "EqValue", "NeValue", "InValue", "LtValue", "LteValue", "GtValue", "GteValue", "IsNullValue", "IsNotNullValue", "TextLikeValue", "TextILikeValue", "TextContainsValue", "TextIContainsValue", "TextStartsWithValue", "TextEndsWithValue", "TextIStartsWithValue", "TextIEndsWithValue", "ValueFilter", "NullableValueFilter", "OrderValueFilter", "NullableOrderValueFilter", "TextValueFilter", "NullableTextValueFilter", "HavingNot", "Eq", "Ne", "In", "NullableEq", "NullableNe", "NullableIn", "Lt", "Lte", "Gt", "Gte", "NullableLt", "NullableLte", "NullableGt", "NullableGte", "IsNull", "IsNotNull", "Asc", "Desc", "TextLike", "TextILike", "TextContains", "TextIContains", "TextStartsWith", "TextEndsWith", "TextIStartsWith", "TextIEndsWith", "NullableTextLike", "NullableTextILike", "NullableTextContains", "NullableTextIContains", "NullableTextStartsWith", "NullableTextEndsWith", "NullableTextIStartsWith", "NullableTextIEndsWith", "TextFilter", "NullableTextFilter", "OrderFilter", "NullableOrderFilter", "CompareFilter", "NullableCompareFilter", "Constant", "Row", "Related", "Sum", "SumAt", "Avg", "AvgAt", "SumFloat", "SumFloatAt", "AvgFloat", "AvgFloatAt", "SumInterval", "SumIntervalAt", "AvgInterval", "AvgIntervalAt", "Min", "MinAt", "Max", "MaxAt", "CountField", "CountFieldAt", "CountDistinctField", "CountDistinctFieldAt", "FoundryAggregate", "Group", "HavingAnd", "HavingOr", "FoundryHaving", "FoundryGroup", "FoundryKey", "FoundryProjectionOrder", "AtNullable", "AtNullableValue", "FoundryNullableScope", "FoundryNullableScopeData", "Coalesce", "CoalesceValue", "CoalesceNullable", "CoalesceNullableValue", "NullIf", "NullIfValue", "NullIfNullable", "NullIfNullableValue", "When", "WhenValue", "Grouped", "Abs", "AbsNullable", "AbsNullableValue", "AbsValue", "Add", "AddNullable", "AddNullableValue", "AddValue", "CompareValue", "Count", "DecimalNullable", "DecimalNullableValue", "DecimalOf", "DecimalValue", "DistinctFrom", "DistinctFromValue", "Divide", "DivideNullable", "DivideNullableValue", "DivideValue", "Equal", "EqualValue", "Exists", "FloatNullable", "FloatNullableValue", "FloatOf", "FloatValue", "FoundrySelected", "Greater", "GreaterColumn", "GreaterNullable", "GreaterNullableValue", "GreaterOrEqual", "GreaterOrEqualNullable", "GreaterOrEqualNullableValue", "GreaterOrEqualValue", "GreaterValue", "Less", "LessNullable", "LessNullableValue", "LessOrEqual", "LessOrEqualNullable", "LessOrEqualNullableValue", "LessOrEqualValue", "LessValue", "Like", "LikeNullable", "LikeNullableValue", "LikeValue", "Multiply", "MultiplyNullable", "MultiplyNullableValue", "MultiplyValue", "Negate", "NegateNullable", "NegateNullableValue", "NegateValue", "NotDistinctFrom", "NotDistinctFromValue", "NotEqual", "NotEqualValue", "Nullable", "NullableRow", "OrderBy", "OrderNullableRow", "OrderNullableValue", "OrderRow", "OrderValue", "Param", "Remainder", "RemainderNullable", "RemainderNullableValue", "RemainderValue", "Subtract", "SubtractNullable", "SubtractNullableValue", "SubtractValue", "Where", "Not", "FoundryRowValue", "FoundrySelectedColumn", "FoundryRowExpression", "FoundrySelectedValue", "FoundryPredicateValue", "Raw", "RawWithCodec", "Lower", "LowerValue", "LowerNullable", "LowerNullableValue", "Upper", "UpperValue", "UpperNullable", "UpperNullableValue", "Trim", "TrimValue", "TrimNullable", "TrimNullableValue", "TrimLeft", "TrimLeftValue", "TrimLeftNullable", "TrimLeftNullableValue", "TrimRight", "TrimRightValue", "TrimRightNullable", "TrimRightNullableValue", "TrimChars", "TrimCharsValue", "TrimCharsNullable", "TrimCharsNullableValue", "TrimLeftChars", "TrimLeftCharsValue", "TrimLeftCharsNullable", "TrimLeftCharsNullableValue", "TrimRightChars", "TrimRightCharsValue", "TrimRightCharsNullable", "TrimRightCharsNullableValue", "Concat", "ConcatValue", "ConcatNullable", "ConcatNullableValue", "Replace", "ReplaceValue", "ReplaceNullable", "ReplaceNullableValue", "Length", "LengthValue", "LengthNullable", "LengthNullableValue", "OctetLength", "OctetLengthValue", "OctetLengthNullable", "OctetLengthNullableValue", "ConcatWS", "ConcatWSValue", "ConcatWSNullable", "ConcatWSNullableValue", "Substring", "SubstringValue", "SubstringNullable", "SubstringNullableValue", "SubstringFrom", "SubstringFromValue", "SubstringFromNullable", "SubstringFromNullableValue", "SubstringAt", "SubstringAtValue", "SubstringAtNullable", "SubstringAtNullableValue", "TextValue", "TextNullableValue", "TruncateDate", "TruncateDateValue", "TruncateDateNullable", "TruncateDateNullableValue", "TruncateLocal", "TruncateLocalValue", "TruncateLocalNullable", "TruncateLocalNullableValue", "TruncateInstant", "TruncateInstantValue", "TruncateInstantNullable", "TruncateInstantNullableValue", "FromUnixMillis", "FromUnixMillisValue", "FromUnixMillisNullable", "FromUnixMillisNullableValue", "LocalAt", "LocalAtValue", "LocalAtNullable", "LocalAtNullableValue", "ResolveLocal", "ResolveLocalValue", "ResolveLocalNullable", "ResolveLocalNullableValue", "DateOf", "DateOfValue", "DateOfNullable", "DateOfNullableValue", "TimeOf", "TimeOfValue", "TimeOfNullable", "TimeOfNullableValue", "CombineDateTime", "CombineDateTimeValue", "CombineDateTimeNullable", "CombineDateTimeNullableValue", "AddDateDays", "AddDateDaysValue", "AddDateDaysNullable", "AddDateDaysNullableValue", "DateDifference", "DateDifferenceValue", "DateDifferenceNullable", "DateDifferenceNullableValue", "AddDateInterval", "AddDateIntervalValue", "AddDateIntervalNullable", "AddDateIntervalNullableValue", "SubtractDateInterval", "SubtractDateIntervalValue", "SubtractDateIntervalNullable", "SubtractDateIntervalNullableValue", "AddLocalInterval", "AddLocalIntervalValue", "AddLocalIntervalNullable", "AddLocalIntervalNullableValue", "SubtractLocalInterval", "SubtractLocalIntervalValue", "SubtractLocalIntervalNullable", "SubtractLocalIntervalNullableValue", "AddInstantInterval", "AddInstantIntervalValue", "AddInstantIntervalNullable", "AddInstantIntervalNullableValue", "SubtractInstantInterval", "SubtractInstantIntervalValue", "SubtractInstantIntervalNullable", "SubtractInstantIntervalNullableValue", "AddClockElapsed", "AddClockElapsedValue", "AddClockElapsedNullable", "AddClockElapsedNullableValue", "SubtractClockElapsed", "SubtractClockElapsedValue", "SubtractClockElapsedNullable", "SubtractClockElapsedNullableValue", "TransactionTime", "Year", "YearValue", "YearNullable", "YearNullableValue", "Month", "MonthValue", "MonthNullable", "MonthNullableValue", "Day", "DayValue", "DayNullable", "DayNullableValue", "DayOfWeek", "DayOfWeekValue", "DayOfWeekNullable", "DayOfWeekNullableValue", "ISODayOfWeek", "ISODayOfWeekValue", "ISODayOfWeekNullable", "ISODayOfWeekNullableValue", "DayOfYear", "DayOfYearValue", "DayOfYearNullable", "DayOfYearNullableValue", "ISOWeek", "ISOWeekValue", "ISOWeekNullable", "ISOWeekNullableValue", "ISOYear", "ISOYearValue", "ISOYearNullable", "ISOYearNullableValue", "Quarter", "QuarterValue", "QuarterNullable", "QuarterNullableValue", "Century", "CenturyValue", "CenturyNullable", "CenturyNullableValue", "Decade", "DecadeValue", "DecadeNullable", "DecadeNullableValue", "Millennium", "MillenniumValue", "MillenniumNullable", "MillenniumNullableValue", "JulianDay", "JulianDayValue", "JulianDayNullable", "JulianDayNullableValue", "Hour", "HourValue", "HourNullable", "HourNullableValue", "Minute", "MinuteValue", "MinuteNullable", "MinuteNullableValue", "Second", "SecondValue", "SecondNullable", "SecondNullableValue", "Milliseconds", "MillisecondsValue", "MillisecondsNullable", "MillisecondsNullableValue", "Microseconds", "MicrosecondsValue", "MicrosecondsNullable", "MicrosecondsNullableValue", "UnixSeconds", "UnixSecondsValue", "UnixSecondsNullable", "UnixSecondsNullableValue", "UnixMilliseconds", "UnixMillisecondsValue", "UnixMillisecondsNullable", "UnixMillisecondsNullableValue", "LoadTimeZone", "UTCZone", "AddIntervals", "AddIntervalsValue", "AddIntervalsNullable", "AddIntervalsNullableValue", "SubtractIntervals", "SubtractIntervalsValue", "SubtractIntervalsNullable", "SubtractIntervalsNullableValue", "NegateInterval", "NegateIntervalValue", "NegateIntervalNullable", "NegateIntervalNullableValue", "LocalDifference", "LocalDifferenceValue", "LocalDifferenceNullable", "LocalDifferenceNullableValue", "InstantDifference", "InstantDifferenceValue", "InstantDifferenceNullable", "InstantDifferenceNullableValue", "ClockDifference", "ClockDifferenceValue", "ClockDifferenceNullable", "ClockDifferenceNullableValue", "IntervalMonths", "IntervalMonthsValue", "IntervalMonthsNullable", "IntervalMonthsNullableValue", "IntervalDays", "IntervalDaysValue", "IntervalDaysNullable", "IntervalDaysNullableValue", "IntervalElapsed", "IntervalElapsedValue", "IntervalElapsedNullable", "IntervalElapsedNullableValue", "ShiftDate", "ShiftDateValue", "ShiftDateNullable", "ShiftDateNullableValue", "ShiftLocal", "ShiftLocalValue", "ShiftLocalNullable", "ShiftLocalNullableValue", "ShiftInstant", "ShiftInstantValue", "ShiftInstantNullable", "ShiftInstantNullableValue", "NewGeographicPoint", "SphericalDistance", "SphericalDistanceValue", "SphericalDistanceNullable", "SphericalDistanceNullableValue", "Over", "RowNumber", "Rank", "DenseRank", "PercentRank", "CumeDist", "NTile", "Lag", "LagNullable", "LagOr", "Lead", "LeadNullable", "LeadOr", "FirstValue", "FirstNullableValue", "LastValue", "LastNullableValue", "NthValue", "NthNullableValue", "WindowFor", "UnboundedPreceding", "CurrentRow", "UnboundedFollowing", "Preceding", "Following", "NumericRange", "NullableNumericRange", "TemporalRange", "NullableTemporalRange", "JSONFieldOf", "JSONFixedArrayOf", "JSONRoot", "JSONNullableRoot", "JSONProperty", "JSONIndex", "JSONMapKey", "JSONArrayElement", "JSONScalar", "JSONTextScalar", "JSONOptional", "JSONNullable", "JSONPointer", "JSONDocument", "JSONType", "JSONTypeValue", "JSONTypeNullable", "JSONTypeNullableValue", "JSONContains", "JSONContainsValue", "JSONContainsNullable", "JSONContainsNullableValue", "JSONContainedBy", "JSONContainedByValue", "JSONContainedByNullable", "JSONContainedByNullableValue", "JSONLength", "JSONLengthValue", "JSONLengthNullable", "JSONLengthNullableValue", "UUIDLess", "UUIDLessOrEqual", "UUIDGreater", "UUIDGreaterOrEqual", "UUIDLessNullable", "UUIDLessOrEqualNullable", "UUIDGreaterNullable", "UUIDGreaterOrEqualNullable", "Or", "ModelPredicate", "Has", "CompareRow", "CompareNullableRow", "TextRow", "TextNullableRow", "TextRowAt", "TextNullableRowAt", "RelationFilter", "RelatedValue", "SumRow", "SumNullableRow", "AvgRow", "AvgNullableRow", "SumFloatRow", "SumFloatNullableRow", "AvgFloatRow", "AvgFloatNullableRow", "SumIntervalRow", "SumIntervalNullableRow", "AvgIntervalRow", "AvgIntervalNullableRow", "MinRow", "MinNullableRow", "MaxRow", "MaxNullableRow", "CountRow", "CountDistinctRow", "RelationOrder", "RelationPivotOrder"},
		}[dir] {
			out.WriteString(pkg.Name() + "." + name + " " + types.TypeString(pkg.Scope().Lookup(name).Type(), nil) + "\n")
		}
		if dir == "./rows" {
			out.WriteString("rows.Raw " + types.TypeString(pkg.Scope().Lookup("Raw").Type(), nil) + "\n")
		}
		if dir == "." {
			for _, name := range []string{"PrimaryOf", "SoftDeletes", "CreatedAtOf", "UpdatedAtOf", "SoftDeleteOf", "NumberKey", "TextKey", "FoundryInsertMapping", "FoundryUpdateMapping", "FoundrySourceKey", "FoundryInsertFrom", "FoundryUpdateFrom", "FoundryDeleteUsing", "FoundryConflictData", "FoundryConflict", "FoundryConflictUpdate", "ConflictKey", "ConflictSet", "ConflictNull", "ConflictIncoming", "OnConflict", "OnConflictConstraint", "Upsert", "UpsertMany", "AuditField", "AuditEncryptedField", "AuditNullableEncryptedField", "AuditPolicyOf", "Audit", "Auditing", "AuditRead", "AuditReadNullable", "AuditReadEncrypted", "AuditReadNullableEncrypted", "WithRetrievalHooks", "NewRetrievalObserver", "RegisterRetrievalObserver", "Scope", "WithGlobalScopes", "WithoutGlobalScope", "WithoutGlobalScopes", "WithScopeContext", "WithoutRelationGlobalScope", "WithoutRelationGlobalScopes", "WithoutPivotGlobalScope", "WithoutPivotGlobalScopes", "CursorPaginate", "ChunkByKey", "EachByKey", "HasOneOf", "Has", "LoadFrom", "LoadMissingFrom", "DefaultRelationLimits", "WithRelationLimits", "ThroughOf", "ManyToManyOf", "HasManyThroughOf", "HasOneThroughOf", "MorphManyOf", "MorphOneOf", "MorphToOf", "MorphToManyOf", "MorphedByManyOf", "InputKeyOf", "InputColumnOf", "NullableInputColumnOf", "InputTextOf", "NullableInputTextOf", "InputNumberOf", "NullableInputNumberOf", "InputTimeOf", "NullableInputTimeOf", "InputEnumOf", "NullableInputEnumOf", "InputJSONOf", "NullableInputJSONOf", "InputIntervalOf", "NullableInputIntervalOf", "InputBytesOf", "NullableInputBytesOf", "EncryptedChangeOf", "NullableEncryptedChangeOf", "WithHooks", "WriteGet", "WriteNullableGet", "WriteSet", "WriteUnset", "WriteIsSet", "ChangeOf", "NullableChangeOf", "Compare", "NewObserver", "NewDeletionObserver", "RegisterObserver", "WithoutModelHooks", "ForceDelete"} {
				out.WriteString("orm." + name + " " + types.TypeString(pkg.Scope().Lookup(name).Type(), nil) + "\n")
			}
			out.WriteString("orm.FoundryColumn " + types.TypeString(pkg.Scope().Lookup("FoundryColumn").Type(), nil) + "\n")
			for _, name := range []string{"NumberKey", "TextKey"} {
				sig := pkg.Scope().Lookup(name).Type().(*types.Signature)
				recordMethodSurface(&out, "orm."+name+" result", sig.Results().At(0).Type())
			}
			for _, name := range pkg.Scope().Names() {
				fn, ok := pkg.Scope().Lookup(name).(*types.Func)
				if !ok || !fn.Exported() {
					continue
				}
				sig := fn.Type().(*types.Signature)
				for i := 0; i < sig.Params().Len(); i++ {
					named, ok := types.Unalias(sig.Params().At(i).Type()).(*types.Named)
					if !ok || named.Obj().Pkg() != pkg {
						continue
					}
					if isDeclarationColumn(named.Obj().Name()) || (i == 0 || i == 2) && (named.Obj().Name() == "Query" || named.Obj().Name() == "LockedQuery" || named.Obj().Name() == "Table") {
						out.WriteString("orm.operation " + name + " " + types.TypeString(sig, nil) + "\n")
						break
					}
				}
			}

		}
	}
	const path = "testdata/public-methods.txt"
	if os.Getenv("FOUNDRY_UPDATE_METHOD_SURFACE") == "1" {
		if err := os.WriteFile(path, []byte(out.String()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("public surface changed; preserve pinned fields and signatures (R9) or obtain API approval\nwant:\n%s\ngot:\n%s", want, out.String())
	}
}

func recordMethodSurface(out *strings.Builder, name string, typ types.Type) {
	iface, isInterface := typ.Underlying().(*types.Interface)
	for _, pointer := range []bool{false, true} {
		if pointer && isInterface {
			continue
		}
		receiver := typ
		label := name
		if pointer {
			receiver = types.NewPointer(typ)
			label = "*" + label
		}
		out.WriteString(label + "\n")
		if isInterface && !iface.IsMethodSet() {
			out.WriteString("  constraint " + types.TypeString(iface, nil) + "\n")
		}
		if !pointer {
			for _, field := range exportedSurfaceFields(typ) {
				out.WriteString("  field " + field.Name() + " " + types.TypeString(field.Type(), nil) + "\n")
			}
		}
		methods := types.NewMethodSet(receiver)
		for i := 0; i < methods.Len(); i++ {
			m := methods.At(i)
			if isInterface || m.Obj().Exported() {
				out.WriteString("  " + m.Obj().Name() + " " + types.TypeString(m.Type(), func(p *types.Package) string { return p.Path() }) + "\n")
			}
		}
	}
}

func promotedPublicMethods(pkg *types.Package) []string {
	var violations []string
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() {
			continue
		}
		named, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || named.TypeParams().Len() == 0 {
			continue
		}
		for _, receiver := range []types.Type{named, types.NewPointer(named)} {
			methods := types.NewMethodSet(receiver)
			for i := 0; i < methods.Len(); i++ {
				method := methods.At(i)
				if len(method.Index()) > 1 {
					violations = append(violations, name+"."+method.Obj().Name()+" violates R9: declare a direct shell over shared state/helpers")
				}
			}
		}
	}
	return violations
}

func TestPublicGenericMethodsAreDirect(t *testing.T) {
	for _, dir := range []string{".", "./rows", "./expr"} {
		if violations := promotedPublicMethods(surfacePackageIn(t, dir, "")); len(violations) > 0 {
			t.Fatal(strings.Join(violations, "\n"))
		}
	}
	pkg := surfacePackage(t, `package orm; type Promoted[M any] struct { Query[M] }`)
	if len(promotedPublicMethods(pkg)) == 0 {
		t.Fatal("R9 guard accepted promoted generic methods")
	}
}

// Gather candidate names through embedding, then let Go resolve visibility,
// shadowing and ambiguity exactly as it does for consumer selectors.
func exportedSurfaceFields(typ types.Type) []*types.Var {
	names := map[string]bool{}
	seen := map[types.Type]bool{}
	var visit func(types.Type)
	visit = func(t types.Type) {
		t = types.Unalias(t)
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if seen[t] {
			return
		}
		seen[t] = true
		s, ok := t.Underlying().(*types.Struct)
		if !ok {
			return
		}
		for i := 0; i < s.NumFields(); i++ {
			f := s.Field(i)
			if f.Exported() {
				names[f.Name()] = true
			}
			if f.Embedded() {
				visit(f.Type())
			}
		}
	}
	visit(typ)
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var fields []*types.Var
	for _, name := range ordered {
		obj, _, _ := types.LookupFieldOrMethod(typ, false, nil, name)
		if field, ok := obj.(*types.Var); ok && field.IsField() && field.Exported() {
			fields = append(fields, field)
		}
	}
	return fields
}

func TestSurfaceIncludesPromotedFields(t *testing.T) {
	pkg := surfacePackage(t, `package orm
 type surfaceBase struct { Visible int; Shadow int; Ambiguous int; hidden int }
 type surfaceOther struct { Ambiguous string }
 type SurfaceFields struct { surfaceBase; surfaceOther; Shadow string; Own bool }
 `)
	fields := exportedSurfaceFields(pkg.Scope().Lookup("SurfaceFields").Type())
	var got []string
	for _, field := range fields {
		got = append(got, field.Name()+" "+types.TypeString(field.Type(), nil))
	}
	if strings.Join(got, ", ") != "Own bool, Shadow string, Visible int" {
		t.Fatalf("incorrect visible fields: %v", got)
	}
}

// These are the 33 declaration receiver families from the approved D2 surface.
// Inspect value and pointer method sets, including private witnesses: adapters
// must be requested explicitly rather than reached by every declaration.
func isDeclarationColumn(name string) bool {
	switch name {
	case "Column", "keyColumn", "numberColumn", "textColumn", "timeColumn", "Enum", "NullableColumn", "NullableNumber", "NullableText", "nullableTimeColumn", "NullableEnum", "InputColumn", "NullableInputColumn", "InputText", "NullableInputText", "InputNumber", "NullableInputNumber", "InputTime", "NullableInputTime", "InputEnum", "NullableInputEnum", "InputJSON", "NullableInputJSON", "InputInterval", "NullableInputInterval", "JSON", "NullableJSON", "Interval", "NullableInterval", "Encrypted", "NullableEncrypted", "numberKeyColumn", "InputKey":
		return true
	}
	return false
}

func TestDeclarationColumnsAreMethodless(t *testing.T) {
	pkg := surfacePackageIn(t, ".", "")
	count := 0
	for _, name := range pkg.Scope().Names() {
		if !isDeclarationColumn(name) {
			continue
		}
		count++
		typ := pkg.Scope().Lookup(name).Type()
		for _, receiver := range []types.Type{typ, types.NewPointer(typ)} {
			if methods := types.NewMethodSet(receiver); methods.Len() != 0 {
				t.Errorf("%s has %d methods; D2 requires explicit expression adapters", receiver, methods.Len())
			}
		}
	}
	if count != 33 {
		t.Fatalf("declaration family set changed: got %d, want 33", count)
	}
}

func TestExpressionAdaptersKeepLayoutAndSealedCapabilities(t *testing.T) {
	pkg := surfacePackageIn(t, ".", "")
	count := 0
	for _, name := range pkg.Scope().Names() {
		fn, ok := pkg.Scope().Lookup(name).(*types.Func)
		if !ok || !fn.Exported() || !strings.HasSuffix(name, "Expr") {
			continue
		}
		sig := fn.Type().(*types.Signature)
		if sig.Params().Len() != 1 || sig.Results().Len() != 1 {
			t.Fatalf("%s must be a value adapter", name)
		}
		handle := sig.Params().At(0).Type()
		adapter := sig.Results().At(0).Type()
		if !types.Identical(handle.Underlying(), adapter.Underlying()) {
			t.Errorf("%s changed the model/value/role layout", name)
		}
		fields := adapter.Underlying().(*types.Struct)
		for i := 0; i < fields.NumFields(); i++ {
			if fields.Field(i).Embedded() {
				t.Errorf("%s embeds a method-bearing type", name)
			}
		}
		methods := types.NewMethodSet(adapter)
		if methods.Len() == 0 {
			t.Errorf("%s has no sealed capabilities", name)
		}
		for i := 0; i < methods.Len(); i++ {
			m := methods.At(i)
			if m.Obj().Exported() || len(m.Index()) != 1 {
				t.Errorf("%s must declare only private direct witnesses: %s", name, m.Obj().Name())
			}
		}
		count++
	}
	if count != 33 {
		t.Fatalf("adapter family set changed: got %d, want 33", count)
	}
}
