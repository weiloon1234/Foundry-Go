package scalarqueries_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
			draft := scalarqueries.SampleDraft{}.SetID(i + 1).SetQuantity([]int16{2, 3, -3}[i]).SetAmount(amount).SetScore([]float32{0.5, 0.25, 0.75}[i]).SetName([]scalarqueries.Label{" Élan_20% ", "Beta", ""}[i])
			if i > 0 {
				draft = draft.SetNote([]scalarqueries.Label{"", "Écho", ""}[i])
			}
			if i != 1 {
				tax, err := decimal.Parse([]string{"0.20", "0", "0"}[i])
				if err != nil {
					return err
				}
				draft = draft.SetTax(tax)
			}
			if _, err := scalarqueries.QueryCalculationSamples().Create(t.Context(), tx, draft); err != nil {
				return err
			}
		}
		return check(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func scalarValues[V any](t *testing.T, tx *database.Tx, e query.Expression[scalarqueries.Sample, V], want []V) {
	t.Helper()
	f := scalarqueries.SampleFields()
	got, err := query.SelectValue(scalarqueries.QueryCalculationSamples().OrderBy(f.ID.Asc()), e).All(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("typed scalar result", got, want, err)
	}
}

func TestPostgresNumericCalculations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		f := scalarqueries.SampleFields()
		quantity := f.Quantity
		incremented := query.Add(quantity, quantity.Param(1))
		if rows, err := scalarqueries.QueryCalculationSamples().Where(incremented.Gte(3)).All(t.Context(), tx); err != nil || len(rows) != 2 {
			t.Fatal("typed arithmetic predicate", rows, err)
		}
		scalarValues(t, tx, query.Add(quantity, quantity.Param(2)).Value(), []int16{4, 5, -1})
		scalarValues(t, tx, query.Subtract(quantity, quantity.Param(1)).Value(), []int16{1, 2, -4})
		scalarValues(t, tx, query.Multiply(quantity, quantity).Value(), []int16{4, 9, 9})
		scalarValues(t, tx, query.Divide(quantity, quantity.Param(2)).Value(), []int16{1, 1, -1})
		scalarValues(t, tx, query.Remainder(quantity, quantity.Param(2)).Value(), []int16{0, 1, -1})
		scalarValues(t, tx, query.Negate(quantity).Value(), []int16{-2, -3, 3})
		scalarValues(t, tx, query.Abs(quantity).Value(), []int16{2, 3, 3})
		scalarValues(t, tx, query.Add(f.Score, f.Score.Param(0.25)).Value(), []float32{0.75, 0.5, 1})
		decimalQuantity := query.DecimalOf(quantity)
		half, _ := decimal.Parse("1.5")
		negativeHalf, _ := decimal.Parse("-1.5")
		scalarValues(t, tx, query.Divide(decimalQuantity, decimalQuantity.Param(decimal.FromInt64(2))).Value(), []decimal.Decimal{decimal.FromInt64(1), half, negativeHalf})
		tenth, _ := decimal.Parse("0.1")
		large, _ := decimal.Parse("9007199254740993.2")
		small, _ := decimal.Parse("1.35")
		negative, _ := decimal.Parse("-2.4")
		scalarValues(t, tx, query.Add(f.Amount, f.Amount.Param(tenth)).Value(), []decimal.Decimal{large, small, negative})
		nullable := query.AddNullable(query.NullableRow(f.Amount), f.Tax)
		withTax, _ := decimal.Parse("9007199254740993.3")
		lastAmount, _ := decimal.Parse("-2.5")
		scalarValues(t, tx, nullable.Value(), []value.Nullable[decimal.Decimal]{value.Of(withTax), value.Null[decimal.Decimal](), value.Of(lastAmount)})
		if rows, err := scalarqueries.QueryCalculationSamples().Where(nullable.Gt(decimal.FromInt64(0))).All(t.Context(), tx); err != nil || len(rows) != 1 || rows[0].ID != 1 {
			t.Fatal("nullable numeric predicate", rows, err)
		}
		two := query.NullableRow(quantity.Param(2))
		nq := query.NullableRow(quantity)
		scalarValues(t, tx, query.SubtractNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(0)), value.Of(int16(1)), value.Of(int16(-5))})
		scalarValues(t, tx, query.MultiplyNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(4)), value.Of(int16(6)), value.Of(int16(-6))})
		scalarValues(t, tx, query.DivideNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(1)), value.Of(int16(1)), value.Of(int16(-1))})
		scalarValues(t, tx, query.RemainderNullable(nq, two).Value(), []value.Nullable[int16]{value.Of(int16(0)), value.Of(int16(1)), value.Of(int16(-1))})
		scalarValues(t, tx, query.AbsNullable(query.NegateNullable(nq)).Value(), []value.Nullable[int16]{value.Of(int16(2)), value.Of(int16(3)), value.Of(int16(3))})
		scalarValues(t, tx, query.FloatOf(quantity).Value(), []float64{2, 3, -3})
		scalarValues(t, tx, query.FloatNullable(nq).Value(), []value.Nullable[float64]{value.Of(float64(2)), value.Of(float64(3)), value.Of(float64(-3))})
		scalarValues(t, tx, query.DecimalNullable(nq).Value(), []value.Nullable[decimal.Decimal]{value.Of(decimal.FromInt64(2)), value.Of(decimal.FromInt64(3)), value.Of(decimal.FromInt64(-3))})
		return nil
	})
}

