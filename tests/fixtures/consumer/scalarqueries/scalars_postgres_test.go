package scalarqueries_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func runCalculations(t *testing.T, check func(*database.Tx) error) {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{`SET LOCAL search_path TO "` + namespace + `"`, `CREATE TABLE calculation_samples (id bigint PRIMARY KEY,quantity smallint NOT NULL,amount numeric NOT NULL,score real NOT NULL,name text NOT NULL,note text,tax numeric)`} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		for i, text := range []string{"9007199254740993.10", "1.25", "-2.5"} {
			amount, err := decimal.Parse(text)
			if err != nil {
				return err
			}
			f := scalarqueries.Samples
			draft := []orm.Assignment[scalarqueries.Sample]{orm.NumberKeySet(f.ID, i+1), orm.NumberSet(f.Quantity, []int16{2, 3, -3}[i]), orm.NumberSet(f.Amount, amount), orm.NumberSet(f.Score, []float32{0.5, 0.25, 0.75}[i]), orm.TextSet(f.Name, []scalarqueries.Label{" Élan_20% ", "Beta", ""}[i])}
			if i > 0 {
				draft = append(draft, orm.NullableTextSet(f.Note, []scalarqueries.Label{"", "Écho", ""}[i]))
			}
			if i != 1 {
				tax, err := decimal.Parse([]string{"0.20", "0", "0"}[i])
				if err != nil {
					return err
				}
				draft = append(draft, orm.NullableNumberSet(f.Tax, tax))
			}
			if _, err := orm.Insert(t.Context(), tx, scalarqueries.Samples.Table, draft...); err != nil {
				return err
			}
		}
		return check(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func scalarValues[V any](t *testing.T, tx *database.Tx, e expr.Selected[scalarqueries.Sample, V], want []V) {
	t.Helper()
	f := scalarqueries.Samples
	got, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(expr.OrderBy(scalarqueries.Samples.Query(), expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID))))), e)).All(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("typed scalar result", got, want, err)
	}
}

