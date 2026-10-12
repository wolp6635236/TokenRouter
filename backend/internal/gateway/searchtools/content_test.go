package searchtools

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/search/contract"
)

func TestIsOnlyWebSearchToolInBody_WebSearchType(t *testing.T) {
	require.True(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[{"type":"web_search"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_WebSearch2025Type(t *testing.T) {
	require.True(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[{"type":"web_search_20250305"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_GoogleSearchType(t *testing.T) {
	require.True(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[{"type":"google_search"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_NameWebSearch(t *testing.T) {
	require.True(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[{"name":"web_search"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_NameWebSearch2025(t *testing.T) {
	require.True(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[{"name":"web_search_20250305"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_NameGoogleSearch(t *testing.T) {
	require.True(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[{"name":"google_search"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_MultipleTools(t *testing.T) {
	require.False(t, IsOnlyWebSearchToolInBody(
		[]byte(`{"tools":[{"type":"web_search"},{"type":"text_editor"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_NoTools(t *testing.T) {
	require.False(t, IsOnlyWebSearchToolInBody([]byte(`{"model":"claude-3"}`)))
}

func TestIsOnlyWebSearchToolInBody_EmptyToolsArray(t *testing.T) {
	require.False(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[]}`)))
}

func TestIsOnlyWebSearchToolInBody_NonWebSearchTool(t *testing.T) {
	require.False(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":[{"type":"text_editor"}]}`)))
}

func TestIsOnlyWebSearchToolInBody_ToolsNotArray(t *testing.T) {
	require.False(t, IsOnlyWebSearchToolInBody([]byte(`{"tools":"web_search"}`)))
}

func TestExtractSearchQueryFromBody_StringContent(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"what is golang"}]}`
	require.Equal(t, "what is golang", ExtractSearchQueryFromBody([]byte(body)))
}

func TestExtractSearchQueryFromBody_ArrayContent(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"text","text":"search this"}]}]}`
	require.Equal(t, "search this", ExtractSearchQueryFromBody([]byte(body)))
}

func TestExtractSearchQueryFromBody_MultipleMessages(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"ok"},{"role":"user","content":"second"}]}`
	require.Equal(t, "second", ExtractSearchQueryFromBody([]byte(body)))
}

func TestExtractSearchQueryFromBody_LastMessageNotUser(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"q"},{"role":"assistant","content":"a"}]}`
	require.Equal(t, "", ExtractSearchQueryFromBody([]byte(body)))
}

func TestExtractSearchQueryFromBody_EmptyMessages(t *testing.T) {
	require.Equal(t, "", ExtractSearchQueryFromBody([]byte(`{"messages":[]}`)))
}

func TestExtractSearchQueryFromBody_NoMessages(t *testing.T) {
	require.Equal(t, "", ExtractSearchQueryFromBody([]byte(`{"model":"claude-3"}`)))
}

func TestExtractSearchQueryFromBody_ArrayContentSkipsEmptyText(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"image"},{"type":"text","text":""},{"type":"text","text":"real query"}]}]}`
	require.Equal(t, "real query", ExtractSearchQueryFromBody([]byte(body)))
}

func TestExtractSearchQueryFromBody_ArrayContentNoTextBlock(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"image","source":{}}]}]}`
	require.Equal(t, "", ExtractSearchQueryFromBody([]byte(body)))
}

func TestBuildSearchResultBlocks_WithResults(t *testing.T) {
	results := []contract.SearchResult{
		{URL: "https://a.com", Title: "A", Snippet: "snippet a", PageAge: "2 days"},
		{URL: "https://b.com", Title: "B", Snippet: "snippet b"},
	}
	blocks := BuildSearchResultBlocks(results)
	require.Len(t, blocks, 2)
	require.Equal(t, "web_search_result", blocks[0]["type"])
	require.Equal(t, "https://a.com", blocks[0]["url"])
	require.Equal(t, "snippet a", blocks[0]["page_content"])
	require.Equal(t, "2 days", blocks[0]["page_age"])

	require.Equal(t, "https://b.com", blocks[1]["url"])
	_, hasPageAge := blocks[1]["page_age"]
	require.False(t, hasPageAge)
}

func TestBuildSearchResultBlocks_Empty(t *testing.T) {
	blocks := BuildSearchResultBlocks(nil)
	require.Empty(t, blocks)
}

func TestBuildSearchResultBlocks_SnippetEmpty(t *testing.T) {
	blocks := BuildSearchResultBlocks([]contract.SearchResult{{URL: "https://x.com", Title: "X", Snippet: ""}})
	_, hasContent := blocks[0]["page_content"]
	require.False(t, hasContent)
}

func TestBuildTextSummary_WithResults(t *testing.T) {
	results := []contract.SearchResult{
		{URL: "https://a.com", Title: "A", Snippet: "desc a"},
	}
	summary := BuildTextSummary("test query", results)
	require.Contains(t, summary, "test query")
	require.Contains(t, summary, "1. **A**")
	require.Contains(t, summary, "https://a.com")
}

func TestBuildTextSummary_NoResults(t *testing.T) {
	summary := BuildTextSummary("test", nil)
	require.Contains(t, summary, "No search results found for: test")
}
