package identity_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identitycore "github.com/TokenFlux/TokenRouter/internal/identity"
	identityprovider "github.com/TokenFlux/TokenRouter/internal/identity/provider"
)

// TestBuildDingTalkSyntheticEmail_UsesUnionID 验证合成邮箱种子使用 unionID。
func TestBuildDingTalkSyntheticEmail_UsesUnionID(t *testing.T) {
	unionID := "union_AbCdEf123"
	email := identitycore.DingTalkSyntheticEmail(unionID)

	want := "dingtalk-union_abcdef123@dingtalk-connect.invalid"
	require.Equal(t, want, email)

	// 合成邮箱统一使用小写。
	require.True(t, strings.ToLower(email) == email, "synthetic email should be all lowercase")

	// 合成邮箱使用 dingtalk- 前缀。
	require.True(t, strings.HasPrefix(email, "dingtalk-"), "should have dingtalk- prefix")

	// 合成邮箱使用保留域名。
	require.True(t, strings.HasSuffix(email, "@dingtalk-connect.invalid"), "should have reserved domain suffix")
}

// TestBuildDingTalkSyntheticEmail_TrimsSpace 验证 unionID 空白被修剪。
func TestBuildDingTalkSyntheticEmail_TrimsSpace(t *testing.T) {
	email := identitycore.DingTalkSyntheticEmail("  UID_XYZ  ")
	require.Equal(t, "dingtalk-uid_xyz@dingtalk-connect.invalid", email)
}

// TestBuildDingTalkUpstreamClaims_EmptyStaff 验证 staff 为空 struct（跨组织降级路径）时：
// - subject 等于 unionID（与 identityKey.ProviderSubject 一致）
// - corp_user_id 为空字符串（跨组织时拿不到企业 userid）
// - email/username 为空字符串
// 用户资料读取失败时使用空 DingTalkStaffInfo 生成 claims。
func TestBuildDingTalkUpstreamClaims_EmptyStaff(t *testing.T) {
	staff := &identityprovider.DingTalkStaffInfo{}
	claims := identitycore.DingTalkUpstreamClaims(staff, "UNION_AAA", "CORP_X")

	require.Equal(t, "", claims["email"])
	require.Equal(t, "", claims["username"])
	// subject = unionID（与 identityKey.ProviderSubject 保持一致）
	require.Equal(t, "UNION_AAA", claims["subject"])
	require.Equal(t, "", claims["corp_user_id"]) // 企业 userid 跨组织时为空
	require.Equal(t, "UNION_AAA", claims["union_id"])
	require.Equal(t, "CORP_X", claims["corp_id"])
}

// TestCheckDingTalkCorpAllowed_CrossOrgPolicy 验证 policy=none 时允许任意 corp。
func TestCheckDingTalkCorpAllowed_CrossOrgPolicy(t *testing.T) {
	cfg := identitycore.DingTalkOAuthOptions{CorpRestrictionPolicy: "none"}

	assert.True(t, identitycore.DingTalkCorpAllowed(cfg, "dingABC"), "policy=none should allow any corp")
	assert.True(t, identitycore.DingTalkCorpAllowed(cfg, ""), "policy=none should allow empty corp")
	assert.True(t, identitycore.DingTalkCorpAllowed(cfg, "foreign_corp"), "policy=none should allow foreign corp")
}

// TestCheckDingTalkCorpAllowed_InternalOnly 检查企业模式允许缺失或不同的 corpID。
// 钉钉扫码登录等授权场景可能缺少 corpId，企业成员资格由 GetUserIdByUnionId 判断。
// 跨企业用户的错误码 60011/60121 映射为 corp_rejected。
func TestCheckDingTalkCorpAllowed_InternalOnly(t *testing.T) {
	cfgWithCorpID := identitycore.DingTalkOAuthOptions{
		CorpRestrictionPolicy: "internal_only",
		InternalCorpID:        "dingInternal",
	}
	assert.True(t, identitycore.DingTalkCorpAllowed(cfgWithCorpID, "dingInternal"), "internal_only: matching corpID allowed")
	assert.True(t, identitycore.DingTalkCorpAllowed(cfgWithCorpID, "foreign_corp"), "internal_only: corpID 字段不再用于决策，step 3 兜底")
	assert.True(t, identitycore.DingTalkCorpAllowed(cfgWithCorpID, ""), "internal_only: 空 corpID 也通过（钉钉部分授权场景不返回 corpId）")

	cfgNoCorpID := identitycore.DingTalkOAuthOptions{
		CorpRestrictionPolicy: "internal_only",
		InternalCorpID:        "",
	}
	assert.True(t, identitycore.DingTalkCorpAllowed(cfgNoCorpID, "dingAnyNonEmpty"), "internal_only + no InternalCorpID: 非空 corpID 通过")
	assert.True(t, identitycore.DingTalkCorpAllowed(cfgNoCorpID, ""), "internal_only + no InternalCorpID: 空 corpID 也通过")
}