func TestPostgresNumericCalculations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		f := scalarqueries.Samples
		quantity := expr.Value(orm.NumberExpr(f.Quantity))
		incremented := expr.Add(quantity, expr.Param(orm.NumberExpr(f.Quantity), 1))
		if rows, err := orm.All(t.Context(), tx, expr.Where(scalarqueries.Samples.Query(), expr.Gte(incremented, 3))); err != nil || len(rows) != 2 {
			t.Fatal("typed arithmetic predicate", rows, err)
		}
		scalarValues(t, tx, expr.AsSelected(expr.Add(quantity, expr.Param(orm.NumberExpr(f.Quantity), 2))), []int16{4, 5, -1})
		scalarValues(t, tx, expr.AsSelected(expr.Subtract(quantity, expr.Param(orm.NumberExpr(f.Quantity), 1))), []int16{1, 2, -4})
		scalarValues(t, tx, expr.AsSelected(expr.Multiply(quantity, quantity)), []int16{4, 9, 9})
		scalarValues(t, tx, expr.AsSelected(expr.Divide(quantity, expr.Param(orm.NumberExpr(f.Quantity), 2))), []int16{1, 1, -1})
		scalarValues(t, tx, expr.AsSelected(expr.Remainder(quantity, expr.Param(orm.NumberExpr(f.Quantity), 2))), []int16{0, 1, -1})
		scalarValues(t, tx, expr.AsSelected(expr.Negate(quantity)), []int16{-2, -3, 3})
		scalarValues(t, tx, expr.AsSelected(expr.Abs(quantity)), []int16{2, 3, 3})
		scalarValues(t, tx, expr.AsSelected(expr.Add(expr.Value(orm.NumberExpr(f.Score)), expr.Param(orm.NumberExpr(f.Score), 0.25))), []float32{0.75, 0.5, 1})
		decimalQuantity := expr.DecimalOf(quantity)
		half, _ := decimal.Parse("1.5")
		negativeHalf, _ := decimal.Parse("-1.5")
		scalarValues(t, tx, expr.AsSelected(expr.Divide(decimalQuantity, expr.ParamOf(decimalQuantity, decimal.FromInt64(2)))), []decimal.Decimal{decimal.FromInt64(1), half, negativeHalf})
		tenth, _ := decimal.Parse("0.1")
		large, _ := decimal.Parse("9007199254740993.2")
		small, _ := decimal.Parse("1.35")
		negative, _ := decimal.Parse("-2.4")
		scalarValues(t, tx, expr.AsSelected(expr.Add(expr.Value(orm.NumberExpr(f.Amount)), expr.Param(orm.NumberExpr(f.Amount), tenth))), []decimal.Decimal{large, small, negative})
		nullable := expr.AddNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Amount))), expr.Value(orm.NullableNumberExpr(f.Tax)))
		withTax, _ := decimal.Parse("9007199254740993.3")
		lastAmount, _ := decimal.Parse("-2.5")
		scalarValues(t, tx, expr.AsSelected(nullable), []value.Nullable[decimal.Decimal]{value.Of(withTax), value.Null[decimal.Decimal](), value.Of(lastAmount)})
		if rows, err := orm.All(t.Context(), tx, expr.Where(scalarqueries.Samples.Query(), expr.NullableGt(nullable, decimal.FromInt64(0)))); err != nil || len(rows) != 1 || rows[0].ID != 1 {
			t.Fatal("nullable numeric predicate", rows, err)
		}
		two := expr.NullableRow(expr.Param(orm.NumberExpr(f.Quantity), 2))
		nq := expr.NullableRow(quantity)
		scalarValues(t, tx, expr.AsSelected(expr.SubtractNullable(nq, two)), []value.Nullable[int16]{value.Of(int16(0)), value.Of(int16(1)), value.Of(int16(-5))})
		scalarValues(t, tx, expr.AsSelected(expr.MultiplyNullable(nq, two)), []value.Nullable[int16]{value.Of(int16(4)), value.Of(int16(6)), value.Of(int16(-6))})
		scalarValues(t, tx, expr.AsSelected(expr.DivideNullable(nq, two)), []value.Nullable[int16]{value.Of(int16(1)), value.Of(int16(1)), value.Of(int16(-1))})
		scalarValues(t, tx, expr.AsSelected(expr.RemainderNullable(nq, two)), []value.Nullable[int16]{value.Of(int16(0)), value.Of(int16(1)), value.Of(int16(-1))})
		scalarValues(t, tx, expr.AsSelected(expr.AbsNullable(expr.NegateNullable(nq))), []value.Nullable[int16]{value.Of(int16(2)), value.Of(int16(3)), value.Of(int16(3))})
		scalarValues(t, tx, expr.AsSelected(expr.FloatOf(quantity)), []float64{2, 3, -3})
		scalarValues(t, tx, expr.AsSelected(expr.FloatNullable(nq)), []value.Nullable[float64]{value.Of(float64(2)), value.Of(float64(3)), value.Of(float64(-3))})
		scalarValues(t, tx, expr.AsSelected(expr.DecimalNullable(nq)), []value.Nullable[decimal.Decimal]{value.Of(decimal.FromInt64(2)), value.Of(decimal.FromInt64(3)), value.Of(decimal.FromInt64(-3))})
		return nil
	})
}

