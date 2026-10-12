package contract

import "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"

var ErrEmailNotConfigured = apperror.ServiceUnavailable("EMAIL_NOT_CONFIGURED", "email service not configured")

// SMTPConfig SMTP 配置。
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	UseTLS   bool
}
