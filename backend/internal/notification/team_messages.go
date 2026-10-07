package notification

import (
	"context"
	"time"
)

// RecipientName 使用邮箱中的可读部分称呼收件人。
func (s *Mailer) RecipientName(email string) string { return EmailRecipientName(email) }

// SendTeamInvitation 按收件人的语言发送团队邀请。
func (s *Mailer) SendTeamInvitation(ctx context.Context, email, recipientName string, recipientUserID int64, teamName, link string, expiresAt time.Time) error {
	return s.SendUserNotification(ctx, SendRequest{
		Event:          NotificationEmailEventTeamInvitation,
		RecipientEmail: email, RecipientName: recipientName, UserID: recipientUserID,
		Variables: map[string]string{"team_name": teamName, "invitation_url": link, "expires_at": expiresAt.Format(time.RFC3339)},
	})
}

// SendOwnershipTransfer 按收件人的偏好发送所有权转让请求。
func (s *Mailer) SendOwnershipTransfer(ctx context.Context, email, teamName, link string) error {
	return s.SendUserNotification(ctx, SendRequest{
		Event:          NotificationEmailEventTeamOwnershipTransfer,
		RecipientEmail: email, RecipientName: EmailRecipientName(email),
		Variables: map[string]string{"team_name": teamName, "transfer_url": link},
	})
}
