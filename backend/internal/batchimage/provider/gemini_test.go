package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
)

type fakeGeminiBatchClient struct {
	calls               []string
	uploaded            *GeminiUploadedFile
	created             *GeminiBatchJob
	got                 *GeminiBatchJob
	uploadErr           error
	createErr           error
	getErr              error
	cancelErr           error
	downloadErr         error
	deleteErr           error
	uploadedJSONL       []byte
	createdFile         string
	cancelledBatch      string
	downloadedFile      string
	downloadBody        string
	downloadContentType string
	deletedFiles        []string
}

func TestBatchImageProviderRegistry_ReturnsGeminiAPI(t *testing.T) {
	registry := newBatchProviderRegistryForTest()
	platform, ok := registry.Get(batchimage.BatchImageProviderGeminiAPI)
	require.True(t, ok)
	require.Equal(t, batchimage.BatchImageProviderGeminiAPI, platform.Name())

	must, err := registry.MustGet(batchimage.BatchImageProviderGeminiAPI)
	require.NoError(t, err)
	require.Same(t, platform, must)

	_, err = registry.MustGet("unknown_provider")
	require.ErrorIs(t, err, batchimage.ErrBatchImageInvalidProvider)
}

func TestGeminiProvider_SupportsOnlyGeminiAPIKeyWithSecret(t *testing.T) {
	platform := NewGeminiAPIBatchImageProvider(&fakeGeminiBatchClient{})

	require.True(t, platform.SupportsProvider(geminiAPIKeyProvider("sk-gemini")))
	require.False(t, platform.SupportsProvider(&providercore.Record{Platform: capability.PlatformGemini, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{}}))
	require.False(t, platform.SupportsProvider(&providercore.Record{Platform: capability.PlatformGemini, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{"api_key": "sk"}}))
	require.False(t, platform.SupportsProvider(&providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "sk"}}))
	require.False(t, platform.SupportsProvider(nil))
}

func TestGeminiProvider_MissingAPIKeyRejected(t *testing.T) {
	platform := NewGeminiAPIBatchImageProvider(&fakeGeminiBatchClient{})
	_, err := platform.Submit(context.Background(), nil, &providercore.Record{Platform: capability.PlatformGemini, Type: capability.ProviderTypeAPIKey}, validGeminiBatchInput())
	require.ErrorIs(t, err, batchimage.ErrBatchImageProviderMissingAPIKey)
}

func TestBuildGeminiBatchJSONL_WritesValidLinesAndPreservesCustomID(t *testing.T) {
	input := validGeminiBatchInput()
	input.Items = append(input.Items, batchimage.BatchImageInputItem{CustomID: "cover_002", Prompt: "Second prompt"})

	jsonl, err := BuildGeminiBatchJSONL(input)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(jsonl)), "\n")
	require.Len(t, lines, 2)
	requireJSONLLine(t, lines[0], "cover_001", "A clean product hero image")
	requireJSONLLine(t, lines[1], "cover_002", "Second prompt")
}

func TestBuildGeminiBatchJSONL_RejectsDuplicateCustomIDs(t *testing.T) {
	input := validGeminiBatchInput()
	input.Items = append(input.Items, batchimage.BatchImageInputItem{CustomID: "cover_001", Prompt: "Duplicate"})

	_, err := BuildGeminiBatchJSONL(input)
	require.ErrorIs(t, err, batchimage.ErrBatchImageProviderInvalidInput)
}

func TestBuildGeminiBatchJSONL_RejectsEmptyPrompt(t *testing.T) {
	input := validGeminiBatchInput()
	input.Items[0].Prompt = " "

	_, err := BuildGeminiBatchJSONL(input)
	require.ErrorIs(t, err, batchimage.ErrBatchImageProviderInvalidInput)
}

