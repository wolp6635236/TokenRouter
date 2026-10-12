package usageview

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNormalizedUsageRejectsUnknownMode(t *testing.T) {
	err := ValidateNormalizedUsage(&UpstreamUsageInfo{Provider: "test", Mode: "window"})
	require.Error(t, err)
	err = ValidateNormalizedUsage(&UpstreamUsageInfo{Provider: "test", Mode: "balance"})
	require.Error(t, err)
}
