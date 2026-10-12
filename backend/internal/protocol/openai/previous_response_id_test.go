package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClassifyOpenAIPreviousResponseIDKind(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "empty", id: " ", want: OpenAIPreviousResponseIDKindEmpty},
		{name: "response_id", id: "resp_0906a621bc423a8d0169a108637ef88197b74b0e2f37ba358f", want: OpenAIPreviousResponseIDKindResponseID},
		{name: "message_id", id: "msg_123456", want: OpenAIPreviousResponseIDKindMessageID},
		{name: "item_id", id: "item_abcdef", want: OpenAIPreviousResponseIDKindMessageID},
		{name: "unknown", id: "foo_123456", want: OpenAIPreviousResponseIDKindUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyOpenAIPreviousResponseIDKind(tc.id); got != tc.want {
				t.Fatalf("ClassifyOpenAIPreviousResponseIDKind(%q)=%q want=%q", tc.id, got, tc.want)
			}
		})
	}
}

func TestIsOpenAIPreviousResponseIDLikelyMessageID(t *testing.T) {
	if ClassifyOpenAIPreviousResponseIDKind("msg_123") != OpenAIPreviousResponseIDKindMessageID {
		t.Fatal("expected msg_123 to be identified as message id")
	}
	if ClassifyOpenAIPreviousResponseIDKind("resp_123") == OpenAIPreviousResponseIDKindMessageID {
		t.Fatal("expected resp_123 not to be identified as message id")
	}
}

func TestRemovePreviousResponseIDFromBody(t *testing.T) {
	t.Run("empty body returned as-is", func(t *testing.T) {
		require.Equal(t, []byte{}, RemovePreviousResponseIDFromBody([]byte{}))
		require.Nil(t, RemovePreviousResponseIDFromBody(nil))
	})

	t.Run("no previous_response_id field is a no-op", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5","input":"hi"}`)
		result := RemovePreviousResponseIDFromBody(body)
		require.Equal(t, body, result)
	})

	t.Run("strips previous_response_id and preserves other fields", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5","previous_response_id":"resp_abc","input":"hi"}`)
		result := RemovePreviousResponseIDFromBody(body)
		require.False(t, gjson.GetBytes(result, "previous_response_id").Exists())
		require.Equal(t, "gpt-5", gjson.GetBytes(result, "model").String())
		require.Equal(t, "hi", gjson.GetBytes(result, "input").String())
	})

	t.Run("empty-string previous_response_id is also stripped", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5","previous_response_id":""}`)
		result := RemovePreviousResponseIDFromBody(body)
		require.False(t, gjson.GetBytes(result, "previous_response_id").Exists())
	})
}
