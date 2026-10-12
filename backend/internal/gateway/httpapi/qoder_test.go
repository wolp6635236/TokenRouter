package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/server/middleware"
)

func TestRequestBodyLimitTooLarge(t *testing.T) {
	limit := int64(16)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(limit))
	router.POST("/test", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			if maxErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{
					"error": BodyTooLargeMessage(maxErr.Limit),
				})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "read_failed",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	payload := bytes.Repeat([]byte("a"), int(limit+1))
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(payload))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Body.String(), BodyTooLargeMessage(limit))
}

func TestParseOpenAICompatibleStream(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStream bool
		wantOK     bool
	}{
		{name: "missing", body: `{"model":"gpt-5"}`, wantStream: false, wantOK: true},
		{name: "true", body: `{"model":"gpt-5","stream":true}`, wantStream: true, wantOK: true},
		{name: "false", body: `{"model":"gpt-5","stream":false}`, wantStream: false, wantOK: true},
		{name: "string", body: `{"model":"gpt-5","stream":"true"}`, wantStream: false, wantOK: false},
		{name: "number", body: `{"model":"gpt-5","stream":1}`, wantStream: false, wantOK: false},
		{name: "null", body: `{"model":"gpt-5","stream":null}`, wantStream: false, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStream, gotOK := ParseOpenAICompatibleStream([]byte(tt.body))

			require.Equal(t, tt.wantStream, gotStream)
			require.Equal(t, tt.wantOK, gotOK)
		})
	}
}