func TestBuildGeminiBatchJSONL_WritesReferenceImages(t *testing.T) {
	input := validGeminiBatchInput()
	input.Items[0].ReferenceImages = []batchimage.BatchImageReference{
		{MimeType: "image/webp", Data: []byte("webp-bytes")},
		{MimeType: "image/jpeg", FileURI: "gs://bucket/refs/style.jpg"},
	}

	jsonl, err := BuildGeminiBatchJSONL(input)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(jsonl)), "\n")
	require.Len(t, lines, 1)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &got))
	request := testassert.MustType[map[string]any](got["request"])
	contents := testassert.MustType[[]any](request["contents"])
	parts := testassert.MustType[[]any](testassert.MustType[map[string]any](contents[0])["parts"])
	require.Len(t, parts, 3)
	require.Equal(t, "A clean product hero image", testassert.MustType[map[string]any](parts[0])["text"])
	inlineData := testassert.MustType[map[string]any](testassert.MustType[map[string]any](parts[1])["inlineData"])
	require.Equal(t, "image/webp", inlineData["mimeType"])
	require.Equal(t, "d2VicC1ieXRlcw==", inlineData["data"])
	fileData := testassert.MustType[map[string]any](testassert.MustType[map[string]any](parts[2])["fileData"])
	require.Equal(t, "image/jpeg", fileData["mimeType"])
	require.Equal(t, "gs://bucket/refs/style.jpg", fileData["fileUri"])
}

func TestGeminiProvider_SubmitUploadsJSONLThenCreatesBatch(t *testing.T) {
	client := &fakeGeminiBatchClient{
		uploaded: &GeminiUploadedFile{Name: "files/input-jsonl"},
		created:  &GeminiBatchJob{Name: "batches/job-123", State: "JOB_STATE_PENDING"},
	}
	platform := NewGeminiAPIBatchImageProvider(client)

	got, err := platform.Submit(context.Background(), &batchimage.BatchImageJob{BatchID: "imgbatch_123", Model: "gemini-3.1-flash-image"}, geminiAPIKeyProvider("sk-secret"), validGeminiBatchInput())
	require.NoError(t, err)
	require.Equal(t, []string{"upload", "create"}, client.calls)
	require.Equal(t, "files/input-jsonl", got.ProviderInputRef)
	require.Equal(t, "batches/job-123", got.ProviderJobName)
	require.Empty(t, got.ProviderOutputRef)
	require.NotContains(t, got.ProviderInputRef, "A clean product hero image")
	require.NotContains(t, string(client.uploadedJSONL), "sk-secret")
}

func TestGeminiProvider_GetMapsStates(t *testing.T) {
	tests := []struct {
		name      string
		job       *GeminiBatchJob
		wantState batchimage.BatchProviderInternalState
		wantDone  bool
		wantRef   string
		wantCode  string
	}{
		{name: "running", job: &GeminiBatchJob{Name: "batches/1", State: "JOB_STATE_RUNNING"}, wantState: batchimage.BatchProviderStateRunning},
		{name: "succeeded_dest_fileName", job: &GeminiBatchJob{Name: "batches/1", State: "JOB_STATE_SUCCEEDED", Dest: &GeminiBatchDest{FileName: "files/out"}}, wantState: batchimage.BatchProviderStateSucceeded, wantDone: true, wantRef: "files/out"},
		{name: "failed", job: &GeminiBatchJob{Name: "batches/1", State: "JOB_STATE_FAILED", Error: &GeminiBatchError{Code: "BAD_PROMPT", Message: "bad prompt"}}, wantState: batchimage.BatchProviderStateFailed, wantDone: true, wantCode: "BAD_PROMPT"},
		{name: "cancelled", job: &GeminiBatchJob{Name: "batches/1", State: "JOB_STATE_CANCELLED"}, wantState: batchimage.BatchProviderStateCancelled, wantDone: true, wantCode: "GEMINI_BATCH_CANCELLED"},
		{name: "expired", job: &GeminiBatchJob{Name: "batches/1", State: "JOB_STATE_EXPIRED"}, wantState: batchimage.BatchProviderStateExpired, wantDone: true, wantCode: "GEMINI_BATCH_EXPIRED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := NewGeminiAPIBatchImageProvider(&fakeGeminiBatchClient{got: tt.job})
			got, err := platform.Get(context.Background(), jobWithProviderName("batches/1"), geminiAPIKeyProvider("sk-secret"))
			require.NoError(t, err)
			require.Equal(t, tt.wantState, got.InternalState)
			require.Equal(t, tt.wantDone, got.Done)
			require.Equal(t, tt.wantRef, got.ProviderOutputRef)
			require.Equal(t, tt.wantCode, got.ErrorCode)
			require.NotContains(t, got.ErrorMessage, "sk-secret")
		})
	}
}

