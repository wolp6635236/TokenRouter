#!/usr/bin/env python3
"""推送前快检：按相对基准提交改动的文件选择检查，在当前工作区执行。"""
import argparse
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent.parent
ZERO_OID = '0' * 40
EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904'
LINTABLE = ('.ts', '.tsx', '.js', '.mjs', '.cjs', '.vue')


def git(*args):
    """执行 Git 并返回去掉首尾空白的输出。"""
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def resolves(rev):
    """判断本地能否解析到这个提交。"""
    return subprocess.run(['git', 'rev-parse', '--verify', '--quiet', rev + '^{commit}'],
                          cwd=ROOT, stdout=subprocess.DEVNULL).returncode == 0


def default_base(rev, remote='origin'):
    """返回 rev 与远端默认分支的共同祖先，找不到远端分支时用空树。"""
    for candidate in ('@{upstream}', remote + '/HEAD', remote + '/main'):
        if resolves(candidate):
            return git('merge-base', rev, candidate)
    return EMPTY_TREE


def push_base(lines, remote):
    """把 pre-push 从 stdin 收到的引用转成一个比较基准，只删除引用时返回 None。"""
    bases = []
    for line in lines:
        _, local_oid, _, remote_oid = line.split()
        if local_oid == ZERO_OID:
            continue
        if remote_oid != ZERO_OID and resolves(remote_oid):
            bases.append(remote_oid)
        else:
            bases.append(default_base(local_oid, remote))
    if not bases:
        return None
    if EMPTY_TREE in bases:
        return EMPTY_TREE
    # 一次推送多个引用时，取所有基准的共同祖先，覆盖全部待推送提交。
    return git('merge-base', '--octopus', *bases)


def changed_files(base):
    """返回基准到工作区的改动和未跟踪文件，删除的文件也包含在内。"""
    files = set()
    for args in (['diff', '--name-only', '--no-renames', '-z', base],
                 ['ls-files', '--others', '--exclude-standard', '-z']):
        output = subprocess.check_output(['git', *args], cwd=ROOT)
        files.update(os.fsdecode(name) for name in output.split(b'\0') if name)
    return sorted(files)


def go_test_packages(directories):
    """从改动目录中挑出默认构建下有 Go 文件的包，只含 integration 文件的包由 lint 覆盖。"""
    if not directories:
        return []
    output = subprocess.check_output(
        ['go', 'list', '-e', '-f', '{{if or .GoFiles .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}',
         *['./' + d for d in directories]],
        cwd=ROOT / 'backend', text=True)
    return ['./' + os.path.relpath(d, ROOT / 'backend') for d in output.split()]


def plan(files, base, go_packages=go_test_packages):
    """把改动文件映射成按顺序执行的检查命令，每条命令是 (工作目录, 参数列表)。"""
    commands = [('.', ['git', 'diff', '--check', base])]
    go_files = [f for f in files if f.startswith('backend/') and f.endswith('.go')]
    go_deps = any(f in ('backend/go.mod', 'backend/go.sum') for f in files)
    if go_files or go_deps:
        commands.append(('.', ['python3', 'tools/format_go.py', '--check', '--base', base]))
        commands.append(('.', ['python3', 'tools/format_go.py', '--check']))
        commands.append(('tools/architecture', ['env', 'GOWORK=off', 'go', 'test', './...']))
        if go_deps:
            lint_targets = test_packages = ['./...']
        else:
            # 删除的包由 CI 检查它的使用方。
            directories = sorted({os.path.dirname(f)[len('backend/'):] or '.' for f in go_files
                                  if (ROOT / os.path.dirname(f)).is_dir()})
            lint_targets = ['./' + d for d in directories]
            test_packages = go_packages(directories)
        if lint_targets:
            commands.append(('backend', ['bash', '../tools/golangci-lint.sh', 'run', '--build-tags=integration',
                                         '--new-from-rev=' + base, *lint_targets]))
        if test_packages:
            commands.append(('backend', ['go', 'test', *test_packages]))

    frontend = [f for f in files if f.startswith('frontend/')]
    locale = [f for f in files if f.startswith('backend/internal/pkg/locale/') and f.endswith('.json')]
    if frontend or locale:
        pnpm = ['pnpm', '--dir', 'frontend']
        config = [f for f in frontend if not f.startswith(('frontend/src/', 'frontend/public/'))]
        if any(f in ('frontend/package.json', 'frontend/pnpm-lock.yaml') for f in config):
            commands.append(('.', [*pnpm, 'install', '--frozen-lockfile']))
        if config:
            # 依赖和构建配置影响整个前端，跑完整 lint 和测试。
            commands.append(('.', [*pnpm, 'run', 'lint:check']))
            commands.append(('.', [*pnpm, 'run', 'typecheck']))
            commands.append(('.', [*pnpm, 'run', 'test:run']))
        else:
            present = [f for f in frontend if (ROOT / f).is_file()]
            lintable = [f[len('frontend/'):] for f in present if f.endswith(LINTABLE)]
            if lintable:
                commands.append(('.', [*pnpm, 'exec', 'eslint', *lintable]))
            commands.append(('.', [*pnpm, 'run', 'check:ui']))
            commands.append(('.', [*pnpm, 'run', 'typecheck']))
            related = [os.path.relpath(ROOT / f, ROOT / 'frontend') for f in present + locale]
            if related:
                commands.append(('.', [*pnpm, 'exec', 'vitest', 'related', '--run', '--passWithNoTests', *related]))

    if any(f.startswith('deploy/') or f.startswith('tools/goreleaser') for f in files):
        commands.append(('.', ['make', 'test-scripts']))
    if any((f.startswith('tools/') and f.endswith('.py')) or f.startswith('.githooks/') for f in files):
        commands.append(('.', ['make', 'test-tools']))
    return commands


def run(commands):
    """依次执行命令，遇到失败立即返回它的退出码。"""
    started = time.monotonic()
    for directory, args in commands:
        print('==> ' + ('' if directory == '.' else '(' + directory + ') ') + ' '.join(args), flush=True)
        code = subprocess.call(args, cwd=ROOT / directory)
        if code:
            print('快检失败。修复后重新推送；确需跳过时使用 git push --no-verify。', file=sys.stderr)
            return code
    print(f'快检通过，用时 {time.monotonic() - started:.0f} 秒。完整检查由 CI 执行。', flush=True)
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', help='比较基准，默认取上游分支的共同祖先')
    parser.add_argument('--pre-push', metavar='REMOTE', help='由 pre-push hook 调用，从 stdin 读取待推送引用')
    args = parser.parse_args()
    if args.pre_push:
        base = push_base(sys.stdin.read().splitlines(), args.pre_push)
        if base is None:
            return 0
    else:
        base = args.base or default_base('HEAD')
    files = changed_files(base)
    print(f'基准 {base[:12]}，改动 {len(files)} 个文件', flush=True)
    return run(plan(files, base))


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, subprocess.CalledProcessError) as error:
        print('快检未完成: ' + str(error), file=sys.stderr)
        sys.exit(1)
