package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/notification"
	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

type expiryNotifications struct {
	Service *notification.NotificationEmailService
}

func (n expiryNotifications) Ready(ctx context.Context) error {
	if n.Service == nil {
		return billing.ErrReminderTransportUnconfigured
	}
	err := n.Service.CheckTransport(ctx)
	if errors.Is(err, notification.ErrEmailNotConfigured) {
		return billing.ErrReminderTransportUnconfigured
	}
	return err
}

func (n expiryNotifications) Send(ctx context.Context, r billing.ExpiryReminder) error {
	language := n.Service.ResolveRecipientLocale(ctx, r.UserID, r.RecipientEmail)
	if r.PlanLocalization.Revision > 0 {
		copy, _ := locale.Content[billing.PlanCopy](r.PlanLocalization).Resolve(language)
		r.PlanName = copy.Name
	}
	return n.Service.Send(ctx, notification.SendRequest{
		Locale:         language,
		Event:          notification.NotificationEmailEventSubscriptionExpiryReminder,
		RecipientEmail: r.RecipientEmail, RecipientName: r.RecipientName, UserID: r.UserID,
		SourceType: "user_subscription", SourceID: strconv.FormatInt(r.SubscriptionID, 10),
		ReminderKey: fmt.Sprintf("%dd", r.DaysRemaining),
		Variables:   map[string]string{"subscription_group": r.PlanName, "expiry_time": r.ExpiresAt.Format("2006-01-02 15:04"), "days_remaining": strconv.Itoa(r.DaysRemaining)},
	})
}
