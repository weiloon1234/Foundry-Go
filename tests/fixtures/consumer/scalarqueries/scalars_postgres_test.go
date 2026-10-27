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
			draft := []orm.Assignment[scalarqueries.Sample]{f.ID.Set(i + 1), f.Quantity.Set([]int16{2, 3, -3}[i]), f.Amount.Set(amount), f.Score.Set([]float32{0.5, 0.25, 0.75}[i]), f.Name.Set([]scalarqueries.Label{" Élan_20% ", "Beta", ""}[i])}
			if i > 0 {
				draft = append(draft, f.Note.Set([]scalarqueries.Label{"", "Écho", ""}[i]))
			}
			if i != 1 {
				tax, err := decimal.Parse([]string{"0.20", "0", "0"}[i])
				if err != nil {
					return err
				}
				draft = append(draft, f.Tax.Set(tax))
			}
			if _, err := scalarqueries.Samples.Insert(t.Context(), tx, draft...); err != nil {
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
	got, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(expr.OrderBy(scalarqueries.Samples.Query(), expr.Value(f.ID).Asc())), e)).All(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("typed scalar result", got, want, err)
	}
}

func TestPostgresNumericCalculations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		f := scalarqueries.Samples
		quantity := expr.Value(f.Quantity)
		incremented := expr.Add(quantity, expr.Param(f.Quantity, 1))
		if rows, err := expr.Where(scalarqueries.Samples.Query(), incremented.Gte(3)).All(t.Context(), tx); err != nil || len(rows) != 2 {
			t.Fatal("typed arithmetic predicate", rows, err)
		}
		scalarValues(t, tx, expr.Add(quantity, expr.Param(f.Quantity, 2)).Value(), []int16{4, 5, -1})
		scalarValues(t, tx, expr.Subtract(quantity, expr.Param(f.Quantity, 1)).Value(), []int16{1, 2, -4})
		scalarValues(t, tx, expr.Multiply(quantity, quantity).Value(), []int16{4, 9, 9})
		scalarValues(t, tx, expr.Divide(quantity, expr.Param(f.Quantity, 2)).Value(), []int16{1, 1, -1})
		scalarValues(t, tx, expr.Remainder(quantity, expr.Param(f.Quantity, 2)).Value(), []int16{0, 1, -1})
		scalarValues(t, tx, expr.Negate(quantity).Value(), []int16{-2, -3, 3})
		scalarValues(t, tx, expr.Abs(quantity).Value(), []int16{2, 3, 3})
		scalarValues(t, tx, expr.Add(expr.Value(f.Score), expr.Param(f.Score, 0.25)).Value(), []float32{0.75, 0.5, 1})
		decimalQuantity := expr.DecimalOf(quantity)
		half, _ := decimal.Parse("1.5")
		negativeHalf, _ := decimal.Parse("-1.5")
		scalarValues(t, tx, expr.Divide(decimalQuantity, decimalQuantity.Param(decimal.FromInt64(2))).Value(), []decimal.Decimal{decimal.FromInt64(1), half, negativeHalf})
		tenth, _ := decimal.Parse("0.1")
		large, _ := decimal.Parse("9007199254740993.2")
		small, _ := decimal.Parse("1.35")
		negative, _ := decimal.Parse("-2.4")
		scalarValues(t, tx, expr.Add(expr.Value(f.Amount), expr.Param(f.Amount, tenth)).Value(), []decimal.Decimal{large, small, negative})
		nullable := expr.AddNullable(expr.NullableRow(expr.Value(f.Amount)), expr.Value(f.Tax))
		withTax, _ := decimal.Parse("9007199254740993.3")
		lastAmount, _ := decimal.Parse("-2.5")
		scalarValues(t, tx, nullable.Value(), []value.Nullable[decimal.Decimal]{value.Of(withTax), value.Null[decimal.Decimal](), value.Of(lastAmount)})
		if rows, err := expr.Where(scalarqueries.Samples.Query(), nullable.Gt(decimal.FromInt64(0))).All(t.Context(), tx); err != nil || len(rows) != 1 || rows[0].ID != 1 {
			t.Fatal("nullable numeric predicate", rows, err)
		}
		two := expr.NullableRow(expr.Param(f.Quantity, 2))
		nq := expr.NullableRow(quantity)
		scalarValues(t, tx, expr.SubtractNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(0)), value.Of(int16(1)), value.Of(int16(-5))})
		scalarValues(t, tx, expr.MultiplyNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(4)), value.Of(int16(6)), value.Of(int16(-6))})
		scalarValues(t, tx, expr.DivideNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(1)), value.Of(int16(1)), value.Of(int16(-1))})
		scalarValues(t, tx, expr.RemainderNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(0)), value.Of(int16(1)), value.Of(int16(-1))})
		scalarValues(t, tx, expr.AbsNullable(expr.NegateNullable(nq)).Value(), []value.Nullable[int16]{value.Of(int16(2)), value.Of(int16(3)), value.Of(int16(3))})
		scalarValues(t, tx, expr.FloatOf(quantity).Value(), []float64{2, 3, -3})
		scalarValues(t, tx, expr.FloatNullable(nq).Value(), []value.Nullable[float64]{value.Of(float64(2)), value.Of(float64(3)), value.Of(float64(-3))})
		scalarValues(t, tx, expr.DecimalNullable(nq).Value(), []value.Nullable[decimal.Decimal]{value.Of(decimal.FromInt64(2)), value.Of(decimal.FromInt64(3)), value.Of(decimal.FromInt64(-3))})
		return nil
	})
}

