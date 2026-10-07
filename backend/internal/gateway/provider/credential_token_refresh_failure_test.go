package provider_test

import (
	"reflect"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// refreshFailureMatchesFixture 比较身份字段和凭据版本，nil 凭据按空对象比较。
func refreshFailureMatchesFixture(value *gatewayprovider.ExecutionProvider, version provider.RefreshFailureVersion) bool {
	if value == nil {
		return false
	}
	credentials := value.Record.Credentials
	if credentials == nil {
		credentials = map[string]any{}
	}
	return value.Record.ID == version.ID && value.Record.Platform == version.Platform && value.Record.Type == version.Type && value.Record.Status == version.Status && value.Record.Schedulable == version.Schedulable && reflect.DeepEqual(credentials, version.Credentials) && reflect.DeepEqual(value.Record.ProxyID, version.ProxyID)
}
