package provider

import (
	"context"
	"reflect"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// UpdateOAuthCredentialsIfUnchanged 条件写入替身核对当前记录的身份和凭据版本，匹配后复用凭据更新逻辑。
func (r *crsStaleCredentialRepo) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, v provider.CredentialVersion, credentials map[string]any) (bool, error) {
	c := r.current
	if c.ID != v.ID || c.Platform != v.Platform || c.Type != v.Type || c.Status != v.Status || !reflect.DeepEqual(c.Credentials, v.Credentials) || !reflect.DeepEqual(c.ProxyID, v.ProxyID) {
		return false, nil
	}
	return true, r.UpdateCredentials(ctx, v.ID, credentials)
}
