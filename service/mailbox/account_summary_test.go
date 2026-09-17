package mailbox

import (
	"fmt"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestOperatorAccountSummaryAndPriority(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := t.Context()
	var ids []int64
	for i := 0; i < 25; i++ {
		a := workflowTestAccount(t, s, fmt.Sprintf("summary-%02d@example.test", i))
		workflowTestAssign(t, s, admin, owner, a.ID)
		ids = append(ids, a.ID)
		if i >= 5 {
			require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("account_id = ?", a.ID).Update("status", StatusSubmitted).Error)
		}
	}
	query := ListQuery{PageSize: 20, IncludeSummary: true, Sort: "pending_first"}
	page, err := s.ListAccounts(ctx, owner, query)
	require.NoError(t, err)
	require.EqualValues(t, 25, page.Total)
	require.EqualValues(t, 5, page.StatusCounts[StatusPending])
	require.EqualValues(t, 20, page.StatusCounts[StatusSubmitted])
	require.EqualValues(t, 0, page.StatusCounts[StatusRejected])
	require.Len(t, page.Items, 20)
	require.Equal(t, ids[4], page.Items[0].ID)
	require.Equal(t, ids[24], page.Items[5].ID)
	query.Page = 2
	next, err := s.ListAccounts(ctx, owner, query)
	require.NoError(t, err)
	require.Len(t, next.Items, 5)
	require.Equal(t, page.StatusCounts, next.StatusCounts)
	legacy, err := s.ListAccounts(ctx, owner, ListQuery{PageSize: 20})
	require.NoError(t, err)
	require.Nil(t, legacy.StatusCounts)
	require.Equal(t, ids[24], legacy.Items[0].ID)
	query.Page = 1
	query.Status = StatusSubmitted
	query.Search = "summary-2"
	filtered, err := s.ListAccounts(ctx, owner, query)
	require.NoError(t, err)
	require.EqualValues(t, 5, filtered.Total)
	require.EqualValues(t, 5, filtered.StatusCounts[StatusSubmitted])
	require.Zero(t, filtered.StatusCounts[StatusPending])
	foreign, err := s.ListAccounts(ctx, other, query)
	require.NoError(t, err)
	require.Zero(t, foreign.Total)
	require.Zero(t, foreign.StatusCounts[StatusSubmitted])
	query.AccountType = AccountTypeOpening
	opening, err := s.ListAccounts(ctx, owner, query)
	require.NoError(t, err)
	require.Zero(t, opening.Total)
	query.Sort = "id DESC; --"
	_, err = s.ListAccounts(ctx, owner, query)
	require.EqualError(t, err, "mailbox_invalid_query")
}
