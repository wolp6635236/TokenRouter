import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import postcss from 'postcss'
import { describe, expect, it } from 'vitest'

import postcssConfig from '../../postcss.config.js'
import tailwindConfig from '../../tailwind.config.js'

const currentDir = dirname(fileURLToPath(import.meta.url))
const mainSource = readFileSync(resolve(currentDir, '../main.ts'), 'utf8')
const globalStyleSource = readFileSync(resolve(currentDir, '../style.css'), 'utf8')
const onboardingStyleSource = readFileSync(resolve(currentDir, '../styles/onboarding.css'), 'utf8')

// 检查全站的英文、中文和等宽字体配置，以及局部样式是否继承它们。
describe('OpenRouter 字体主题', () => {
  const fontFamily = tailwindConfig.theme.extend.fontFamily

  it('英文用 Plus Jakarta Sans，中文先用苹方，没有苹方时用思源黑体', () => {
    expect(fontFamily.sans.slice(0, 3)).toEqual([
      '"Plus Jakarta Sans Variable"',
      '"PingFang SC"',
      '"Noto Sans SC Variable"'
    ])
    expect(fontFamily.sans).toEqual(
      expect.arrayContaining(['Hiragino Sans GB', 'Microsoft YaHei', 'sans-serif'])
    )
  })

  it('使用 Geist Mono 并保留系统等宽字体回退', () => {
    expect(fontFamily.mono[0]).toBe('"Geist Mono Variable"')
    expect(fontFamily.mono).toEqual(
      expect.arrayContaining(['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace'])
    )
  })

  it('在全局样式前加载正常体、斜体、等宽和中文字体', () => {
    const fontImports = [
      "import '@fontsource-variable/plus-jakarta-sans'",
      "import '@fontsource-variable/plus-jakarta-sans/wght-italic.css'",
      "import '@fontsource-variable/geist-mono'",
      "import '@fontsource-variable/noto-sans-sc'"
    ]
    const styleImport = "import './style.css'"

    for (const fontImport of fontImports) {
      expect(mainSource).toContain(fontImport)
      expect(mainSource.indexOf(fontImport)).toBeLessThan(mainSource.indexOf(styleImport))
    }
  })

  it('思源黑体的字重上限压到 600，emoji 码位交给系统 emoji 字体', async () => {
    const [adjustNotoSansScFontFace] = postcssConfig.plugins
    const fontFace = (unicodeRange) =>
      `@font-face{font-family:x;font-weight:100 900;src:url(a.woff2);unicode-range:${unicodeRange}}`
    const process = (css, from) => postcss([adjustNotoSansScFontFace]).process(css, { from })
    const notoCss = '/repo/node_modules/@fontsource-variable/noto-sans-sc/index.css'

    // 🍥 是 U+1F365，✅ 是 U+2705，★（U+2605）默认按文字显示，留在思源黑体里。
    const mixed = await process(fontFace('U+4e00-4e02,U+1f364-1f366,U+2605,U+2705'), notoCss)
    expect(mixed.css).toContain('font-weight:100 600')
    expect(mixed.css).toContain('unicode-range:U+4e00-4e02,U+2605')

    // 只有 emoji 的切片整条删除。
    const emojiOnly = await process(fontFace('U+1f364-1f366'), notoCss)
    expect(emojiOnly.css).toBe('')

    // 其他字体原样输出。
    const jakartaCss = '/repo/node_modules/@fontsource-variable/plus-jakarta-sans/index.css'
    const jakarta = await process(fontFace('U+1f365'), jakartaCss)
    expect(jakarta.css).toContain('font-weight:100 900')
    expect(jakarta.css).toContain('unicode-range:U+1f365')
  })

  it('由根页面和引导浮层统一继承字体主题', () => {
    expect(globalStyleSource).toMatch(/html\s*\{[\s\S]*?@apply[^;]*font-sans[^;]*;/)
    expect(onboardingStyleSource).toContain('font-family: inherit !important;')
    expect(onboardingStyleSource).not.toContain('font-family: ui-sans-serif')
  })
})
