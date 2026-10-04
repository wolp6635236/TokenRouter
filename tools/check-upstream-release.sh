#!/usr/bin/env bash
# 查询官方 TokenRouter 的 latest Release，并对照 tools/upstream-release.seen。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SEEN_FILE="${UPSTREAM_SEEN_FILE:-$ROOT/tools/upstream-release.seen}"
UPSTREAM_REPO="${UPSTREAM_REPO:-TokenFlux/TokenRouter}"
API_URL="https://api.github.com/repos/${UPSTREAM_REPO}/releases/latest"

if [[ "${1:-}" == "--update-seen" ]]; then
	tag="${2:-}"
	if [[ -z "$tag" ]]; then
		echo "用法: $0 --update-seen TAG" >&2
		exit 1
	fi
	printf '%s\n' "$tag" >"$SEEN_FILE"
	echo "已写入 $SEEN_FILE: $tag"
	exit 0
fi

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
	echo "用法: $0 [--update-seen TAG]"
	exit 0
fi

auth_header=()
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
	auth_header=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
elif [[ -n "${UPDATE_GITHUB_TOKEN:-}" ]]; then
	auth_header=(-H "Authorization: Bearer ${UPDATE_GITHUB_TOKEN}")
fi

body="$(
	curl -fsSL \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		"${auth_header[@]}" \
		"$API_URL"
)"

tag="$(python3 -c 'import json,sys; print((json.load(sys.stdin).get("tag_name") or "").strip())' <<<"$body")"
if [[ -z "$tag" ]]; then
	echo "官方 latest Release 缺少 tag_name" >&2
	exit 1
fi

if [[ ! -f "$SEEN_FILE" ]]; then
	echo "缺少对照文件 $SEEN_FILE" >&2
	exit 1
fi

seen="$(tr -d '[:space:]' <"$SEEN_FILE")"
echo "upstream_latest=$tag"
echo "seen=$seen"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
	{
		echo "new_tag=$tag"
		echo "seen_tag=$seen"
	} >>"$GITHUB_OUTPUT"
fi

if [[ "$tag" == "$seen" ]]; then
	echo "官方 latest 与对照文件一致。"
	if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
		echo "changed=false" >>"$GITHUB_OUTPUT"
	fi
	exit 0
fi

echo "官方发布了 $tag。确认后 git fetch upstream，merge，处理冲突，再打本仓库 tag。"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
	echo "changed=true" >>"$GITHUB_OUTPUT"
fi
exit 2
