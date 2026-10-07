package batchimage_test

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/stretchr/testify/require"
)

func testBatchImageOwner() batchimage.BatchImageOwner {
	groupID := int64(7)
	return batchimage.BatchImageOwner{UserID: 11, APIKeyID: 22, GroupID: &groupID}
}

func requireBatchImagePublicJSONHasNoInternals(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{
		"provider_job_name",
		"provider_input_ref",
		"provider_output_ref",
		"gcs_input_uri",
		"gcs_output_uri",
		"provider_id",
		"service_account",
		"api_key",
		"download_url",
		"providers/",
		"files/",
		"gs://",
	} {
		require.NotContains(t, body, forbidden)
	}
}