func TestPostgresTextCalculations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		f := scalarqueries.Samples
		trimmed := expr.Trim(expr.Value(orm.TextExpr(f.Name)))
		scalarValues(t, tx, expr.AsSelected(trimmed), []string{"Élan_20%", "Beta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.Upper(trimmed)), []string{"ÉLAN_20%", "BETA", ""})
		scalarValues(t, tx, expr.AsSelected(expr.Lower(expr.Upper(trimmed))), []string{"élan_20%", "beta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.Length(trimmed)), []int64{8, 4, 0})
		scalarValues(t, tx, expr.AsSelected(expr.OctetLength(trimmed)), []int64{9, 4, 0})
		scalarValues(t, tx, expr.AsSelected(expr.TrimLeft(expr.Value(orm.TextExpr(f.Name)))), []string{"Élan_20% ", "Beta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.TrimRight(expr.Value(orm.TextExpr(f.Name)))), []string{" Élan_20%", "Beta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.TrimChars(trimmed, expr.ParamOf(trimmed, "Éa%"))), []string{"lan_20", "Bet", ""})
		scalarValues(t, tx, expr.AsSelected(expr.TrimLeftChars(trimmed, expr.ParamOf(trimmed, "Éa%"))), []string{"lan_20%", "Beta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.TrimRightChars(trimmed, expr.ParamOf(trimmed, "Éa%"))), []string{"Élan_20", "Bet", ""})
		scalarValues(t, tx, expr.AsSelected(expr.Concat(trimmed, expr.ParamOf(trimmed, "!"))), []string{"Élan_20%!", "Beta!", "!"})
		scalarValues(t, tx, expr.AsSelected(expr.Replace(trimmed, expr.ParamOf(trimmed, "_20%"), expr.ParamOf(trimmed, "done"))), []string{"Élandone", "Beta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.Substring(trimmed, 2, 3)), []string{"lan", "eta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.SubstringFrom(trimmed, 3)), []string{"an_20%", "ta", ""})
		scalarValues(t, tx, expr.AsSelected(expr.SubstringAt(trimmed, expr.Param(orm.NumberKeyExpr(f.ID), 1), expr.Value(orm.NumberKeyExpr(f.ID)))), []string{"É", "Be", ""})
		scalarValues(t, tx, expr.AsSelected(expr.LowerNullable(expr.Value(orm.NullableTextExpr(f.Note)))), []value.Nullable[string]{value.Null[string](), value.Of("écho"), value.Of("")})
		scalarValues(t, tx, expr.AsSelected(expr.LengthNullable(expr.Value(orm.NullableTextExpr(f.Note)))), []value.Nullable[int64]{value.Null[int64](), value.Of(int64(4)), value.Of(int64(0))})
		scalarValues(t, tx, expr.AsSelected(expr.SubstringNullable(expr.Value(orm.NullableTextExpr(f.Note)), 2, 2)), []value.Nullable[string]{value.Null[string](), value.Of("ch"), value.Of("")})
		scalarValues(t, tx, expr.AsSelected(expr.ConcatNullable(expr.NullableRow(expr.Value(orm.TextExpr(f.Name))), expr.Value(orm.NullableTextExpr(f.Note)))), []value.Nullable[string]{value.Null[string](), value.Of("BetaÉcho"), value.Of("")})
		scalarValues(t, tx, expr.AsSelected(expr.ConcatWSNullable("|", expr.NullableRow(expr.Value(orm.TextExpr(f.Name))), expr.Value(orm.NullableTextExpr(f.Note)))), []string{" Élan_20% ", "Beta|Écho", "|"})
		scalarValues(t, tx, expr.AsSelected(expr.ConcatWSNullable("|", expr.ParamOf(expr.NullableRow(expr.Value(orm.TextExpr(f.Name))), value.Null[scalarqueries.Label]()), expr.ParamOf(expr.NullableRow(expr.Value(orm.TextExpr(f.Name))), value.Null[scalarqueries.Label]()))), []string{"", "", ""})
		parts := make([]expr.RowValue[scalarqueries.Sample, string], 140)
		for i := range parts {
			parts[i] = expr.ParamOf(trimmed, "x")
		}
		joined := expr.ConcatWS("|", expr.ParamOf(trimmed, "x"), parts...)
		want := strings.TrimSuffix(strings.Repeat("x|", 141), "|")
		scalarValues(t, tx, expr.AsSelected(joined), []string{want, want, want})
		if rows, err := orm.All(t.Context(), tx, expr.Where(scalarqueries.Samples.Query(), expr.TextContains(trimmed, "_20%"))); err != nil || len(rows) != 1 || rows[0].ID != 1 {
			t.Fatal("literal computed text filter", rows, err)
		}
		if rows, err := orm.All(t.Context(), tx, expr.Where(scalarqueries.Samples.Query(), expr.NullableTextLike(expr.LowerNullable(expr.Value(orm.NullableTextExpr(f.Note))), "éc%"))); err != nil || len(rows) != 1 || rows[0].ID != 2 {
			t.Fatal("nullable computed LIKE", rows, err)
		}
		return nil
	})
}

func TestPostgresComputedModelOrderAndGroupedResults(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.Samples.Query()
		f := scalarqueries.Samples
		magnitude := expr.Abs(expr.Value(orm.NumberExpr(f.Quantity)))
		page, err := orm.Paginate(t.Context(), tx, expr.OrderBy(q, expr.Desc(magnitude)), orm.PageRequest{Number: 1, Size: 2})
		if err != nil || page.Total != 3 || len(page.Items) != 2 || page.Items[0].ID != 2 || page.Items[1].ID != 3 {
			t.Fatal("computed model page order/tie-breaker", page, err)
		}
		var ids []int
		if err := orm.Chunk(t.Context(), tx, expr.OrderBy(q, expr.Desc(magnitude)), 2, func(rows []scalarqueries.Sample) error {
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			return nil
		}); err != nil || !reflect.DeepEqual(ids, []int{2, 3, 1}) {
			t.Fatal("computed chunk order", ids, err)
		}
		if rows, err := orm.AllLocked(t.Context(), tx, orm.Lock(expr.OrderBy(q, expr.Desc(magnitude), expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID)))))); err != nil || len(rows) != 3 || rows[0].ID != 2 {
			t.Fatal("computed locked model order", rows, err)
		}
		count := expr.Count[scalarqueries.Sample]()
		doubled := expr.MultiplyValue(expr.AsSelected(count), expr.AsSelected(expr.ParamOf(count, 2)))
		if got, err := rowset.ReadScalar(rowset.HavingScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), doubled), expr.Group(magnitude)), expr.GtValue(expr.OrderValue(doubled), 2))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{4}) {
			t.Fatal("arithmetic grouping/HAVING", got, err)
		}
		window := expr.WindowFor(rowset.ModelScope(q)).OrderBy(expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID)))).Named("calculation_rows")
		shifted := expr.AddValue(expr.RowNumber(window), expr.AsSelected(expr.ParamOf(count, 10)))
		scalarValues(t, tx, shifted, []int64{11, 12, 13})
		if got, err := rowset.Read(rowset.OrderBy(rowset.DistinctOnValues(rowset.FromModel(q), expr.Key(expr.AsSelected(magnitude))), expr.Asc(magnitude), expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID))))).All(t.Context(), tx); err != nil || len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
			t.Fatal("computed distinct winners", got, err)
		}
		return nil
	})
}

