package identity_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/stretchr/testify/require"
)

// 这些用例锁死"每个入口只声明自己真正要改的列"：
// 任何退回整行回写的改动都会让并发写入被陈旧快照覆盖，并在这里变红。

func TestUpdateProfile_OnlyDeclaresRequestedColumns(t *testing.T) {
	username := "renamed"
	tests := []struct {
		name string
		req  identity.UpdateProfileRequest
		want identity.UserUpdateFields
	}{
		{
			name: "username only",
			req:  identity.UpdateProfileRequest{Username: &username},
			want: identity.UserUpdateFields{Username: true},
		},
		{
			name: "notify settings only",
			req:  identity.UpdateProfileRequest{BalanceNotifyEnabled: boolPtr(true)},
			want: identity.UserUpdateFields{BalanceNotifySettings: true},
		},
		{
			name: "username and notify threshold",
			req:  identity.UpdateProfileRequest{Username: &username, BalanceNotifyThreshold: float64Ptr(1.5)},
			want: identity.UserUpdateFields{Username: true, BalanceNotifySettings: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockUserRepo{getByIDUser: &identity.User{ID: 7, Balance: 0.30, Status: identity.StatusActive}}
			svc := identity.NewUserService(repo, nil, nil, nil, runProfileBackground)

			_, err := svc.UpdateProfile(context.Background(), 7, tt.req)
			require.NoError(t, err)
			require.Equal(t, []identity.UserUpdateFields{tt.want}, repo.updateFields)
		})
	}
}

// TestUpdateProfile_AvatarOnlySkipsUserRowWrite 检查单独更新头像时跳过用户行更新。
func TestUpdateProfile_AvatarOnlySkipsUserRowWrite(t *testing.T) {
	repo := &mockUserRepo{getByIDUser: &identity.User{ID: 7, Balance: 0.30}}
	svc := identity.NewUserService(repo, nil, nil, nil, runProfileBackground)

	avatar := "https://cdn.example.com/a.png"
	_, err := svc.UpdateProfile(context.Background(), 7, identity.UpdateProfileRequest{AvatarURL: &avatar})
	require.NoError(t, err)
	require.Len(t, repo.upsertAvatarArgs, 1, "avatar must still be stored")
	require.Equal(t, []identity.UserUpdateFields{{}}, repo.updateFields, "no user column should be declared")
}

func TestChangePassword_OnlyDeclaresPasswordHash(t *testing.T) {
	user := &identity.User{ID: 7, Balance: 0.30}
	require.NoError(t, user.SetPassword("old-password"))
	repo := &mockUserRepo{getByIDUser: user}
	svc := identity.NewUserService(repo, nil, nil, nil, runProfileBackground)

	err := svc.ChangePassword(context.Background(), 7, identity.ChangePasswordRequest{
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	})
	require.NoError(t, err)
	require.Equal(t, []identity.UserUpdateFields{{PasswordHash: true}}, repo.updateFields)
}

func TestUpdateStatus_OnlyDeclaresStatus(t *testing.T) {
	repo := &mockUserRepo{getByIDUser: &identity.User{ID: 7, Balance: 0.30, Status: identity.StatusActive}}
	svc := identity.NewUserService(repo, nil, nil, nil, runProfileBackground)

	require.NoError(t, svc.UpdateStatus(context.Background(), 7, "disabled"))
	require.Equal(t, []identity.UserUpdateFields{{Status: true}}, repo.updateFields)
}

// TestUpdateProfileLanguagePreference 检查偏好规范化、清除以及与其他资料字段的独立更新。
func TestUpdateProfileLanguagePreference(t *testing.T) {
	en, zh, invalid := "en", "zh", "unsupported"
	for _, tc := range []struct {
		name      string
		input     identity.UpdateProfileRequest
		expected  *string
		wantError bool
	}{
		{name: "alias", input: identity.UpdateProfileRequest{PreferredLocale: &zh}, expected: func() *string { value := "zh-Hans"; return &value }()},
		{name: "clear", input: identity.UpdateProfileRequest{ClearPreferredLocale: true}},
		{name: "omitted", input: identity.UpdateProfileRequest{}, expected: &en},
		{name: "invalid", input: identity.UpdateProfileRequest{PreferredLocale: &invalid}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockUserRepo{getByIDUser: &identity.User{ID: 7, PreferredLocale: &en, Status: identity.StatusActive}}
			service := identity.NewUserService(repo, nil, nil, nil, runProfileBackground)
			updated, err := service.UpdateProfile(context.Background(), 7, tc.input)
			if tc.wantError {
				require.Error(t, err)
				require.Empty(t, repo.updateFields)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, updated.PreferredLocale)
			changed := tc.input.PreferredLocale != nil || tc.input.ClearPreferredLocale
			require.Equal(t, []identity.UserUpdateFields{{PreferredLocale: changed}}, repo.updateFields)
		})
	}
}
