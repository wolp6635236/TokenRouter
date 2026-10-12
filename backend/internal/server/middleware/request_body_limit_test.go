package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestBodyLimit_LimitsBody(t *testing.T) {
	r := gin.New()
	r.Use(RequestBodyLimit(4))
	r.POST("/t", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		require.Error(t, err)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewBufferString("12345"))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}
