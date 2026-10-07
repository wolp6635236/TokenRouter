package notification

import (
	"context"
	"strconv"
	"time"
)

// SendNotifyVerification 按用户语言发送通知邮箱验证码。
func (s *Mailer) SendNotifyVerification(ctx context.Context, userID int64, email, code, language, siteName string) error {
	return s.SendUserNotification(ctx, SendRequest{
		Event: NotificationEmailEventNotificationEmailVerifyCode, Locale: language,
		RecipientEmail: email, RecipientName: EmailRecipientName(email), UserID: userID,
		Variables: map[string]string{"verification_code": code, "expires_in_minutes": strconv.Itoa(int(verifyCodeTTL / time.Minute))},
	})
}
