import js from '@eslint/js'
import vue from 'eslint-plugin-vue'
import globals from 'globals'
import tseslint from 'typescript-eslint'

// Vue 模板使用 Vue parser，脚本部分交给 TypeScript parser。
export default tseslint.config(
  {
    ignores: [
      'vite.config.js', 'vite.config.d.ts', '**/node_modules/**', '**/dist/**',
      '**/coverage/**', '**/.cache/**', '**/.vite/**', '**/.vitest/**', '**/*.timestamp-*.mjs'
    ]
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...vue.configs['flat/essential'],
  {
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser, ...globals.node },
      parserOptions: { parser: tseslint.parser, extraFileExtensions: ['.vue'] }
    },
    rules: {
      'no-constant-condition': 'off',
      'no-mixed-spaces-and-tabs': 'off',
      'no-useless-escape': 'off',
      'no-unused-vars': 'off',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' }],
      // 类型规则允许空对象、Function 和装箱类型。
      '@typescript-eslint/no-empty-object-type': 'off',
      '@typescript-eslint/no-unsafe-function-type': 'off',
      '@typescript-eslint/no-wrapper-object-types': 'off',
      '@typescript-eslint/ban-ts-comment': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      'vue/multi-word-component-names': 'off',
      'vue/no-use-v-if-with-v-for': 'off'
    }
  }
)
