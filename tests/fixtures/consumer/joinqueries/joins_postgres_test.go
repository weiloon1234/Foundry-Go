package joinqueries_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type referralAlias struct{}
type sponsorAlias struct{}

func TestPostgresTypedJoins(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		counter := &queryfixture.QueryCounter{Executor: tx}
		referrals := rows.As[referralAlias](models.Users.Query(), "referral")
		sponsors := rows.As[sponsorAlias](models.Users.Query(), "sponsor")
		r := rows.Scope(referrals)
		s := rows.Scope(sponsors)
		joined := rows.LeftJoin(referrals, sponsors, rows.On(expr.At(r, models.Users.IntroducerID), expr.At(s, models.Users.ID)))
		referralScope := rows.LeftScope(joined, rows.Scope(referrals))
		sponsorScope := rows.NullableRightScope(joined, rows.Scope(sponsors))
		referral := referralScope
		sponsor := sponsorScope
		report := rows.OrderBy(rows.Select(joined, reports.ReferralRows, rows.MapSelected(reports.ReferralRows.ID, expr.Select(expr.At(referral, models.Users.ID))), rows.MapSelected(reports.ReferralRows.Email, expr.Select(expr.At(referral, models.Users.Email))), rows.MapSelected(reports.ReferralRows.IntroducerID, expr.Select(expr.AtNullable(sponsor, models.Users.ID))), rows.MapSelected(reports.ReferralRows.IntroducerEmail, expr.Select(expr.AtNullable(sponsor, models.Users.Email))), rows.MapSelected(reports.ReferralRows.IntroducerNickname, expr.Select(expr.AtNullableValue(sponsor, models.Users.Nickname))), rows.MapSelected(reports.ReferralRows.IntroducerStatus, expr.Select(expr.AtNullable(sponsor, models.Users.Status)))), expr.At(referral, models.Users.Email).Asc())
		items, err := report.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(items) != 4 || counter.Queries.Load() != 1 {
			return errors.New("left self-join did not use one query for all items")
		}
		if !items[0].IntroducerID.IsNull() || !items[0].IntroducerStatus.IsNull() || !items[0].IntroducerNickname.IsNull() {
			t.Error("unmatched self-join fields lost nullability")
		}
		for _, i := range []int{1, 2, 3} {
			id, present := items[i].IntroducerID.Get()
			expected := users[0].ID
			if i == 3 {
				expected = users[1].ID
			}
			if !present || id != expected || !items[i].IntroducerNickname.IsNull() {
				t.Error("matched self-join lost typed IDs or existing nullable field")
			}
		}
		if status, ok := items[3].IntroducerStatus.Get(); !ok || status != models.StatusDisabled {
			t.Error("nullable imported enum lost its codec")
		}
		if n, err := rows.Count(t.Context(), counter, report.Where(expr.AtNullable(sponsor, models.Users.ID).IsNull())); err != nil || n != 1 {
			t.Error("outer nullable predicate failed", err)
		}
		if n, err := rows.Count(t.Context(), counter, report.Where(expr.AtNullable(sponsor, models.Users.Status).Eq(models.StatusActive))); err != nil || n != 2 {
			t.Error("post-join WHERE did not filter matched items", err)
		}
		// Scope filters and windows belong to the right source before joining.
		filtered := rows.As[sponsorAlias](models.Users.Query().Where(models.Users.Status.Eq(models.StatusDisabled)).OrderBy(models.Users.Age.Desc()).Limit(1), "sponsor")
		filteredJoin := rows.LeftJoin(referrals, filtered, rows.On(expr.At(r, models.Users.IntroducerID), expr.At(rows.Scope(filtered), models.Users.ID)))
		fr := rows.LeftScope(filteredJoin, rows.Scope(referrals))
		fs := rows.NullableRightScope(filteredJoin, rows.Scope(filtered))
		filteredReport := rows.Select(filteredJoin, reports.UserPairs, rows.MapSelected(reports.UserPairs.LeftID, expr.Nullable(expr.Select(expr.At(fr, models.Users.ID)))), rows.MapSelected(reports.UserPairs.RightID, expr.Select(expr.AtNullable(fs, models.Users.ID))))
		filteredRows, err := filteredReport.All(t.Context(), counter)
		if err != nil {
			return err
		}
		matches := 0
		for _, row := range filteredRows {
			if !row.RightID.IsNull() {
				matches++
			}
		}
		if len(filteredRows) != 4 || matches != 1 {
			t.Error("filtered right input discarded unmatched left items")
		}
		statement, err := rows.Compile(filteredReport)
		if err != nil {
			return err
		}
		if !strings.Contains(statement.SQL(), "LEFT JOIN (SELECT") || len(statement.Arguments()) != 2 {
			t.Error("source window or parameters were not preserved")
		}
		// An ON filter preserves outer items; the corresponding final WHERE does not.
		onJoin := rows.LeftJoin(referrals, sponsors, rows.On(expr.At(r, models.Users.IntroducerID), expr.At(s, models.Users.ID)).WhereRight(expr.At(s, models.Users.Status).Eq(models.StatusDisabled)))
		onLeft := rows.LeftScope(onJoin, rows.Scope(referrals))
		onRight := rows.NullableRightScope(onJoin, rows.Scope(sponsors))
		onReport := rows.Select(onJoin, reports.UserPairs, rows.MapSelected(reports.UserPairs.LeftID, expr.Nullable(expr.Select(expr.At(onLeft, models.Users.ID)))), rows.MapSelected(reports.UserPairs.RightID, expr.Select(expr.AtNullable(onRight, models.Users.ID))))
		if n, err := rows.Count(t.Context(), counter, onReport); err != nil || n != 4 {
			t.Error("ON scope discarded outer items", err)
		}
		if n, err := rows.Count(t.Context(), counter, onReport.Where(expr.AtNullable(onRight, models.Users.Status).Eq(models.StatusDisabled))); err != nil || n != 1 {
			t.Error("ON and WHERE semantics were conflated", err)
		}
		composite := rows.LeftJoin(referrals, sponsors, rows.OnAnd(rows.On(expr.At(r, models.Users.IntroducerID), expr.At(s, models.Users.ID)), rows.On(expr.At(r, models.Users.Status), expr.At(s, models.Users.Status))))
		cl := rows.LeftScope(composite, rows.Scope(referrals))
		csp := rows.NullableRightScope(composite, rows.Scope(sponsors))
		compositeReport := rows.Select(composite, reports.UserPairs, rows.MapSelected(reports.UserPairs.LeftID, expr.Nullable(expr.Select(expr.At(cl, models.Users.ID)))), rows.MapSelected(reports.UserPairs.RightID, expr.Select(expr.AtNullable(csp, models.Users.ID))))
		if n, err := rows.Count(t.Context(), counter, compositeReport.Where(expr.AtNullable(csp, models.Users.ID).IsNotNull())); err != nil || n != 1 {
			t.Error("composite typed join did not preserve AND semantics", err)
		}
		alternative := rows.InnerJoin(referrals, sponsors, rows.OnOr(rows.On(expr.At(r, models.Users.IntroducerID), expr.At(s, models.Users.ID)), rows.On(expr.At(r, models.Users.ID), expr.At(s, models.Users.ID))))
		al := rows.LeftScope(alternative, rows.Scope(referrals))
		ar := rows.RightScope(alternative, rows.Scope(sponsors))
		alternativeReport := rows.Select(alternative, reports.UserPairs, rows.MapSelected(reports.UserPairs.LeftID, expr.Nullable(expr.Select(expr.At(al, models.Users.ID)))), rows.MapSelected(reports.UserPairs.RightID, expr.Nullable(expr.Select(expr.At(ar, models.Users.ID)))))
		if n, err := rows.Count(t.Context(), counter, alternativeReport); err != nil || n != 7 {
			t.Error("alternative typed join did not preserve OR semantics", err)
		}
		// Failure behavior: no SQL for canceled/invalid queries, and stream release.
		counter.Queries.Store(0)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if items, err := report.All(ctx, counter); !errors.Is(err, context.Canceled) || items != nil || counter.Queries.Load() != 0 {
			t.Error("canceled join performed SQL")
		}
		if items, err := rows.Select(joined, reports.ReferralRows).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || items != nil || counter.Queries.Load() != 0 {
			t.Error("incomplete joined selection executed SQL")
		}
		stop := errors.New("stop joined stream")
		if err := rows.Each(t.Context(), counter, report, func(reports.ReferralRow) error { return stop }); !errors.Is(err, stop) {
			t.Error("joined stream ignored callback failure", err)
		}
		if n, err := rows.Count(t.Context(), counter, report); err != nil || n != 4 {
			t.Error("joined stream left transaction busy", err)
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE id=$1`, users[1].ID.String()); err != nil {
			return err
		}
		if items, err := report.All(t.Context(), counter); err == nil || items != nil {
			t.Error("malformed joined enum published partial results")
		}
		return nil
	})
}
