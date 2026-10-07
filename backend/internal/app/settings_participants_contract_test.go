package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/payment"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	settingsdto "github.com/TokenFlux/TokenRouter/internal/settings/httpapi/dto"
	"github.com/stretchr/testify/require"
)

// TestSettingsFieldOwnership 检查 app 登记覆盖全部扁平输入，缺少字段登记时失败。
func TestSettingsFieldOwnership(t *testing.T) {
	defaults := scheduler.DefaultAdminSettingsDefaults()
	participants := staticSettingsParticipants(&payment.Runtime{}, nil, &defaults, provideGatewayAdminRules())
	_, err := settings.NewRegistry(participants...)
	require.NoError(t, err)
	owners := map[string]string{}
	for _, participant := range participants {
		encoded, err := json.Marshal(struct {
			Module       string
			Fields, Keys []string
		}{participant.Module, participant.Fields, participant.Keys})
		require.NoError(t, err)
		t.Logf("TEST_PARTICIPANT %s", encoded)
		for _, field := range participant.Fields {
			require.Empty(t, owners[field], "字段 %s 有多个所有者", field)
			owners[field] = participant.Module
		}
	}
	input := reflect.TypeFor[settingsdto.UpdateSettingsRequest]()
	count := 0
	for i := 0; i < input.NumField(); i++ {
		field := input.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		require.Contains(t, owners, name, "输入字段 %s 没有静态所有者", field.Name)
		delete(owners, name)
		count++
	}
	require.Equal(t, 282, count, "HTTP 字段变化需要同步字段登记")
	require.Empty(t, owners, "参与者不应声明不存在的输入字段")
}
