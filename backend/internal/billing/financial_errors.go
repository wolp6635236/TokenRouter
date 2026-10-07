package billing

import (
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

var ErrAPIKeyNotFound = apperror.NotFound("API_KEY_NOT_FOUND", "api key not found")

var ErrAPIKeyQuotaExhausted = apperror.TooManyRequests("API_KEY_QUOTA_EXHAUSTED", "api key quota exhausted")

var ErrAPIKeyRateLimit1dExceeded = apperror.TooManyRequests("API_KEY_RATE_1D_EXCEEDED", "The API key daily limit has been reached")

var ErrAPIKeyRateLimit5hExceeded = apperror.TooManyRequests("API_KEY_RATE_5H_EXCEEDED", "The API key five-hour limit has been reached")

var ErrAPIKeyRateLimit7dExceeded = apperror.TooManyRequests("API_KEY_RATE_7D_EXCEEDED", "The API key seven-day limit has been reached")

var ErrProviderNotFound = apperror.NotFound("PROVIDER_NOT_FOUND", "provider not found")

var ErrTaskInsufficientBalance = apperror.New(apperror.Category(402), "BATCH_IMAGE_INSUFFICIENT_BALANCE", "insufficient balance for batch image hold")

var ErrTaskNotFound = apperror.New(apperror.CategoryNotFound, "BATCH_IMAGE_JOB_NOT_FOUND", "batch image job not found")

var ErrPreferredSubscriptionInsufficient = apperror.TooManyRequests("PREFERRED_SUBSCRIPTION_EXHAUSTED", "preferred subscription has insufficient remaining quota")

var ErrPreferredSubscriptionInvalid = apperror.Forbidden("PREFERRED_SUBSCRIPTION_INVALID", "preferred subscription is unavailable")

var ErrSubscriptionNotFound = apperror.NotFound("SUBSCRIPTION_NOT_FOUND", "subscription not found")

var ErrTeamMemberDailyExceeded = apperror.TooManyRequests("TEAM_MEMBER_DAILY_LIMIT_EXCEEDED", "The team member daily limit has been reached")

var ErrTeamMemberMonthlyExceeded = apperror.TooManyRequests("TEAM_MEMBER_MONTHLY_LIMIT_EXCEEDED", "The team member monthly limit has been reached")

var ErrTeamMemberWeeklyExceeded = apperror.TooManyRequests("TEAM_MEMBER_WEEKLY_LIMIT_EXCEEDED", "The team member weekly limit has been reached")

var ErrTeamMembershipRequired = apperror.Forbidden("TEAM_MEMBERSHIP_REQUIRED", "Team membership is required")

var ErrUserNotFound = apperror.NotFound("USER_NOT_FOUND", "user not found")

var ErrInsufficientBalance = apperror.BadRequest("INSUFFICIENT_BALANCE", "insufficient balance")

var ErrPreferredSubscriptionGroup = apperror.Forbidden("PREFERRED_SUBSCRIPTION_GROUP_NOT_ALLOWED", "preferred subscription does not allow this group")
