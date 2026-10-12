package settings_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/settings/composite"
	settingskit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
)

func TestBuildSystemSettingsUpdatesCreativeModelSettings(t *testing.T) {
	svc := settingskit.NewComposite(&settingUpdateRepoStub{}, nil)
	updates, err := composite.Prepare(context.Background(), &composite.Snapshot{
		CreativeModelSettings: []creative.CreativeModelSetting{{
			GroupID:    3,
			Model:      "gpt-image-2",
			Operations: []string{creative.CreativeOperationInpaint, creative.CreativeOperationGenerate},
		}},
	}, svc.Prepare)
	require.NoError(t, err)
	var persisted []creative.CreativeModelSetting
	require.NoError(t, json.Unmarshal([]byte(updates[creative.SettingKeyCreativeModelSettings]), &persisted))
	require.Equal(t, []creative.CreativeModelSetting{{
		GroupID:    3,
		Model:      "gpt-image-2",
		Operations: []string{creative.CreativeOperationGenerate, creative.CreativeOperationInpaint},
	}}, persisted)

	_, err = composite.Prepare(context.Background(), &composite.Snapshot{
		CreativeModelSettings: []creative.CreativeModelSetting{{GroupID: 3, Model: "gpt-image-2"}},
	}, svc.Prepare)
	require.Error(t, err)
}
