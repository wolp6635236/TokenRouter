package provider_test

import (
	"context"
	"reflect"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// UpdateOAuthCredentialsIfUnchanged 在凭据版本匹配时调用测试仓库的 UpdateCredentials。
func (r *tokenRefreshProviderRepo) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, version provider.CredentialVersion, credentials map[string]any) (bool, error) {
	current := r.providersByID[version.ID]
	if current == nil {
		return false, nil
	}
	expected := current.Record.Credentials
	if expected == nil {
		expected = map[string]any{}
	}
	if current.Record.ID != version.ID || current.Record.Platform != version.Platform || current.Record.Type != version.Type || current.Record.Status != version.Status || !reflect.DeepEqual(current.Record.ProxyID, version.ProxyID) || !reflect.DeepEqual(expected, version.Credentials) {
		return false, nil
	}
	err := r.UpdateCredentials(ctx, version.ID, credentials)
	return err == nil, err
}
