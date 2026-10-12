package moderation

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/moderation/contract"
	notice "github.com/TokenFlux/TokenRouter/internal/notification/contract"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

const (
	StatusActive                      = "active"
	StatusDisabled                    = "disabled"
	SettingKeyContentModerationConfig = "content_moderation_config"
	SettingKeyRiskControlEnabled      = "risk_control_enabled"
)

var ErrSettingNotFound = settings.ErrSettingNotFound

type (
	ContentModerationMedia = contract.ContentModerationMedia
	SettingRepository      = settings.Repository
	Setting                = settings.Setting
)

type UserSnapshot struct {
	ID                  int64
	Role, Status, Email string
}
type UserCommands interface {
	GetByID(context.Context, int64) (*UserSnapshot, error)
	SetStatus(context.Context, int64, string) error
}
type GroupRepository interface {
	CheckGroup(context.Context, int64) error
}
type ProxyInfo struct {
	URL, Name, Status, Address string
	Active, Expired            bool
}
type ProxyRepository interface {
	Lookup(context.Context, int64, time.Time) (ProxyInfo, error)
}
type (
	APIKeyAuthCacheInvalidator interface{ InvalidateAuthCacheByUserID(context.Context, int64) }
	AuditTransport             interface {
		Execute(context.Context, string, string, []byte, func() (string, error), any) (int, []byte, error)
	}
)

type Runtime struct {
	MissingRow    func(error) bool
	MissingUser   func(error) bool
	Audit         AuditTransport
	SnapshotMedia func(context.Context, []ContentModerationMedia) []ContentModerationMedia
	Background    func(string, func())
	CyberText     func(string) bool
	CyberPolicy   func([]byte) (bool, string, string)
	ErrorMessage  func([]byte) string
}
type RiskSender interface {
	SendViolationEmail(context.Context, *notice.RiskPolicy, *notice.RiskLog) error
	SendAccountDisabledEmail(context.Context, *notice.RiskPolicy, *notice.RiskLog) error
	SendCyberAccountDisabledEmail(context.Context, *notice.RiskPolicy, *notice.RiskWarning) error
}

type Decision = ContentModerationDecision