func TestPostgresTextCalculations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		f := scalarqueries.SampleFields()
		trimmed := query.Trim(f.Name)
		scalarValues(t, tx, trimmed.Value(), []string{"Élan_20%", "Beta", ""})
		scalarValues(t, tx, query.Upper(trimmed).Value(), []string{"ÉLAN_20%", "BETA", ""})
		scalarValues(t, tx, query.Lower(query.Upper(trimmed)).Value(), []string{"élan_20%", "beta", ""})
		scalarValues(t, tx, query.Length(trimmed).Value(), []int64{8, 4, 0})
		scalarValues(t, tx, query.OctetLength(trimmed).Value(), []int64{9, 4, 0})
		scalarValues(t, tx, query.TrimLeft(f.Name).Value(), []string{"Élan_20% ", "Beta", ""})
		scalarValues(t, tx, query.TrimRight(f.Name).Value(), []string{" Élan_20%", "Beta", ""})
		scalarValues(t, tx, query.TrimChars(trimmed, trimmed.Param("Éa%")).Value(), []string{"lan_20", "Bet", ""})
		scalarValues(t, tx, query.TrimLeftChars(trimmed, trimmed.Param("Éa%")).Value(), []string{"lan_20%", "Beta", ""})
		scalarValues(t, tx, query.TrimRightChars(trimmed, trimmed.Param("Éa%")).Value(), []string{"Élan_20", "Bet", ""})
		scalarValues(t, tx, query.Concat(trimmed, trimmed.Param("!")).Value(), []string{"Élan_20%!", "Beta!", "!"})
		scalarValues(t, tx, query.Replace(trimmed, trimmed.Param("_20%"), trimmed.Param("done")).Value(), []string{"Élandone", "Beta", ""})
		scalarValues(t, tx, query.Substring(trimmed, 2, 3).Value(), []string{"lan", "eta", ""})
		scalarValues(t, tx, query.SubstringFrom(trimmed, 3).Value(), []string{"an_20%", "ta", ""})
		scalarValues(t, tx, query.SubstringAt(trimmed, f.ID.Param(1), f.ID).Value(), []string{"É", "Be", ""})
		scalarValues(t, tx, query.LowerNullable(f.Note).Value(), []value.Nullable[string]{value.Null[string](), value.Of("écho"), value.Of("")})
		scalarValues(t, tx, query.LengthNullable(f.Note).Value(), []value.Nullable[int64]{value.Null[int64](), value.Of(int64(4)), value.Of(int64(0))})
		scalarValues(t, tx, query.SubstringNullable(f.Note, 2, 2).Value(), []value.Nullable[string]{value.Null[string](), value.Of("ch"), value.Of("")})
		scalarValues(t, tx, query.ConcatNullable(query.NullableRow(f.Name), f.Note).Value(), []value.Nullable[string]{value.Null[string](), value.Of("BetaÉcho"), value.Of("")})
		scalarValues(t, tx, query.ConcatWSNullable("|", query.NullableRow(f.Name), f.Note).Value(), []string{" Élan_20% ", "Beta|Écho", "|"})
		scalarValues(t, tx, query.ConcatWSNullable("|", query.NullFor(f.Name), query.NullFor(f.Name)).Value(), []string{"", "", ""})
		parts := make([]query.RowValue[scalarqueries.Sample, string], 140)
		for i := range parts {
			parts[i] = trimmed.Param("x")
		}
		joined := query.ConcatWS("|", trimmed.Param("x"), parts...)
		want := strings.TrimSuffix(strings.Repeat("x|", 141), "|")
		scalarValues(t, tx, joined.Value(), []string{want, want, want})
		if rows, err := scalarqueries.QueryCalculationSamples().Where(trimmed.Contains("_20%")).All(t.Context(), tx); err != nil || len(rows) != 1 || rows[0].ID != 1 {
			t.Fatal("literal computed text filter", rows, err)
		}
		if rows, err := scalarqueries.QueryCalculationSamples().Where(query.LowerNullable(f.Note).Like("éc%")).All(t.Context(), tx); err != nil || len(rows) != 1 || rows[0].ID != 2 {
			t.Fatal("nullable computed LIKE", rows, err)
		}
		return nil
	})
}

