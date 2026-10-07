package notification

import (
	"context"
	"testing"

	mailtest "github.com/TokenFlux/TokenRouter/internal/notification/testkit"
	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
	"github.com/stretchr/testify/require"
)

type localeDelivery struct {
	language, subject, body string
	count                   int
}

func (s *localeDelivery) SendEmail(ctx context.Context, _, subject, body string) error {
	s.language, s.subject, s.body = locale.FromContext(ctx), subject, body
	s.count++
	return nil
}

// TestUserNotificationLocales 覆盖用户事件、非法自定义模板回退和事件语言优先级。
func TestUserNotificationLocales(t *testing.T) {
	events := []string{NotificationEmailEventAuthVerifyCode, NotificationEmailEventAuthPasswordReset, NotificationEmailEventNotificationEmailVerifyCode, NotificationEmailEventTeamInvitation, NotificationEmailEventTeamOwnershipTransfer, NotificationEmailEventSubscriptionPurchaseSuccess, NotificationEmailEventSubscriptionExpiryReminder, NotificationEmailEventBalanceLow, NotificationEmailEventBalanceRechargeSuccess, NotificationEmailEventContentModerationViolation, NotificationEmailEventContentModerationDisabled}
	for _, event := range events {
		t.Run(event, func(t *testing.T) {
			ctx := context.Background()
			repo := mailtest.NewMemorySettings()
			sender := &localeDelivery{}
			service := NewNotificationEmailService(repo, sender)
			service.SetRecipientLocaleReader(func(context.Context, int64, string) string { return "zh" })
			require.NoError(t, repo.Set(ctx, notificationEmailTemplateKey(event, "zh-Hans"), `{"subject":"broken","html":"{{invalid_placeholder}}"}`))
			input := SendRequest{Event: event, RecipientEmail: "user@example.com", RecipientName: "User", UserID: 12}
			require.NoError(t, service.Send(ctx, input))
			require.Equal(t, "zh-Hans", sender.language)
			require.NotContains(t, sender.body, "invalid_placeholder")
			require.NotEqual(t, "broken", sender.subject)
			input.Locale = "en"
			require.NoError(t, service.Send(ctx, input))
			require.Equal(t, "en", sender.language)
			require.Equal(t, 2, sender.count)
		})
	}
}

// TestRecipientLocalePriority 检查账户、邮箱和站点默认语言的次序。
func TestRecipientLocalePriority(t *testing.T) {
	ctx := context.Background()
	repo := mailtest.NewMemorySettings()
	service := NewNotificationEmailService(repo, nil)
	require.NoError(t, repo.Set(ctx, "default_locale", "zh-Hans"))
	require.Equal(t, "zh-Hans", service.ResolveRecipientLocale(ctx, 5, "u@example.com"))
	service.RememberRecipientLocale(ctx, 5, "u@example.com", "en")
	require.Equal(t, "en", service.ResolveRecipientLocale(ctx, 5, "u@example.com"))
	service.SetRecipientLocaleReader(func(context.Context, int64, string) string { return "zh-Hans" })
	require.Equal(t, "zh-Hans", service.ResolveRecipientLocale(ctx, 5, "u@example.com"))
}