func TestGeminiProvider_GetExtractsResponsesFileReference(t *testing.T) {
	platform := NewGeminiAPIBatchImageProvider(&fakeGeminiBatchClient{
		got: &GeminiBatchJob{
			Name:     "batches/1",
			State:    "JOB_STATE_SUCCEEDED",
			Response: &GeminiBatchResponse{ResponsesFile: "files/responses-jsonl"},
		},
	})

	got, err := platform.Get(context.Background(), jobWithProviderName("batches/1"), geminiAPIKeyProvider("sk-secret"))
	require.NoError(t, err)
	require.Equal(t, batchimage.BatchProviderStateSucceeded, got.InternalState)
	require.Equal(t, "files/responses-jsonl", got.ProviderOutputRef)
}

func TestGeminiProvider_GetRejectsInlineResultShape(t *testing.T) {
	platform := NewGeminiAPIBatchImageProvider(&fakeGeminiBatchClient{
		got: &GeminiBatchJob{
			Name:     "batches/1",
			State:    "JOB_STATE_SUCCEEDED",
			Response: &GeminiBatchResponse{InlinedResponses: []any{map[string]any{"response": "large"}}},
		},
	})

	_, err := platform.Get(context.Background(), jobWithProviderName("batches/1"), geminiAPIKeyProvider("sk-secret"))
	require.ErrorIs(t, err, batchimage.ErrBatchImageProviderInlineResultUnsupported)
}

func TestGeminiProvider_OpenResultStreamsResultFile(t *testing.T) {
	client := &fakeGeminiBatchClient{downloadBody: "line1\n", downloadContentType: "application/jsonl"}
	platform := NewGeminiAPIBatchImageProvider(client)

	outputRef := "files/output-jsonl"
	r, contentType, err := platform.OpenResult(context.Background(), &batchimage.BatchImageJob{ProviderOutputRef: &outputRef}, geminiAPIKeyProvider("sk-secret"))
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()

	body, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "line1\n", string(body))
	require.Equal(t, "application/jsonl", contentType)
	require.Equal(t, "files/output-jsonl", client.downloadedFile)
}

func TestGeminiProvider_CancelCallsClient(t *testing.T) {
	client := &fakeGeminiBatchClient{}
	platform := NewGeminiAPIBatchImageProvider(client)

	require.NoError(t, platform.Cancel(context.Background(), jobWithProviderName("batches/1"), geminiAPIKeyProvider("sk-secret")))
	require.Equal(t, "batches/1", client.cancelledBatch)
}

func TestGeminiProvider_CleanupDeletesRefsOnlyWhenPresent(t *testing.T) {
	inputRef := "files/input"
	outputRef := "files/output"
	client := &fakeGeminiBatchClient{}
	platform := NewGeminiAPIBatchImageProvider(client)

	err := platform.Cleanup(context.Background(), &batchimage.BatchImageJob{ProviderInputRef: &inputRef, ProviderOutputRef: &outputRef}, geminiAPIKeyProvider("sk-secret"), batchimage.CleanupTargetAll)
	require.NoError(t, err)
	require.Equal(t, []string{"files/input", "files/output"}, client.deletedFiles)

	err = platform.Cleanup(context.Background(), &batchimage.BatchImageJob{}, geminiAPIKeyProvider("sk-secret"), batchimage.CleanupTargetAll)
	require.NoError(t, err)
	require.Equal(t, []string{"files/input", "files/output"}, client.deletedFiles)
}

func TestGeminiProvider_ErrorsDoNotExposeAPIKey(t *testing.T) {
	apiKey := "sk-top-secret"
	client := &fakeGeminiBatchClient{uploadErr: &GeminiAPIError{StatusCode: 401, Message: "upstream body should be hidden " + apiKey}}
	platform := NewGeminiAPIBatchImageProvider(client)

	_, err := platform.Submit(context.Background(), nil, geminiAPIKeyProvider(apiKey), validGeminiBatchInput())
	require.Error(t, err)
	require.Equal(t, "GEMINI_AUTH_FAILED", apperror.Reason(err))
	require.NotContains(t, err.Error(), apiKey)
}