func TestPostgresTextCalculations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		f := scalarqueries.Samples
		trimmed := expr.Trim(expr.Value(f.Name))
		scalarValues(t, tx, trimmed.Value(), []string{"Élan_20%", "Beta", ""})
		scalarValues(t, tx, expr.Upper(trimmed).Value(), []string{"ÉLAN_20%", "BETA", ""})
		scalarValues(t, tx, expr.Lower(expr.Upper(trimmed)).Value(), []string{"élan_20%", "beta", ""})
		scalarValues(t, tx, expr.Length(trimmed).Value(), []int64{8, 4, 0})
		scalarValues(t, tx, expr.OctetLength(trimmed).Value(), []int64{9, 4, 0})
		scalarValues(t, tx, expr.TrimLeft(expr.Value(f.Name)).Value(), []string{"Élan_20% ", "Beta", ""})
		scalarValues(t, tx, expr.TrimRight(expr.Value(f.Name)).Value(), []string{" Élan_20%", "Beta", ""})
		scalarValues(t, tx, expr.TrimChars(trimmed, trimmed.Param("Éa%")).Value(), []string{"lan_20", "Bet", ""})
		scalarValues(t, tx, expr.TrimLeftChars(trimmed, trimmed.Param("Éa%")).Value(), []string{"lan_20%", "Beta", ""})
		scalarValues(t, tx, expr.TrimRightChars(trimmed, trimmed.Param("Éa%")).Value(), []string{"Élan_20", "Bet", ""})
		scalarValues(t, tx, expr.Concat(trimmed, trimmed.Param("!")).Value(), []string{"Élan_20%!", "Beta!", "!"})
		scalarValues(t, tx, expr.Replace(trimmed, trimmed.Param("_20%"), trimmed.Param("done")).Value(), []string{"Élandone", "Beta", ""})
		scalarValues(t, tx, expr.Substring(trimmed, 2, 3).Value(), []string{"lan", "eta", ""})
		scalarValues(t, tx, expr.SubstringFrom(trimmed, 3).Value(), []string{"an_20%", "ta", ""})
		scalarValues(t, tx, expr.SubstringAt(trimmed, expr.Param(f.ID, 1), expr.Value(f.ID)).Value(), []string{"É", "Be", ""})
		scalarValues(t, tx, expr.LowerNullable(expr.Value(f.Note)).Value(), []value.Nullable[string]{value.Null[string](), value.Of("écho"), value.Of("")})
		scalarValues(t, tx, expr.LengthNullable(expr.Value(f.Note)).Value(), []value.Nullable[int64]{value.Null[int64](), value.Of(int64(4)), value.Of(int64(0))})
		scalarValues(t, tx, expr.SubstringNullable(expr.Value(f.Note), 2, 2).Value(), []value.Nullable[string]{value.Null[string](), value.Of("ch"), value.Of("")})
		scalarValues(t, tx, expr.ConcatNullable(expr.NullableRow(expr.Value(f.Name)), expr.Value(f.Note)).Value(), []value.Nullable[string]{value.Null[string](), value.Of("BetaÉcho"), value.Of("")})
		scalarValues(t, tx, expr.ConcatWSNullable("|", expr.NullableRow(expr.Value(f.Name)), expr.Value(f.Note)).Value(), []string{" Élan_20% ", "Beta|Écho", "|"})
		scalarValues(t, tx, expr.ConcatWSNullable("|", expr.NullableRow(expr.Value(f.Name)).Param(value.Null[scalarqueries.Label]()), expr.NullableRow(expr.Value(f.Name)).Param(value.Null[scalarqueries.Label]())).Value(), []string{"", "", ""})
		parts := make([]expr.RowValue[scalarqueries.Sample, string], 140)
		for i := range parts {
			parts[i] = trimmed.Param("x")
		}
		joined := expr.ConcatWS("|", trimmed.Param("x"), parts...)
		want := strings.TrimSuffix(strings.Repeat("x|", 141), "|")
		scalarValues(t, tx, joined.Value(), []string{want, want, want})
		if rows, err := expr.Where(scalarqueries.Samples.Query(), trimmed.Contains("_20%")).All(t.Context(), tx); err != nil || len(rows) != 1 || rows[0].ID != 1 {
			t.Fatal("literal computed text filter", rows, err)
		}
		if rows, err := expr.Where(scalarqueries.Samples.Query(), expr.LowerNullable(expr.Value(f.Note)).Like("éc%")).All(t.Context(), tx); err != nil || len(rows) != 1 || rows[0].ID != 2 {
			t.Fatal("nullable computed LIKE", rows, err)
		}
		return nil
	})
}

