package notification

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"

	"github.com/TokenFlux/TokenRouter/internal/notification/contract"
)

type Mailer struct {
	settingRepo              SettingRepository
	transport                SMTPTransport
	notificationEmailService *NotificationEmailService
}

func NewMailer(repo SettingRepository, transport SMTPTransport) *Mailer {
	return &Mailer{settingRepo: repo, transport: transport}
}

func (s *Mailer) SetNotificationEmailService(n *NotificationEmailService) {
	s.notificationEmailService = n
}

func (s *Mailer) SendEmail(ctx context.Context, to, subject, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := s.GetSMTPConfig(ctx)
	if err != nil {
		return err
	}
	return s.transport.Send(ctx, cfg, to, subject, body)
}

func (s *Mailer) SendEmailWithConfigContext(ctx context.Context, cfg *SMTPConfig, to, subject, body string) error {
	return s.transport.Send(ctx, cfg, to, subject, body)
}

func (s *Mailer) TestSMTPConnectionContext(ctx context.Context, cfg *SMTPConfig) error {
	return s.transport.Test(ctx, cfg)
}

var ErrEmailNotConfigured = contract.ErrEmailNotConfigured

func EmailRecipientName(email string) string {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at > 0 {
		return trimmed[:at]
	}
	return trimmed
}

// GetSMTPConfig 从数据库获取SMTP配置
func (s *Mailer) GetSMTPConfig(ctx context.Context) (*SMTPConfig, error) {
	keys := []string{
		SettingKeySMTPHost,
		SettingKeySMTPPort,
		SettingKeySMTPUsername,
		SettingKeySMTPPassword,
		SettingKeySMTPFrom,
		SettingKeySMTPFromName,
		locale.TextSettingKey(SettingKeySMTPFromName),
		SettingKeySMTPUseTLS,
	}

	settings, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("get smtp settings: %w", err)
	}

	host := strings.TrimSpace(settings[SettingKeySMTPHost])
	if host == "" {
		return nil, ErrEmailNotConfigured
	}

	port := 587 // 默认端口
	if portStr := settings[SettingKeySMTPPort]; portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	useTLS := settings[SettingKeySMTPUseTLS] == "true"

	return &SMTPConfig{
		Host:     host,
		Port:     port,
		Username: strings.TrimSpace(settings[SettingKeySMTPUsername]),
		Password: strings.TrimSpace(settings[SettingKeySMTPPassword]),
		From:     strings.TrimSpace(settings[SettingKeySMTPFrom]),
		FromName: locale.ResolveSettingText(settings, SettingKeySMTPFromName, "", locale.FromContext(ctx)),
		UseTLS:   useTLS,
	}, nil
}

const (
	SettingKeySMTPFrom     = "smtp_from"
	SettingKeySMTPFromName = "smtp_from_name"
	SettingKeySMTPHost     = "smtp_host"
	SettingKeySMTPPassword = "smtp_password"
	SettingKeySMTPPort     = "smtp_port"
	SettingKeySMTPUseTLS   = "smtp_use_tls"
	SettingKeySMTPUsername = "smtp_username"
)

// SanitizeEmailHeader 防止模板参数注入额外邮件头。
func SanitizeEmailHeader(s string) string { return strings.NewReplacer("\r", "", "\n", "").Replace(s) }

// CheckTransport 检查邮件传输参数，建连和发送由后续调用执行。
func (s *NotificationEmailService) CheckTransport(ctx context.Context) error {
	if s == nil || s.emailService == nil {
		return ErrEmailNotConfigured
	}
	source, ok := s.emailService.(interface {
		GetSMTPConfig(context.Context) (*SMTPConfig, error)
	})
	if !ok {
		return ErrEmailNotConfigured
	}
	_, err := source.GetSMTPConfig(ctx)
	return err
}

// SendUserNotification 让用户邮件及内置模板回退共用语言选择和发送状态。
func (s *Mailer) SendUserNotification(ctx context.Context, input SendRequest) error {
	sender := s.notificationEmailService
	if sender == nil {
		sender = NewNotificationEmailService(s.settingRepo, s)
	}
	return sender.Send(ctx, input)
}