func TestGeminiProvider_MetadataDoesNotStoreImageBytesOrBase64(t *testing.T) {
	client := &fakeGeminiBatchClient{
		uploaded: &GeminiUploadedFile{Name: "files/input-jsonl"},
		created:  &GeminiBatchJob{Name: "batches/job-123", State: "JOB_STATE_PENDING"},
	}
	platform := NewGeminiAPIBatchImageProvider(client)

	got, err := platform.Submit(context.Background(), nil, geminiAPIKeyProvider("sk-secret"), validGeminiBatchInput())
	require.NoError(t, err)
	require.NotContains(t, got.ProviderJobName, "base64")
	require.NotContains(t, got.ProviderInputRef, "base64")
	require.NotContains(t, got.ProviderOutputRef, "base64")
	require.NotContains(t, got.ProviderJobName+got.ProviderInputRef+got.ProviderOutputRef, "iVBOR")
	require.NotContains(t, got.ProviderJobName+got.ProviderInputRef+got.ProviderOutputRef, "A clean product hero image")
}

func requireJSONLLine(t *testing.T, line, wantKey, wantPrompt string) {
	t.Helper()
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &got))
	require.Equal(t, wantKey, got["key"])
	request := testassert.MustType[map[string]any](got["request"])
	config := testassert.MustType[map[string]any](request["generationConfig"])
	require.Equal(t, []any{"TEXT", "IMAGE"}, config["responseModalities"])
	contents := testassert.MustType[[]any](request["contents"])
	parts := testassert.MustType[[]any](testassert.MustType[map[string]any](contents[0])["parts"])
	require.Equal(t, wantPrompt, testassert.MustType[map[string]any](parts[0])["text"])
}

func validGeminiBatchInput() batchimage.BatchImageInput {
	return batchimage.BatchImageInput{
		BatchID:     "imgbatch_123",
		Model:       "gemini-3.1-flash-image",
		DisplayName: "test batch",
		Items: []batchimage.BatchImageInputItem{{
			CustomID: "cover_001",
			Prompt:   "A clean product hero image",
		}},
	}
}

func geminiAPIKeyProvider(apiKey string) *providercore.Record {
	return &providercore.Record{
		Platform:    capability.PlatformGemini,
		Type:        capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": apiKey},
	}
}

func jobWithProviderName(name string) *batchimage.BatchImageJob {
	return &batchimage.BatchImageJob{ProviderJobName: &name}
}

func (f *fakeGeminiBatchClient) UploadJSONL(_ context.Context, apiKey string, _ string, r io.Reader) (*GeminiUploadedFile, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("missing api key")
	}
	f.calls = append(f.calls, "upload")
	f.uploadedJSONL, _ = io.ReadAll(r)
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	if f.uploaded != nil {
		return f.uploaded, nil
	}
	return &GeminiUploadedFile{Name: "files/input-jsonl"}, nil
}

func (f *fakeGeminiBatchClient) CreateBatch(_ context.Context, _ string, _ string, fileName string, _ string) (*GeminiBatchJob, error) {
	f.calls = append(f.calls, "create")
	f.createdFile = fileName
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.created != nil {
		return f.created, nil
	}
	return &GeminiBatchJob{Name: "batches/job-123", State: "JOB_STATE_PENDING"}, nil
}

func (f *fakeGeminiBatchClient) GetBatch(_ context.Context, _ string, _ string) (*GeminiBatchJob, error) {
	f.calls = append(f.calls, "get")
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.got, nil
}

func (f *fakeGeminiBatchClient) CancelBatch(_ context.Context, _ string, batchName string) error {
	f.calls = append(f.calls, "cancel")
	f.cancelledBatch = batchName
	return f.cancelErr
}

func (f *fakeGeminiBatchClient) DownloadFile(_ context.Context, _ string, fileName string) (io.ReadCloser, string, error) {
	f.calls = append(f.calls, "download")
	f.downloadedFile = fileName
	if f.downloadErr != nil {
		return nil, "", f.downloadErr
	}
	contentType := f.downloadContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return io.NopCloser(bytes.NewBufferString(f.downloadBody)), contentType, nil
}

func (f *fakeGeminiBatchClient) DeleteFile(_ context.Context, _ string, fileName string) error {
	f.calls = append(f.calls, "delete")
	f.deletedFiles = append(f.deletedFiles, fileName)
	return f.deleteErr
}
