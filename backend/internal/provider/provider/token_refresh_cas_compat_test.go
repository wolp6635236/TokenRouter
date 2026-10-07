package provider

import (
	"context"
	"reflect"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// UpdateOAuthCredentialsIfUnchanged 模拟条件写入，记录次数并支持注入失败和提交后取消。
func (r *tokenRefreshProviderRepo) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, version provider.CredentialVersion, credentials map[string]any) (bool, error) {
	current := r.providersByID[version.ID]
	if current == nil {
		return false, nil
	}
	expected := current.Credentials
	if expected == nil {
		expected = map[string]any{}
	}
	if current.ID != version.ID || current.Platform != version.Platform || current.Type != version.Type || current.Status != version.Status || !reflect.DeepEqual(current.ProxyID, version.ProxyID) || !reflect.DeepEqual(expected, version.Credentials) {
		return false, nil
	}
	err := r.UpdateCredentials(ctx, version.ID, credentials)
	return err == nil, err
}
