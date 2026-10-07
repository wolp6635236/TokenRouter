package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/stretchr/testify/require"
)

type failTestEventWriter struct {
	TestStreamWriter
	err error
}

func (w failTestEventWriter) Write([]byte) (int, error) { return 0, w.err }

// TestProviderTestPropagatesEventWriteFailure 验证流事件无法写出时，测试不能把已经丢失的输出报告为执行成功。
func TestProviderTestPropagatesEventWriteFailure(t *testing.T) {
	failed := errors.New("forced test event write failure")
	writer := failTestEventWriter{TestStreamWriter: httptest.NewRecorder(), err: failed}
	run := provideradapter.NewTestRun(t.Context(), make(http.Header), NewTestEventSink(writer))
	defer run.Cancel()
	err := (provideradapter.TestStreamOutput{}).Responses(run, strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"fixture\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"))
	err = run.Result(err)
	require.ErrorIs(t, err, failed)
}