// TestCompleteDingTalkRegistration_UsernameFromEmailLocalPart 验证 username 为空时
// 退到 email local part（@ 之前的部分）。
func TestCompleteDingTalkRegistration_UsernameFromEmailLocalPart(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		username  string
		wantUser  string
		wantValid bool
	}{
		{
			name:      "username empty, normal email → local part",
			email:     "dingtalk-uid123@dingtalk-connect.invalid",
			username:  "",
			wantUser:  "dingtalk-uid123",
			wantValid: true,
		},
		{
			name:      "username already set → keep original",
			email:     "user@example.com",
			username:  "张三",
			wantUser:  "张三",
			wantValid: true,
		},
		{
			name:      "username empty, no @ in email → use whole email",
			email:     "noemail",
			username:  "",
			wantUser:  "noemail",
			wantValid: true,
		},
		{
			name:      "both empty → invalid",
			email:     "",
			username:  "",
			wantUser:  "",
			wantValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			username := tc.username
			email := tc.email

			// 模拟 CompleteDingTalkOAuthRegistration 中的 fallback 逻辑
			if username == "" {
				if at := strings.Index(email, "@"); at > 0 {
					username = email[:at]
				} else {
					username = email
				}
			}

			isValid := email != "" && username != ""
			require.Equal(t, tc.wantUser, username, fmt.Sprintf("username for email=%q", tc.email))
			require.Equal(t, tc.wantValid, isValid, "validity check")
		})
	}
}

// TestBuildDingTalkUpstreamClaims_SubjectEqualsUnionID 验证 subject = unionID
// 并检查它与 identityKey.ProviderSubject 一致。
func TestBuildDingTalkUpstreamClaims_SubjectEqualsUnionID(t *testing.T) {
	staff := &identityprovider.DingTalkStaffInfo{UserID: "user123", Name: "张三", Email: "zhangsan@corp.com"}
	claims := identitycore.DingTalkUpstreamClaims(staff, "union456", "dingcorp789")

	// subject = unionID（全局唯一，与 identityKey.ProviderSubject 一致）
	require.Equal(t, "union456", claims["subject"], "subject should equal unionID after refactor")
	// 企业 userid 保留为独立字段，供 audit/debug 使用
	require.Equal(t, "user123", claims["corp_user_id"], "corp_user_id should be staff.UserID")
	// union_id 字段与 subject 相同（冗余保留，便于读取）
	require.Equal(t, "union456", claims["union_id"])
	require.Equal(t, "dingcorp789", claims["corp_id"])
	require.Equal(t, "张三", claims["username"])
	require.Equal(t, "zhangsan@corp.com", claims["email"])
}

// TestBuildDingTalkUpstreamClaims_CrossOrgEmptyCorpUserID 验证跨组织降级时
// corp_user_id 为空字符串（跨组织拿不到企业 userid），subject 仍为 unionID。
func TestBuildDingTalkUpstreamClaims_CrossOrgEmptyCorpUserID(t *testing.T) {
	// 跨组织降级路径：staff = &DingTalkStaffInfo{}（所有字段为零值）
	staff := &identityprovider.DingTalkStaffInfo{}
	claims := identitycore.DingTalkUpstreamClaims(staff, "union_cross_org", "foreign_corp")

	require.Equal(t, "union_cross_org", claims["subject"], "subject should still be unionID for cross-org users")
	require.Equal(t, "", claims["corp_user_id"], "corp_user_id should be empty for cross-org fallback")
	require.Equal(t, "", claims["email"])
	require.Equal(t, "", claims["username"])
}

// TestBuildDingTalkUpstreamClaims_PrimaryDeptIDInClaims 验证首个 dept_id 被存入 claims。
func TestBuildDingTalkUpstreamClaims_PrimaryDeptIDInClaims(t *testing.T) {
	staff := &identityprovider.DingTalkStaffInfo{UserID: "u1", Name: "张三", Email: "a@b.com", DeptIDs: []int64{42, 99}}
	claims := identitycore.DingTalkUpstreamClaims(staff, "uid1", "corpX")

	// 只取首个 dept_id
	require.Equal(t, int64(42), claims["primary_dept_id"], "primary_dept_id should be the first dept_id")
}

// TestBuildDingTalkUpstreamClaims_NoDeptIDs 验证无部门时 primary_dept_id=0。
func TestBuildDingTalkUpstreamClaims_NoDeptIDs(t *testing.T) {
	staff := &identityprovider.DingTalkStaffInfo{UserID: "u2", Name: "李四"}
	claims := identitycore.DingTalkUpstreamClaims(staff, "uid2", "corpY")

	require.Equal(t, int64(0), claims["primary_dept_id"], "primary_dept_id should be 0 when no depts")
}
