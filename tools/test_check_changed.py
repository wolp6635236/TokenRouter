"""校验推送快检按改动文件选出的检查命令。"""
import unittest

from check_changed import plan

BASE = 'abc123'


def commands(files):
    """返回 plan 生成的参数列表，Go 包选择直接回显改动目录。"""
    return [args for _, args in plan(files, BASE, go_packages=lambda dirs: ['./' + d for d in dirs])]


def flat(files):
    return [' '.join(args) for args in commands(files)]


class PlanTest(unittest.TestCase):

    def test_document_change_only_checks_whitespace(self):
        self.assertEqual(commands(['docs/index.md']), [['git', 'diff', '--check', BASE]])

    def test_go_change_lints_and_tests_changed_package(self):
        result = flat(['backend/internal/apikey/service.go'])
        self.assertIn('python3 tools/format_go.py --check --base ' + BASE, result)
        self.assertIn('env GOWORK=off go test ./...', result)
        self.assertIn('bash ../tools/golangci-lint.sh run --build-tags=integration --new-from-rev='
                      + BASE + ' ./internal/apikey', result)
        self.assertIn('go test ./internal/apikey', result)
        self.assertFalse(any('pnpm' in line for line in result))

    def test_go_dependency_change_checks_whole_module(self):
        result = flat(['backend/go.sum'])
        self.assertIn('go test ./...', result)

    def test_deleted_package_skips_lint_and_test(self):
        result = flat(['backend/internal/removed_package_for_test/gone.go'])
        self.assertFalse(any(line.startswith(('go test ./internal', 'bash ../tools')) for line in result))

    def test_frontend_source_change_runs_related_checks(self):
        result = flat(['frontend/src/main.ts'])
        self.assertIn('pnpm --dir frontend exec eslint src/main.ts', result)
        self.assertIn('pnpm --dir frontend run typecheck', result)
        self.assertIn('pnpm --dir frontend exec vitest related --run --passWithNoTests src/main.ts', result)
        self.assertNotIn('pnpm --dir frontend run test:run', result)

    def test_frontend_dependency_change_runs_full_suite(self):
        result = flat(['frontend/package.json'])
        self.assertIn('pnpm --dir frontend install --frozen-lockfile', result)
        self.assertIn('pnpm --dir frontend run test:run', result)

    def test_backend_locale_change_runs_frontend_related_tests(self):
        result = flat(['backend/internal/pkg/locale/manifest.json'])
        self.assertIn('pnpm --dir frontend exec vitest related --run --passWithNoTests '
                      '../backend/internal/pkg/locale/manifest.json', result)

    def test_script_and_tool_changes_run_their_tests(self):
        self.assertIn('make test-scripts', flat(['deploy/install.sh']))
        self.assertIn('make test-tools', flat(['tools/format_go.py']))
        self.assertIn('make test-tools', flat(['.githooks/pre-push']))


if __name__ == '__main__':
    unittest.main()