func TestPostgresScalarFailureBoundaries(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.Samples.Query()
		f := scalarqueries.Samples
		for _, test := range []struct {
			name, state string
			expr        expr.Selected[scalarqueries.Sample, int]
		}{
			{"division zero", "22012", expr.AsSelected(expr.Divide(expr.Value(orm.NumberKeyExpr(f.ID)), expr.Param(orm.NumberKeyExpr(f.ID), 0)))},
			{"bigint overflow", "22003", expr.AsSelected(expr.Add(expr.Param(orm.NumberKeyExpr(f.ID), math.MaxInt64), expr.Param(orm.NumberKeyExpr(f.ID), 1)))},
		} {
			if _, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.From(q), test.expr))); err != nil {
				t.Fatal("runtime case failed compilation", test.name, err)
			}
			err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
				_, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q), test.expr)).All(t.Context(), inner)
				return err
			})
			var databaseError *database.Error
			if !errors.As(err, &databaseError) || databaseError.SQLState() != test.state {
				t.Fatal(test.name, err)
			}
		}
		tooSmall := expr.Add(expr.Param(orm.NumberExpr(f.Quantity), math.MaxInt16), expr.Param(orm.NumberExpr(f.Quantity), 1))
		if rows, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q), expr.AsSelected(tooSmall))).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("narrow decode published partial results", rows, err)
		}
		return nil
	})
}

func TestScalarFailuresBeforeExecution(t *testing.T) {
	q := scalarqueries.Samples.Query()
	f := scalarqueries.Samples
	if _, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q.Limit(0)), expr.AsSelected(expr.Substring(expr.Value(orm.TextExpr(f.Name)), 1, -1)))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := orm.CursorPaginate(t.Context(), queryfixture.NoQueries(t), expr.OrderBy(q, expr.Asc(expr.Abs(expr.Value(orm.NumberExpr(f.Quantity))))), orm.CursorRequest[scalarqueries.Sample]{Size: 2}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
