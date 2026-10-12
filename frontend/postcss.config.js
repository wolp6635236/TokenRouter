import autoprefixer from 'autoprefixer'
import tailwindcss from 'tailwindcss'

// 思源黑体可变字体 CSS 所在的目录。
const notoSansScCssPattern = /@fontsource-variable[\\/]noto-sans-sc[\\/]/

// 思源黑体的字重上限。苹方最粗是 Semibold（600），font-bold 在 Mac 上也只显示 600，
// 思源黑体封顶在同一档，Windows 上的粗体标题和 Mac 一样粗。
const notoSansScMaxWeight = 600

// 默认以彩色 emoji 显示的字符，例如 🍥、⚡、✅。
const emojiPresentationPattern = /\p{Emoji_Presentation}/u

// parseUnicodeRange 把 "U+4e00-4e0b,U+4e2d" 解析成 [起点, 终点] 列表。
const parseUnicodeRange = (value) =>
  value.split(',').map((part) => {
    const [start, end = start] = part.trim().replace(/^U\+/i, '').split('-')
    return [parseInt(start, 16), parseInt(end, 16)]
  })

// formatUnicodeRange 把 [起点, 终点] 列表写回 unicode-range 的格式。
const formatUnicodeRange = (ranges) =>
  ranges
    .map(([start, end]) => (start === end ? `U+${start.toString(16)}` : `U+${start.toString(16)}-${end.toString(16)}`))
    .join(',')

// removeEmojiCodePoints 从码位区间里去掉 emoji 字符，剩下的码位重新合并成连续区间。
const removeEmojiCodePoints = (ranges) => {
  const kept = []
  for (const [start, end] of ranges) {
    for (let codePoint = start; codePoint <= end; codePoint++) {
      if (emojiPresentationPattern.test(String.fromCodePoint(codePoint))) continue

      const last = kept[kept.length - 1]
      if (last && last[1] === codePoint - 1) {
        last[1] = codePoint
      } else {
        kept.push([codePoint, codePoint])
      }
    }
  }
  return kept
}

// adjustNotoSansScFontFace 改写思源黑体的 @font-face。
// 字重范围上限改成 notoSansScMaxWeight，浏览器会把 font-bold 压到这一档。
// unicode-range 去掉 emoji 码位，emoji 由系统的彩色 emoji 字体显示。
// 去掉 emoji 后为空的切片整条删除。
const adjustNotoSansScFontFace = () => ({
  postcssPlugin: 'adjust-noto-sans-sc-font-face',
  AtRule: {
    'font-face': (rule) => {
      const source = rule.source?.input.file ?? ''
      if (!notoSansScCssPattern.test(source)) return

      rule.walkDecls('font-weight', (decl) => {
        const [min] = decl.value.trim().split(/\s+/)
        decl.value = `${min} ${notoSansScMaxWeight}`
      })
      rule.walkDecls('unicode-range', (decl) => {
        const ranges = removeEmojiCodePoints(parseUnicodeRange(decl.value))
        if (ranges.length === 0) {
          rule.remove()
          return
        }
        decl.value = formatUnicodeRange(ranges)
      })
    }
  }
})
adjustNotoSansScFontFace.postcss = true

export default {
  plugins: [adjustNotoSansScFontFace(), tailwindcss(), autoprefixer()]
}