func TestPostgresComputedModelOrderAndGroupedResults(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.Samples.Query()
		f := scalarqueries.Samples
		magnitude := expr.Abs(expr.Value(f.Quantity))
		page, err := expr.OrderBy(q, magnitude.Desc()).Paginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 2})
		if err != nil || page.Total != 3 || len(page.Items) != 2 || page.Items[0].ID != 2 || page.Items[1].ID != 3 {
			t.Fatal("computed model page order/tie-breaker", page, err)
		}
		var ids []int
		if err := expr.OrderBy(q, magnitude.Desc()).Chunk(t.Context(), tx, 2, func(rows []scalarqueries.Sample) error {
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			return nil
		}); err != nil || !reflect.DeepEqual(ids, []int{2, 3, 1}) {
			t.Fatal("computed chunk order", ids, err)
		}
		if rows, err := expr.OrderBy(q, magnitude.Desc(), expr.Value(f.ID).Asc()).ForUpdate().All(t.Context(), tx); err != nil || len(rows) != 3 || rows[0].ID != 2 {
			t.Fatal("computed locked model order", rows, err)
		}
		count := expr.Count[scalarqueries.Sample]()
		doubled := expr.MultiplyValue(count.Value(), count.Param(2).Value())
		if got, err := rowset.ReadScalar(rowset.HavingScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), doubled), expr.Group(magnitude)), expr.OrderValue(doubled).Gt(2))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{4}) {
			t.Fatal("arithmetic grouping/HAVING", got, err)
		}
		window := expr.WindowFor(rowset.ModelScope(q)).OrderBy(expr.Value(f.ID).Asc()).Named("calculation_rows")
		shifted := expr.AddValue(expr.RowNumber(window), count.Param(10).Value())
		scalarValues(t, tx, shifted, []int64{11, 12, 13})
		if got, err := rowset.Read(rowset.OrderBy(rowset.DistinctOnValues(rowset.FromModel(q), magnitude.Value().Key()), magnitude.Asc(), expr.Value(f.ID).Asc())).All(t.Context(), tx); err != nil || len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
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
			{"division zero", "22012", expr.Divide(expr.Value(f.ID), expr.Param(f.ID, 0)).Value()},
			{"bigint overflow", "22003", expr.Add(expr.Param(f.ID, math.MaxInt64), expr.Param(f.ID, 1)).Value()},
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
		tooSmall := expr.Add(expr.Param(f.Quantity, math.MaxInt16), expr.Param(f.Quantity, 1))
		if rows, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q), tooSmall.Value())).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("narrow decode published partial results", rows, err)
		}
		return nil
	})
}

func TestScalarFailuresBeforeExecution(t *testing.T) {
	q := scalarqueries.Samples.Query()
	f := scalarqueries.Samples
	if _, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q.Limit(0)), expr.Substring(expr.Value(f.Name), 1, -1).Value())).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := orm.CursorPaginate(t.Context(), queryfixture.NoQueries(t), expr.OrderBy(q, expr.Abs(expr.Value(f.Quantity)).Asc()), orm.CursorRequest[scalarqueries.Sample]{Size: 2}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
