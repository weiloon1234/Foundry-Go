package joinqueries_test

import (
	"context"
	"errors"
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

type referralAlias struct{}
type sponsorAlias struct{}

func TestPostgresTypedJoins(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		counter := &queryfixture.QueryCounter{Executor: tx}
		referrals := query.As[referralAlias](models.QueryUsers(), "referral")
		sponsors := query.As[sponsorAlias](models.QueryUsers(), "sponsor")
		r := models.UserFieldsAt(referrals.Scope())
		s := models.UserFieldsAt(sponsors.Scope())
		joined := query.LeftJoin(referrals, sponsors, query.On(r.IntroducerID, s.ID))
		referralScope := query.LeftScope(joined, referrals.Scope())
		sponsorScope := query.NullableRightScope(joined, sponsors.Scope())
		referral := models.UserFieldsAt(referralScope)
		sponsor := models.UserNullableFieldsAt(sponsorScope)
		selection := reports.ProjectReferralRow(joined)
		report := selection.SelectID(referral.ID.Value()).SelectEmail(referral.Email.Value()).
			SelectIntroducerID(sponsor.ID.Value()).SelectIntroducerEmail(sponsor.Email.Value()).
			SelectIntroducerNickname(sponsor.Nickname.Value()).SelectIntroducerStatus(sponsor.Status.Value()).
			Query().OrderBy(referral.Email.Asc())
		rows, err := report.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(rows) != 4 || counter.Queries.Load() != 1 {
			return errors.New("left self-join did not use one query for all rows")
		}
		if !rows[0].IntroducerID.IsNull() || !rows[0].IntroducerStatus.IsNull() || !rows[0].IntroducerNickname.IsNull() {
			t.Error("unmatched self-join fields lost nullability")
		}
		for _, i := range []int{1, 2, 3} {
			id, present := rows[i].IntroducerID.Get()
			expected := users[0].ID
			if i == 3 {
				expected = users[1].ID
			}
			if !present || id != expected || !rows[i].IntroducerNickname.IsNull() {
				t.Error("matched self-join lost typed IDs or existing nullable field")
			}
		}
		if status, ok := rows[3].IntroducerStatus.Get(); !ok || status != models.StatusDisabled {
			t.Error("nullable imported enum lost its codec")
		}
		if n, err := report.Where(sponsor.ID.IsNull()).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("outer nullable predicate failed", err)
		}
		if n, err := report.Where(sponsor.Status.Eq(models.StatusActive)).Count(t.Context(), counter); err != nil || n != 2 {
			t.Error("post-join WHERE did not filter matched rows", err)
		}
		// Scope filters and windows belong to the right source before joining.
		filtered := query.As[sponsorAlias](models.QueryUsers().Where(models.UserFields().Status.Eq(models.StatusDisabled)).OrderBy(models.UserFields().Age.Desc()).Limit(1), "sponsor")
		filteredJoin := query.LeftJoin(referrals, filtered, query.On(r.IntroducerID, models.UserFieldsAt(filtered.Scope()).ID))
		fr := models.UserFieldsAt(query.LeftScope(filteredJoin, referrals.Scope()))
		fs := models.UserNullableFieldsAt(query.NullableRightScope(filteredJoin, filtered.Scope()))
		filteredReport := reports.ProjectUserPair(filteredJoin).SelectLeftID(query.Nullable(fr.ID.Value())).SelectRightID(fs.ID.Value()).Query()
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
			t.Error("filtered right input discarded unmatched left rows")
		}
		statement, err := filteredReport.Compile()
		if err != nil {
			return err
		}
		if !strings.Contains(statement.SQL(), "LEFT JOIN (SELECT") || len(statement.Arguments()) != 2 {
			t.Error("source window or parameters were not preserved")
		}
		// An ON filter preserves outer rows; the corresponding final WHERE does not.
		onJoin := query.LeftJoin(referrals, sponsors, query.On(r.IntroducerID, s.ID).WhereRight(s.Status.Eq(models.StatusDisabled)))
		onLeft := models.UserFieldsAt(query.LeftScope(onJoin, referrals.Scope()))
		onRight := models.UserNullableFieldsAt(query.NullableRightScope(onJoin, sponsors.Scope()))
		onReport := reports.ProjectUserPair(onJoin).SelectLeftID(query.Nullable(onLeft.ID.Value())).SelectRightID(onRight.ID.Value()).Query()
		if n, err := onReport.Count(t.Context(), counter); err != nil || n != 4 {
			t.Error("ON scope discarded outer rows", err)
		}
		if n, err := onReport.Where(onRight.Status.Eq(models.StatusDisabled)).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("ON and WHERE semantics were conflated", err)
		}
		composite := query.LeftJoin(referrals, sponsors, query.OnAnd(query.On(r.IntroducerID, s.ID), query.On(r.Status, s.Status)))
		cl := models.UserFieldsAt(query.LeftScope(composite, referrals.Scope()))
		csp := models.UserNullableFieldsAt(query.NullableRightScope(composite, sponsors.Scope()))
		compositeReport := reports.ProjectUserPair(composite).SelectLeftID(query.Nullable(cl.ID.Value())).SelectRightID(csp.ID.Value()).Query()
		if n, err := compositeReport.Where(csp.ID.IsNotNull()).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("composite typed join did not preserve AND semantics", err)
		}
		alternative := query.InnerJoin(referrals, sponsors, query.OnOr(query.On(r.IntroducerID, s.ID), query.On(r.ID, s.ID)))
		al := models.UserFieldsAt(query.LeftScope(alternative, referrals.Scope()))
		ar := models.UserFieldsAt(query.RightScope(alternative, sponsors.Scope()))
		alternativeReport := reports.ProjectUserPair(alternative).SelectLeftID(query.Nullable(al.ID.Value())).SelectRightID(query.Nullable(ar.ID.Value())).Query()
		if n, err := alternativeReport.Count(t.Context(), counter); err != nil || n != 7 {
			t.Error("alternative typed join did not preserve OR semantics", err)
		}
		// Failure behavior: no SQL for canceled/invalid queries, and stream release.
		counter.Queries.Store(0)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if rows, err := report.All(ctx, counter); !errors.Is(err, context.Canceled) || rows != nil || counter.Queries.Load() != 0 {
			t.Error("canceled join performed SQL")
		}
		if rows, err := reports.ProjectReferralRow(joined).Query().All(t.Context(), counter); !errors.Is(err, fault.Invalid) || rows != nil || counter.Queries.Load() != 0 {
			t.Error("incomplete joined selection executed SQL")
		}
		stop := errors.New("stop joined stream")
		if err := report.Each(t.Context(), counter, func(reports.ReferralRow) error { return stop }); !errors.Is(err, stop) {
			t.Error("joined stream ignored callback failure", err)
		}
		if n, err := report.Count(t.Context(), counter); err != nil || n != 4 {
			t.Error("joined stream left transaction busy", err)
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE id=$1`, users[1].ID.String()); err != nil {
			return err
		}
		if rows, err := report.All(t.Context(), counter); err == nil || rows != nil {
			t.Error("malformed joined enum published partial results")
		}
		return nil
	})
}
