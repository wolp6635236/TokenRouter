package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
	"github.com/TokenFlux/TokenRouter/internal/site"
	"github.com/stretchr/testify/require"
)

// TestLocalizedPageFiles 覆盖正文实际语言、图片目录、公共回退及目录外符号链接。
func TestLocalizedPageFiles(t *testing.T) {
	root := t.TempDir()
	files := New(root)
	dir := filepath.Join(root, "pages")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "guide", "en"), 0o755))
	for path, body := range map[string]string{"guide.md": "原文", "guide/en.md": "English", "guide/en/logo.png": "translated", "guide/shared.png": "shared"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, path), []byte(body), 0o600))
	}
	ctx := context.Background()
	body, language, err := files.ReadLocalizedMarkdown(ctx, "guide", "en-US")
	require.NoError(t, err)
	require.Equal(t, "English", string(body))
	require.Equal(t, "en", language)
	body, language, err = files.ReadLocalizedMarkdown(ctx, "guide", "zh")
	require.NoError(t, err)
	require.Equal(t, "原文", string(body))
	require.Empty(t, language)
	selected := locale.WithLanguage(ctx, "en")
	image, err := files.ImagePath(selected, "guide", "logo.png")
	require.NoError(t, err)
	require.Contains(t, image, "/guide/en/logo.png")
	image, err = files.ImagePath(selected, "guide", "shared.png")
	require.NoError(t, err)
	require.Contains(t, image, "/guide/shared.png")
	external := filepath.Join(t.TempDir(), "external.md")
	require.NoError(t, os.WriteFile(external, []byte("private"), 0o600))
	require.NoError(t, os.Symlink(external, filepath.Join(dir, "guide", "zh-Hans.md")))
	body, language, err = files.ReadLocalizedMarkdown(ctx, "guide", "zh")
	require.NoError(t, err)
	require.Equal(t, "原文", string(body))
	require.Empty(t, language)
	_, err = files.ImagePath(selected, "guide", "../guide.md")
	require.ErrorIs(t, err, site.ErrPageNotFound)
}
