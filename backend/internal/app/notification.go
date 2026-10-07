package app

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/ops"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	identitypostgres "github.com/TokenFlux/TokenRouter/internal/identity/postgres"
	identityredis "github.com/TokenFlux/TokenRouter/internal/identity/rediscache"
	"github.com/TokenFlux/TokenRouter/internal/notification"
	notificationhttp "github.com/TokenFlux/TokenRouter/internal/notification/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/notification/smtp"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/site"
	"github.com/redis/go-redis/v9"
)

func provideEmailCache(r *redis.Client) identity.EmailCache { return identityredis.NewEmailCache(r) }

func provideMailer(store *settings.Store) *notification.Mailer {
	return notification.NewMailer(store, smtp.New())
}

func provideEmailChallenges(cache identity.EmailCache, mail *notification.Mailer) *identity.EmailChallenges {
	return identity.NewEmailChallenges(cache, mail)
}

func provideNotification(store *settings.Store, mail *notification.Mailer, users *identitypostgres.UserStore) *notification.NotificationEmailService {
	n := notification.NewNotificationEmailService(store, mail)
	n.SetRecipientLocaleReader(func(ctx context.Context, id int64, email string) string {
		var user *identity.User
		var err error
		if id > 0 {
			user, err = users.GetByID(ctx, id)
		} else {
			user, err = users.GetByEmail(ctx, email)
		}
		if err == nil && user != nil && user.PreferredLocale != nil {
			return *user.PreferredLocale
		}
		return ""
	})
	mail.SetNotificationEmailService(n)
	return n
}

func provideEmailQueue(c *identity.EmailChallenges) *notification.EmailQueueService {
	return notification.NewEmailQueueService(c, 3)
}

func provideNotificationHTTP(mail *notification.Mailer, n *notification.NotificationEmailService, settings *site.DisplaySettings) *notificationhttp.Handler {
	return notificationhttp.New(mail, n, settings)
}

// notificationOpsDelivery 将报告和告警事件转换为通知数据。
func notificationOpsDelivery(mail *notification.Mailer, n *notification.NotificationEmailService) *ops.EmailDelivery {
	return &ops.EmailDelivery{TemplatesEnabled: n != nil, SendEmail: mail.SendEmail, RecipientName: notification.EmailRecipientName, ShouldFallback: notification.ShouldFallbackNotificationEmail, ResolveRecipientLocale: n.ResolveRecipientLocale, SendTemplate: func(ctx context.Context, v ops.Notification) error {
		return n.Send(ctx, notification.SendRequest{Event: v.Event, Locale: v.Locale, RecipientEmail: v.RecipientEmail, RecipientName: v.RecipientName, SourceType: v.SourceType, SourceID: v.SourceID, ReminderKey: v.ReminderKey, Variables: v.Variables, RawHTMLVariables: v.RawHTMLVariables})
	}}
}

func provideAlertDelivery(mail *notification.Mailer, n *notification.NotificationEmailService) *notification.AlertDelivery {
	return notification.NewAlertDelivery(mail, n)
}

func provideBalanceNotifications(sender *notification.AlertDelivery, settings *settings.Store, providers *providerpostgres.ProviderStore, tasks *lifecycle.Tasks) *billing.BalanceNotifyService {
	return billing.NewBalanceNotifyService(sender, settings, billingQuotaNotifyReader{providers}, func(name string, fn func()) { tasks.Go(name, fn) })
}

type billingQuotaNotifyReader struct {
	providers *providerpostgres.ProviderStore
}

func (r billingQuotaNotifyReader) GetByID(ctx context.Context, id int64) (*billing.QuotaNotifyProvider, error) {
	a, e := r.providers.GetByID(ctx, id)
	return provideradapter.QuotaNotification(a), e
}
