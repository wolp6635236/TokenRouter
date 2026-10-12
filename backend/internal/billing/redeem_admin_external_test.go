package billing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingpostgres "github.com/TokenFlux/TokenRouter/internal/billing/postgres"
)

func TestAdminService_DeleteRedeemCode_Success(t *testing.T) {
	repo := &redeemRepoStub{}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	err := svc.DeleteRedeemCode(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, []int64{10}, repo.deletedIDs)
}

func TestAdminService_DeleteRedeemCode_Idempotent(t *testing.T) {
	repo := &redeemRepoStub{getErrByID: map[int64]error{999: billing.ErrRedeemCodeNotFound}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	err := svc.DeleteRedeemCode(context.Background(), 999)
	require.ErrorIs(t, err, billing.ErrRedeemCodeNotFound)
	require.Empty(t, repo.deletedIDs)
}

func TestAdminService_DeleteRedeemCode_Error(t *testing.T) {
	deleteErr := errors.New("delete failed")
	repo := &redeemRepoStub{deleteErrByID: map[int64]error{1: deleteErr}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	err := svc.DeleteRedeemCode(context.Background(), 1)
	require.ErrorIs(t, err, deleteErr)
	require.Equal(t, []int64{1}, repo.deletedIDs)
}

func TestAdminService_BatchDeleteRedeemCodes_Success(t *testing.T) {
	repo := &redeemRepoStub{}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	deleted, err := svc.BatchDeleteRedeemCodes(context.Background(), []int64{1, 2, 3})
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)
	require.Equal(t, []int64{1, 2, 3}, repo.deletedIDs)
}

func TestAdminService_BatchDeleteRedeemCodes_PartialFailures(t *testing.T) {
	repo := &redeemRepoStub{
		deleteErrByID: map[int64]error{
			2: errors.New("db error"),
		},
	}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	deleted, err := svc.BatchDeleteRedeemCodes(context.Background(), []int64{1, 2, 3})
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.Equal(t, []int64{1, 2, 3}, repo.deletedIDs)
}

func TestAdminService_UpdateRedeemCode_UnusedBalanceUpdatesValueLimitAndExpiry(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	value := 25.5
	maxUses := 3
	repo := &redeemRepoStub{codesByID: map[int64]*billing.RedeemCode{
		1: {ID: 1, Code: "R-1", Type: billing.RedeemTypeBalance, Value: 10, Status: billing.StatusUnused, MaxUses: 1},
	}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	updated, err := svc.UpdateRedeemCode(context.Background(), 1, &billing.UpdateRedeemCodeInput{
		Value:        &value,
		MaxUses:      &maxUses,
		ExpiresAt:    &expiresAt,
		ExpiresAtSet: true,
	})

	require.NoError(t, err)
	require.Equal(t, value, updated.Value)
	require.Equal(t, maxUses, updated.MaxUses)
	require.Equal(t, &expiresAt, updated.ExpiresAt)
	require.Equal(t, billing.StatusUnused, updated.Status)
	require.Len(t, repo.updatedCodes, 1)
	require.Equal(t, []int64{1}, repo.lockedGetIDs)
}

func TestAdminService_UpdateRedeemCode_UsedValueLocked(t *testing.T) {
	value := 50.0
	repo := &redeemRepoStub{codesByID: map[int64]*billing.RedeemCode{
		1: {ID: 1, Code: "R-1", Type: billing.RedeemTypeBalance, Value: 10, Status: billing.StatusUsed, MaxUses: 1, UsedCount: 1},
	}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	_, err := svc.UpdateRedeemCode(context.Background(), 1, &billing.UpdateRedeemCodeInput{Value: &value})

	require.Error(t, err)
	require.ErrorContains(t, err, "value or plan cannot be updated")
	require.Empty(t, repo.updatedCodes)
}

func TestAdminService_UpdateRedeemCode_RejectsMaxUsesBelowUsedCount(t *testing.T) {
	maxUses := 1
	repo := &redeemRepoStub{codesByID: map[int64]*billing.RedeemCode{
		1: {ID: 1, Code: "R-1", Type: billing.RedeemTypeBalance, Value: 10, Status: billing.StatusActive, MaxUses: 3, UsedCount: 2},
	}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	_, err := svc.UpdateRedeemCode(context.Background(), 1, &billing.UpdateRedeemCodeInput{MaxUses: &maxUses})

	require.Error(t, err)
	require.ErrorContains(t, err, "max_uses cannot be less than used_count")
	require.Empty(t, repo.updatedCodes)
}

func TestAdminService_UpdateRedeemCode_RestoresExhaustedCodeWhenLimitIncreases(t *testing.T) {
	maxUses := 2
	repo := &redeemRepoStub{codesByID: map[int64]*billing.RedeemCode{
		1: {ID: 1, Code: "R-1", Type: billing.RedeemTypeBalance, Value: 10, Status: billing.StatusUsed, MaxUses: 1, UsedCount: 1},
	}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	updated, err := svc.UpdateRedeemCode(context.Background(), 1, &billing.UpdateRedeemCodeInput{MaxUses: &maxUses})

	require.NoError(t, err)
	require.Equal(t, billing.StatusActive, updated.Status)
	require.Equal(t, maxUses, updated.MaxUses)
}

func TestAdminService_UpdateRedeemCode_RestoresExpiredCodeWhenExpiryCleared(t *testing.T) {
	repo := &redeemRepoStub{codesByID: map[int64]*billing.RedeemCode{
		1: {
			ID:        1,
			Code:      "R-1",
			Type:      billing.RedeemTypeBalance,
			Value:     10,
			Status:    billing.StatusExpired,
			MaxUses:   3,
			UsedCount: 1,
			ExpiresAt: ptrTime(time.Now().Add(-time.Hour)),
		},
	}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	updated, err := svc.UpdateRedeemCode(context.Background(), 1, &billing.UpdateRedeemCodeInput{ExpiresAtSet: true})

	require.NoError(t, err)
	require.Nil(t, updated.ExpiresAt)
	require.Equal(t, billing.StatusActive, updated.Status)
}

func TestAdminService_UpdateRedeemCode_InvitationKeepsExpiry(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	maxUses := 0
	repo := &redeemRepoStub{codesByID: map[int64]*billing.RedeemCode{
		1: {ID: 1, Code: "INVITE-1", Type: billing.RedeemTypeInvitation, Status: billing.StatusUnused, MaxUses: 1},
	}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	updated, err := svc.UpdateRedeemCode(context.Background(), 1, &billing.UpdateRedeemCodeInput{
		MaxUses:      &maxUses,
		ExpiresAt:    &expiresAt,
		ExpiresAtSet: true,
	})

	require.NoError(t, err)
	require.Equal(t, 1, updated.MaxUses)
	require.Equal(t, &expiresAt, updated.ExpiresAt)
	require.Equal(t, billing.StatusUnused, updated.Status)
}

func TestAdminService_UpdateRedeemCode_RejectsSystemRecords(t *testing.T) {
	maxUses := 2
	repo := &redeemRepoStub{codesByID: map[int64]*billing.RedeemCode{
		1: {ID: 1, Code: "AFF-1", Type: "affiliate_balance", Value: 10, Status: billing.StatusUsed, MaxUses: 1, UsedCount: 1},
	}}
	svc := billing.NewRedeemAdmin(repo, billingpostgres.NewRedeemAdministrationMutations(nil), time.Now)

	_, err := svc.UpdateRedeemCode(context.Background(), 1, &billing.UpdateRedeemCodeInput{MaxUses: &maxUses})

	require.Error(t, err)
	require.ErrorContains(t, err, "system redeem records cannot be updated")
	require.Empty(t, repo.updatedCodes)
}
