package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"

	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// failingAdminService 嵌入 managementCredentialFixture，可配置 UpdateProvider 在指定 ID 时失败。
type failingAdminService struct {
	*managementCredentialFixture
	failOnProviderID int64
	updateCallCount  atomic.Int64
}

func (f *failingAdminService) UpdateProvider(ctx context.Context, id int64, input *provider.UpdateProviderInput) (*provider.Record, error) {
	f.updateCallCount.Add(1)
	if id == f.failOnProviderID {
		return nil, errors.New("database error")
	}
	return f.managementCredentialFixture.UpdateProvider(ctx, id, input)
}

func setupProviderHandlerWithService(adminSvc ProviderManagement) (*gin.Engine, *ManagementHandler) {
	router := gin.New()
	handler := NewManagementHandler(adminSvc, ManagementOptions{Batch: provider.NewManagementBatch(adminSvc, nil)})
	router.POST("/api/v1/admin/providers/batch-update-credentials", handler.BatchUpdateCredentials)
	return router, handler
}

func TestBatchUpdateCredentials_AllSuccess(t *testing.T) {
	svc := &failingAdminService{managementCredentialFixture: &managementCredentialFixture{}}
	router, _ := setupProviderHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		ProviderIDs: []int64{1, 2, 3},
		Field:       "account_uuid",
		Value:       "test-uuid",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/providers/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "全部成功时应返回 200")
	require.Equal(t, int64(3), svc.updateCallCount.Load(), "应调用 3 次 UpdateProvider")
}

func TestBatchUpdateCredentials_PartialFailure(t *testing.T) {
	// 让第 2 个提供商（ID=2）更新时失败
	svc := &failingAdminService{
		managementCredentialFixture: &managementCredentialFixture{},
		failOnProviderID:            2,
	}
	router, _ := setupProviderHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		ProviderIDs: []int64{1, 2, 3},
		Field:       "org_uuid",
		Value:       "test-org",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/providers/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// 实现采用"部分成功"模式：总是返回 200 + 成功/失败明细
	require.Equal(t, http.StatusOK, w.Code, "批量更新返回 200 + 成功/失败明细")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := testassert.MustType[map[string]any](resp["data"])
	require.Equal(t, float64(2), data["success"], "应有 2 个成功")
	require.Equal(t, float64(1), data["failed"], "应有 1 个失败")

	// 所有 3 个提供商都会被尝试更新（非 fail-fast）
	require.Equal(t, int64(3), svc.updateCallCount.Load(),
		"应调用 3 次 UpdateProvider（逐个尝试，失败后继续）")
}

func TestBatchUpdateCredentials_FirstProviderNotFound(t *testing.T) {
	// GetProvider 在 managementCredentialFixture 中总是成功的，需要创建一个 GetProvider 会失败的 stub
	svc := &getProviderFailingService{
		managementCredentialFixture: &managementCredentialFixture{},
		failOnProviderID:            1,
	}
	router, _ := setupProviderHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		ProviderIDs: []int64{1, 2, 3},
		Field:       "account_uuid",
		Value:       "test",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/providers/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "第一阶段验证失败应返回 404")
}

// getProviderFailingService 模拟 GetProvider 在特定 ID 时返回 not found。
type getProviderFailingService struct {
	*managementCredentialFixture
	failOnProviderID int64
}

func (f *getProviderFailingService) GetProvider(ctx context.Context, id int64) (*provider.Record, error) {
	if id == f.failOnProviderID {
		return nil, errors.New("not found")
	}
	return f.managementCredentialFixture.GetProvider(ctx, id)
}

func TestBatchUpdateCredentials_InterceptWarmupRequests_NonBool(t *testing.T) {
	svc := &failingAdminService{managementCredentialFixture: &managementCredentialFixture{}}
	router, _ := setupProviderHandlerWithService(svc)

	// intercept_warmup_requests 传入非 bool 类型（string），应返回 400
	body, _ := json.Marshal(map[string]any{
		"provider_ids": []int64{1},
		"field":        "intercept_warmup_requests",
		"value":        "not-a-bool",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/providers/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code,
		"intercept_warmup_requests 传入非 bool 值应返回 400")
}

func TestBatchUpdateCredentials_InterceptWarmupRequests_ValidBool(t *testing.T) {
	svc := &failingAdminService{managementCredentialFixture: &managementCredentialFixture{}}
	router, _ := setupProviderHandlerWithService(svc)

	body, _ := json.Marshal(map[string]any{
		"provider_ids": []int64{1},
		"field":        "intercept_warmup_requests",
		"value":        true,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/providers/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"intercept_warmup_requests 传入合法 bool 值应返回 200")
}

func TestBatchUpdateCredentials_AccountUUID_NonString(t *testing.T) {
	svc := &failingAdminService{managementCredentialFixture: &managementCredentialFixture{}}
	router, _ := setupProviderHandlerWithService(svc)

	// account_uuid 传入非 string 类型（number），应返回 400
	body, _ := json.Marshal(map[string]any{
		"provider_ids": []int64{1},
		"field":        "account_uuid",
		"value":        12345,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/providers/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code,
		"account_uuid 传入非 string 值应返回 400")
}

func TestBatchUpdateCredentials_AccountUUID_NullValue(t *testing.T) {
	svc := &failingAdminService{managementCredentialFixture: &managementCredentialFixture{}}
	router, _ := setupProviderHandlerWithService(svc)

	// account_uuid 传入 null（设置为空），应正常通过
	body, _ := json.Marshal(map[string]any{
		"provider_ids": []int64{1},
		"field":        "account_uuid",
		"value":        nil,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/providers/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"account_uuid 传入 null 应返回 200")
}

// 凭据批量测试提供预读和更新响应，通过用例接口注入错误。
type managementCredentialFixture struct{ ProviderManagement }

func (s *managementCredentialFixture) GetProvider(_ context.Context, id int64) (*provider.Record, error) {
	return &provider.Record{ID: id, Name: "provider", Platform: provider.PlatformAnthropic, Type: provider.ProviderTypeOAuth, Status: provider.StatusActive}, nil
}

func (s *managementCredentialFixture) UpdateProvider(_ context.Context, id int64, input *provider.UpdateProviderInput) (*provider.Record, error) {
	return &provider.Record{ID: id, Name: input.Name, Platform: provider.PlatformAnthropic, Type: input.Type, Status: provider.StatusActive, Credentials: input.Credentials}, nil
}
