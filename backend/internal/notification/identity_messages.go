package notification

import (
	"context"
	"strconv"
	"time"
)

const (
	verifyCodeTTL         = 15 * time.Minute
	passwordResetTokenTTL = 30 * time.Minute
)

// SendVerifyCodeMessage 使用同语言内置模板处理管理员覆盖缺失或损坏的情况。
func (s *Mailer) SendVerifyCodeMessage(ctx context.Context, email, siteName, code, language string) error {
	return s.SendUserNotification(ctx, SendRequest{
		Event: NotificationEmailEventAuthVerifyCode, Locale: language,
		RecipientEmail: email, RecipientName: EmailRecipientName(email),
		Variables: map[string]string{"verification_code": code, "expires_in_minutes": strconv.Itoa(int(verifyCodeTTL / time.Minute))},
	})
}

// SendPasswordResetMessage 将重置链接传入经过变量转义的语言模板。
func (s *Mailer) SendPasswordResetMessage(ctx context.Context, email, siteName, fullResetURL, language string) error {
	return s.SendUserNotification(ctx, SendRequest{
		Event: NotificationEmailEventAuthPasswordReset, Locale: language,
		RecipientEmail: email, RecipientName: EmailRecipientName(email),
		Variables: map[string]string{"reset_url": fullResetURL, "expires_in_minutes": strconv.Itoa(int(passwordResetTokenTTL / time.Minute))},
	})
}
