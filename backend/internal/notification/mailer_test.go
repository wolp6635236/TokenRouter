package notification

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/notification/smtp"
	mailtest "github.com/TokenFlux/TokenRouter/internal/notification/testkit"
)

func TestSanitizeEmailHeader_CRLF(t *testing.T) {
	require.Equal(t, "Subject injected", SanitizeEmailHeader("Subject\r\n injected"))
}

func TestSanitizeEmailHeader_OnlyCR(t *testing.T) {
	require.Equal(t, "foobar", SanitizeEmailHeader("foo\rbar"))
}

func TestSanitizeEmailHeader_OnlyLF(t *testing.T) {
	require.Equal(t, "foobar", SanitizeEmailHeader("foo\nbar"))
}

func TestSanitizeEmailHeader_Clean(t *testing.T) {
	require.Equal(t, "TokenRouter", SanitizeEmailHeader("TokenRouter"))
}

func TestSanitizeEmailHeader_Empty(t *testing.T) {
	require.Equal(t, "", SanitizeEmailHeader(""))
}

func TestSanitizeEmailHeader_MultipleNewlines(t *testing.T) {
	require.Equal(t, "abc", SanitizeEmailHeader("a\r\nb\r\nc"))
}

func TestSMTPContextCancellation(t *testing.T) {
	t.Run("before-send", func(t *testing.T) {
		repo := mailtest.NewMemorySettings()
		server := mailtest.StartSMTPServer(t)
		require.NoError(t, repo.SetMultiple(context.Background(), server.Settings()))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.ErrorIs(t, NewMailer(repo, smtp.New()).SendEmail(ctx, "fixture@example.com", "fixture", "fixture"), context.Canceled)
		require.Zero(t, server.MessageCount())
	})
	t.Run("in-flight", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { _ = ln.Close() }()
		accepted := make(chan net.Conn, 1)
		go func() {
			conn, e := ln.Accept()
			if e == nil {
				accepted <- conn
			}
		}()
		repo := mailtest.NewMemorySettings()
		address, ok := ln.Addr().(*net.TCPAddr)
		require.True(t, ok)
		require.NoError(t, repo.SetMultiple(context.Background(), map[string]string{SettingKeySMTPHost: "127.0.0.1", SettingKeySMTPPort: fmt.Sprint(address.Port), SettingKeySMTPFrom: "sender@example.com", SettingKeySMTPUsername: "fixture", SettingKeySMTPPassword: "fixture"}))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- NewMailer(repo, smtp.New()).SendEmail(ctx, "fixture@example.com", "fixture", "fixture")
		}()
		conn := <-accepted
		defer func() { _ = conn.Close() }()
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("SMTP 未响应取消")
		}
	})
}
