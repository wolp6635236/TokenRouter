package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/stretchr/testify/require"
)

// creativeImageResponseBody 记录生图响应关闭时点，验证下载前已释放连接。
type creativeImageResponseBody struct {
	io.Reader
	closed bool
}

func (b *creativeImageResponseBody) Close() error {
	b.closed = true
	return nil
}

// TestOpenAIImageURLResults 覆盖两种变体及三种操作，下载重试不会重新生图。
func TestOpenAIImageURLResults(t *testing.T) {
	for _, model := range []string{"gpt-image-2.5-flare", "gpt-image-2.5-sunburst"} {
		for _, operation := range []string{"generate", "edit", "inpaint"} {
			t.Run(model+"/"+operation, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					body := &creativeImageResponseBody{Reader: strings.NewReader(`{"data":[{"url":"https://cdn.example.com/image.png?sig=test"}]}`)}
					generations, downloads := 0, 0
					target := &Target{OpenAI: &OpenAIOptions{
						Token:        func(context.Context) (string, error) { return "token", nil },
						URL:          func(endpoint string) (string, error) { return "https://relay.example.com" + endpoint, nil },
						Prepare:      func(req *http.Request) *http.Request { return req },
						AuthHeaders:  func(context.Context, string) (http.Header, error) { return http.Header{}, nil },
						ApplyHeaders: func(http.Header) {},
						Do: func(req *http.Request) (*http.Response, error) {
							generations++
							require.Equal(t, http.MethodPost, req.Method)
							return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
						},
						FetchImage: func(ctx context.Context, raw string) (string, error) {
							downloads++
							require.True(t, body.closed)
							require.Equal(t, "https://cdn.example.com/image.png?sig=test", raw)
							if downloads == 1 {
								return "", errors.New("temporary download failure")
							}
							return base64.StdEncoding.EncodeToString([]byte("image bytes")), nil
						},
					}}
					outputs, err := target.ExecuteOpenAI(context.Background(), creative.CreativeRun{Operation: operation, ImageSize: "1K"}, creative.CreativeRunPayload{Prompt: "test"}, model)
					require.NoError(t, err)
					require.Len(t, outputs, 1)
					require.Equal(t, []byte("image bytes"), outputs[0].Bytes)
					require.Equal(t, 1, generations)
					require.Equal(t, 2, downloads)
				})
			})
		}
	}
}

// TestOpenAIImageResultFailures 验证结果错误独立分类且不泄露下载地址。
func TestOpenAIImageResultFailures(t *testing.T) {
	for _, test := range []struct {
		name, body, code string
		cancel           bool
		wantDownloads    int
	}{
		{name: "download exhausted", body: `{"data":[{"url":"https://cdn.example.com/private?sig=secret"}]}`, code: "IMAGE_DOWNLOAD_FAILED", wantDownloads: 3},
		{name: "cancelled download", body: `{"data":[{"url":"https://cdn.example.com/private?sig=secret"}]}`, code: "IMAGE_DOWNLOAD_FAILED", cancel: true, wantDownloads: 1},
		{name: "empty output", body: `{"data":[]}`, code: "INVALID_IMAGE_RESPONSE"},
		{name: "invalid base64", body: `{"data":[{"b64_json":"%%%"}]}`, code: "INVALID_IMAGE_RESPONSE"},
		{name: "invalid json", body: `not json`, code: "INVALID_IMAGE_RESPONSE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				downloads := 0
				target := &Target{OpenAI: &OpenAIOptions{FetchImage: func(context.Context, string) (string, error) {
					downloads++
					if test.cancel {
						cancel()
					}
					return "", errors.New("failed https://cdn.example.com/private?sig=secret")
				}}}
				_, err := target.parseOpenAIImageOutputs(ctx, []byte(test.body))
				var resultErr *creative.CreativeUpstreamError
				require.ErrorAs(t, err, &resultErr)
				require.Equal(t, test.code, resultErr.Code)
				require.False(t, creative.IsRetryableCreativeError(err))
				require.NotContains(t, err.Error(), "secret")
				require.Equal(t, test.wantDownloads, downloads)
			})
		})
	}
}

// TestOpenAIImageBase64Preferred 验证已有 Base64 时无需下载 URL。
func TestOpenAIImageBase64Preferred(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		target := &Target{OpenAI: &OpenAIOptions{FetchImage: func(context.Context, string) (string, error) {
			t.Fatal("unexpected image download")
			return "", nil
		}}}
		outputs, err := target.parseOpenAIImageOutputs(context.Background(), []byte(`{"data":[{"url":"https://cdn.example.com/image.png","b64_json":"aW1hZ2U="}]}`))
		require.NoError(t, err)
		require.Equal(t, []byte("image"), outputs[0].Bytes)
	})
}
