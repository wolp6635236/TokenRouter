import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import en from '../locales/en'
import zh from '../locales/zh'
import manifest from '../../../../backend/internal/pkg/locale/manifest.json'

const root = existsSync(resolve(process.cwd(), 'src/views')) ? resolve(process.cwd(), 'src') : resolve(process.cwd(), 'frontend/src')

// 从用户页面追踪实际引用，管理员独立页面不进入本次补齐范围。
function filesUnder(path: string): string[] {
  return readdirSync(path, { withFileTypes: true }).flatMap(entry => {
    if (entry.name === '__tests__') return []
    const file = join(path, entry.name)
    return entry.isDirectory() ? filesUnder(file) : /\.(vue|ts)$/.test(file) && !/\.(spec|test)\.ts$/.test(file) ? [file] : []
  })
}

function userSources(): string[] {
  const queue = [
    ...['views/user', 'views/public', 'views/auth'].flatMap(path => filesUnder(join(root, path))),
    ...['views/HomeView.vue', 'views/ModelMarketplaceView.vue', 'views/KeyUsageView.vue', 'views/NotFoundView.vue', 'components/common/LocalizedEditor.vue', 'components/common/LocalizedFieldsEditor.vue'].map(path => join(root, path)),
  ]
  const visited = new Set<string>()
  while (queue.length) {
    const file = queue.pop()!
    if (visited.has(file) || file.includes('/i18n/locales/') || file.includes('/router/')) continue
    visited.add(file)
    const source = readFileSync(file, 'utf8')
    for (const match of source.matchAll(/(?:from\s*|import\s*\()(['"])([^'"]+)\1/g)) {
      const reference = match[2]
      if (!reference.startsWith('@/') && !reference.startsWith('.')) continue
      const base = reference.startsWith('@/') ? join(root, reference.slice(2)) : resolve(dirname(file), reference)
      const target = [base, `${base}.ts`, `${base}.vue`, join(base, 'index.ts')].find(candidate => /\.(vue|ts)$/.test(candidate) && existsSync(candidate))
      if (target && target.startsWith(root)) queue.push(target)
    }
  }
  return [...visited].sort()
}

function hasKey(value: unknown, key: string): boolean {
  let current = value
  for (const segment of key.split('.')) {
    if (!current || typeof current !== 'object' || !(segment in current)) return false
    current = (current as Record<string, unknown>)[segment]
  }
  return typeof current === 'string'
}

function flatStrings(value: unknown, prefix = '', result: Record<string, string> = {}): Record<string, string> {
  if (typeof value === 'string') result[prefix] = value
  else if (value && typeof value === 'object') {
    for (const [key, item] of Object.entries(value)) flatStrings(item, prefix ? `${prefix}.${key}` : key, result)
  }
  return result
}

describe('user translation resources', () => {
  it('resolves literal keys referenced by user pages and their dependencies', () => {
    const missing = new Set<string>()
    for (const file of userSources()) {
      const source = readFileSync(file, 'utf8')
      for (const match of source.matchAll(/(?<![\w])(?:\$t|t)\(\s*(['"])([A-Za-z][\w]*(?:\.[\w]+)+)\1/g)) {
        for (const [code, messages] of [['en', en], ['zh-Hans', zh]] as const) {
          if (!hasKey(messages, match[2])) missing.add(`${code}: ${match[2]} (${file.slice(root.length)})`)
        }
      }
    }
    expect([...missing]).toEqual([])
  })

  it('keeps keys and named placeholders consistent', () => {
    const english = flatStrings(en)
    const chinese = flatStrings(zh)
    expect(Object.keys(english).sort()).toEqual(Object.keys(chinese).sort())
    const variables = (message: string) => [...new Set([...message.matchAll(/\{([a-zA-Z_]\w*)\}/g)].map(match => match[1]))].sort()
    const mismatches = Object.keys(english).filter(key => JSON.stringify(variables(english[key])) !== JSON.stringify(variables(chinese[key])))
    expect(mismatches).toEqual([])
  })

  it('has a loadable catalog for every production language', () => {
    for (const language of manifest.locales) expect(existsSync(join(root, 'i18n/locales', language.catalog, 'index.ts'))).toBe(true)
  })
})