func TestPostgresComputedModelOrderAndGroupedResults(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.QueryCalculationSamples()
		f := scalarqueries.SampleFields()
		magnitude := query.Abs(f.Quantity)
		page, err := q.OrderBy(magnitude.Desc()).Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 2})
		if err != nil || page.Total != 3 || len(page.Items) != 2 || page.Items[0].ID != 2 || page.Items[1].ID != 3 {
			t.Fatal("computed model page order/tie-breaker", page, err)
		}
		var ids []int
		if err := q.OrderBy(magnitude.Desc()).Chunk(t.Context(), tx, 2, func(rows []scalarqueries.Sample) error {
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			return nil
		}); err != nil || !reflect.DeepEqual(ids, []int{2, 3, 1}) {
			t.Fatal("computed chunk order", ids, err)
		}
		if rows, err := q.OrderBy(magnitude.Desc(), f.ID.Asc()).ForUpdate().All(t.Context(), tx); err != nil || len(rows) != 3 || rows[0].ID != 2 {
			t.Fatal("computed locked model order", rows, err)
		}
		count := query.Count[scalarqueries.Sample]()
		doubled := query.MultiplyValue(count.Value(), count.Param(2).Value())
		if got, err := query.SelectValue(q, doubled).GroupBy(magnitude.Group()).Having(query.OrderValue(doubled).Gt(2)).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{4}) {
			t.Fatal("arithmetic grouping/HAVING", got, err)
		}
		window := query.WindowFor(q).OrderBy(f.ID.Asc()).Named("calculation_rows")
		shifted := query.AddValue(query.RowNumber(window), count.Param(10).Value())
		scalarValues(t, tx, shifted, []int64{11, 12, 13})
		if got, err := q.DistinctOnValues(magnitude.Value().Key()).OrderBy(magnitude.Asc(), f.ID.Asc()).All(t.Context(), tx); err != nil || len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
			t.Fatal("computed distinct winners", got, err)
		}
		return nil
	})
}

func TestPostgresScalarFailureBoundaries(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.QueryCalculationSamples()
		f := scalarqueries.SampleFields()
		for _, test := range []struct {
			name, state string
			expr        query.Expression[scalarqueries.Sample, int]
		}{
			{"division zero", "22012", query.Divide(f.ID, f.ID.Param(0)).Value()},
			{"bigint overflow", "22003", query.Add(f.ID.Param(math.MaxInt64), f.ID.Param(1)).Value()},
		} {
			if _, err := query.SelectValue(q, test.expr).Compile(); err != nil {
				t.Fatal("runtime case failed compilation", test.name, err)
			}
			err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
				_, err := query.SelectValue(q, test.expr).All(t.Context(), inner)
				return err
			})
			var databaseError *database.Error
			if !errors.As(err, &databaseError) || databaseError.SQLState() != test.state {
				t.Fatal(test.name, err)
			}
		}
		tooSmall := query.Add(f.Quantity.Param(math.MaxInt16), f.Quantity.Param(1))
		if rows, err := query.SelectValue(q, tooSmall.Value()).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("narrow decode published partial results", rows, err)
		}
		return nil
	})
}

func TestScalarFailuresBeforeExecution(t *testing.T) {
	q := scalarqueries.QueryCalculationSamples()
	f := scalarqueries.SampleFields()
	if _, err := query.SelectValue(q.Limit(0), query.Substring(f.Name, 1, -1).Value()).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := q.OrderBy(query.Abs(f.Quantity).Asc()).CursorPaginate(t.Context(), queryfixture.NoQueries(t), query.CursorRequest[scalarqueries.Sample]{Size: 2}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestPostgresComputedRelationOrder(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		f := models.UserFields()
		relation := models.UserRelations().Referrals.OrderBy(query.Negate(f.Age).Asc())
		rows, err := models.QueryUsers().Where(f.ID.Eq(users[0].ID)).With(relation).All(t.Context(), tx)
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		children, _ := rows[0].Referrals.Get()
		if len(children) != 2 || children[0].ID != users[2].ID {
			t.Fatal("computed eager relation order", children)
		}
		return nil
	})
}
