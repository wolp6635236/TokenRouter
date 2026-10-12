package forward

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendRawJSON_EmptyObjectPlaceholder(t *testing.T) {
	t.Parallel()

	fragment := `{"query":"status"}`
	require.JSONEq(t, fragment, string(AppendRawJSON(json.RawMessage("{ \n\t }"), fragment)))
	require.Equal(t, `{"existing":true}{"query":"status"}`, string(AppendRawJSON(json.RawMessage(`{"existing":true}`), fragment)))
}
