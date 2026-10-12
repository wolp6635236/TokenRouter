package identity_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	identitycore "github.com/TokenFlux/TokenRouter/internal/identity"
	identityprovider "github.com/TokenFlux/TokenRouter/internal/identity/provider"
)

// TestSyncDingTalkIdentity_UsesCfgAttrKeys_NoopWithNilService 检查使用自定义属性键且用户属性服务为空时，同步调用完成。
func TestSyncDingTalkIdentity_UsesCfgAttrKeys_NoopWithNilService(t *testing.T) {
	syncer := &identitycore.DingTalkSyncRuntime{Profiles: &identitycore.DingTalkProfileSync{}}

	cfg := identitycore.DingTalkOAuthOptions{
		CorpRestrictionPolicy: "internal_only",
		SyncCorpEmail:         true,
		SyncDisplayName:       true,
		SyncDept:              true,
		// 自定义 attr key（非默认值）
		SyncCorpEmailAttrKey:   "custom_email_key",
		SyncDisplayNameAttrKey: "custom_name_key",
		SyncDeptAttrKey:        "custom_dept_key",
	}

	staff := &identityprovider.DingTalkStaffInfo{
		Name:  "张三",
		Email: "zhangsan@example.com",
	}

	// 调用不应 panic（userAttributeService 为 nil 时走 warn 跳过路径）
	require.NotPanics(t, func() {
		syncer.Sync(context.Background(), cfg, nil, 42, staff, false)
	})
}

// TestSyncDingTalkIdentity_DefaultAttrKeys_NoopWithNilService 检查使用默认属性键且用户属性服务为空时，同步调用完成。
func TestSyncDingTalkIdentity_DefaultAttrKeys_NoopWithNilService(t *testing.T) {
	syncer := &identitycore.DingTalkSyncRuntime{Profiles: &identitycore.DingTalkProfileSync{}}

	cfg := identitycore.DingTalkOAuthOptions{
		CorpRestrictionPolicy: "internal_only",
		SyncCorpEmail:         true,
		SyncDisplayName:       true,
		SyncDept:              false,
		// 配置读取阶段填充的默认属性键。
		SyncCorpEmailAttrKey:   "dingtalk_email",
		SyncDisplayNameAttrKey: "dingtalk_name",
		SyncDeptAttrKey:        "dingtalk_department",
	}

	staff := &identityprovider.DingTalkStaffInfo{
		Name:  "李四",
		Email: "lisi@corp.com",
	}

	require.NotPanics(t, func() {
		syncer.Sync(context.Background(), cfg, nil, 99, staff, false)
	})
}
