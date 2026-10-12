package provider

import (
	"context"
	"fmt"
)

// ManagementProviderMissing 保留批量预验证阶段的 404 展示，原始原因供内部错误链读取。
type ManagementProviderMissing struct {
	ID    int64
	Cause error
}

type ManagementPatchItem struct {
	ProviderID int64
	Success    bool
	Error      string
}
type ManagementPatchResult struct {
	Success, Failed       int
	SuccessIDs, FailedIDs []int64
	Results               []ManagementPatchItem
}

func (e *ManagementProviderMissing) Error() string { return fmt.Sprintf("Provider %d not found", e.ID) }
func (e *ManagementProviderMissing) Unwrap() error { return e.Cause }

// ValidateCredentialFieldValue 只允许原管理接口的三个字段和既有值类型。
func ValidateCredentialFieldValue(field string, value any) error {
	switch field {
	case "intercept_warmup_requests":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("intercept_warmup_requests must be boolean")
		}
	case "account_uuid", "org_uuid":
		if value != nil {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be string or null", field)
			}
		}
	default:
		return fmt.Errorf("unsupported credential field: %s", field)
	}
	return nil
}

// PatchCredentials 保留先完整预验证、再逐项提交；只发送明确字段，锁内继承其他最新凭据与配置。
func (s *ManagementBatch) PatchCredentials(ctx context.Context, ids []int64, field string, value any) (*ManagementPatchResult, error) {
	if err := ValidateCredentialFieldValue(field, value); err != nil {
		return nil, err
	}
	for _, id := range ids {
		v, err := s.store.GetProvider(ctx, id)
		if err != nil || v == nil {
			return nil, &ManagementProviderMissing{ID: id, Cause: err}
		}
	}
	result := &ManagementPatchResult{SuccessIDs: make([]int64, 0, len(ids)), FailedIDs: make([]int64, 0, len(ids)), Results: make([]ManagementPatchItem, 0, len(ids))}
	for _, id := range ids {
		_, err := s.store.UpdateProvider(ctx, id, &UpdateProviderInput{Credentials: map[string]any{field: value}, PatchCredentials: true})
		if err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, id)
			result.Results = append(result.Results, ManagementPatchItem{ProviderID: id, Error: err.Error()})
			continue
		}
		result.Success++
		result.SuccessIDs = append(result.SuccessIDs, id)
		result.Results = append(result.Results, ManagementPatchItem{ProviderID: id, Success: true})
	}
	return result, nil
}
