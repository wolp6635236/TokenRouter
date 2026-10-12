#!/usr/bin/env python3
import argparse
import json
from pathlib import Path
import subprocess
import sys
from datetime import date


HIGH_SEVERITIES = {"high", "critical"}
REQUIRED_FIELDS = {"package", "advisory", "severity", "mitigation", "expires_on"}


def split_kv(line: str) -> tuple[str, str]:
    # 解析 "key: value" 形式的简单 YAML 行，并去除引号。
    key, value = line.split(":", 1)
    value = value.strip()
    if (value.startswith('"') and value.endswith('"')) or (
        value.startswith("'") and value.endswith("'")
    ):
        value = value[1:-1]
    return key.strip(), value


def parse_exceptions(path: str) -> list[dict]:
    # 逐项读取例外清单的包名、公告和有效期。
    exceptions = []
    current = None
    with open(path, "r", encoding="utf-8") as handle:
        for raw in handle:
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            if line.startswith("version:") or line.startswith("exceptions:"):
                continue
            if line.startswith("- "):
                if current:
                    exceptions.append(current)
                current = {}
                line = line[2:].strip()
                if line:
                    key, value = split_kv(line)
                    current[key] = value
                continue
            if current is not None and ":" in line:
                key, value = split_kv(line)
                current[key] = value
    if current:
        exceptions.append(current)
    return exceptions


def pick_advisory_id(advisory: dict) -> str | None:
    # 优先用 GHSA、URL 或 CVE 匹配漏洞公告。
    return (
        advisory.get("github_advisory_id")
        or advisory.get("url")
        or (advisory.get("cves") or [None])[0]
        or (str(advisory.get("id")) if advisory.get("id") is not None else None)
        or advisory.get("title")
        or advisory.get("advisory")
        or advisory.get("overview")
    )


def iter_vulns(data: dict):
    # 兼容 pnpm audit 的不同输出结构（advisories / vulnerabilities），并提取 advisory 标识。
    advisories = data.get("advisories")
    if isinstance(advisories, dict):
        for advisory in advisories.values():
            name = advisory.get("module_name") or advisory.get("name")
            severity = advisory.get("severity")
            advisory_id = pick_advisory_id(advisory)
            title = (
                advisory.get("title")
                or advisory.get("advisory")
                or advisory.get("overview")
                or advisory.get("url")
            )
            yield name, severity, advisory_id, title

    vulnerabilities = data.get("vulnerabilities")
    if isinstance(vulnerabilities, dict):
        for name, vuln in vulnerabilities.items():
            severity = vuln.get("severity")
            via = vuln.get("via", [])
            titles = []
            advisories = []
            if isinstance(via, list):
                for item in via:
                    if isinstance(item, dict):
                        advisories.append(
                            item.get("github_advisory_id")
                            or item.get("url")
                            or item.get("source")
                            or item.get("title")
                            or item.get("name")
                        )
                        titles.append(
                            item.get("title")
                            or item.get("url")
                            or item.get("advisory")
                            or item.get("source")
                        )
                    elif isinstance(item, str):
                        advisories.append(item)
                        titles.append(item)
            elif isinstance(via, str):
                advisories.append(via)
                titles.append(via)
            title = "; ".join(str(t) for t in titles if t)
            for advisory_id in [a for a in advisories if a]:
                yield name, severity, advisory_id, title


def validate_audit(data: dict, exit_code: int | None = None) -> None:
    """审计报告需要完整结构，网络错误和缺失字段返回失败。"""
    if not isinstance(data, dict) or 'error' in data:
        raise ValueError("审计服务返回错误报告")
    sections = [key for key in ('advisories', 'vulnerabilities') if key in data]
    if not sections or any(not isinstance(data[key], dict) for key in sections):
        raise ValueError("审计报告缺少漏洞列表")
    counts = data.get('metadata', {}).get('vulnerabilities')
    levels = ('info', 'low', 'moderate', 'high', 'critical')
    if not isinstance(counts, dict) or any(type(counts.get(level)) is not int or counts[level] < 0 for level in levels):
        raise ValueError("审计报告缺少有效的漏洞计数")
    for advisory in data.get('advisories', {}).values():
        if not isinstance(advisory, dict) or not (advisory.get('module_name') or advisory.get('name')):
            raise ValueError("漏洞记录缺少包名")
        if advisory.get('severity') not in levels or not pick_advisory_id(advisory):
            raise ValueError("漏洞记录缺少等级或公告标识")
    for name, vulnerability in data.get('vulnerabilities', {}).items():
        if not name or not isinstance(vulnerability, dict) or vulnerability.get('severity') not in levels:
            raise ValueError("漏洞记录缺少包名或等级")
        via = vulnerability.get('via')
        if not isinstance(via, list) or not via:
            raise ValueError("漏洞记录缺少公告来源")
        for item in via:
            if isinstance(item, str):
                if not item:
                    raise ValueError("漏洞来源为空")
            elif not isinstance(item, dict) or not any(item.get(key) for key in ('github_advisory_id', 'url', 'source')):
                raise ValueError("漏洞来源缺少公告标识")
    findings = list(iter_vulns(data))
    high_count = sum(1 for _, level, _, _ in findings if level in HIGH_SEVERITIES)
    # --audit-level=high 会过滤低等级明细，metadata 仍统计所有等级。
    if (counts['high'] + counts['critical'] > 0) != (high_count > 0):
        raise ValueError("审计计数与高危漏洞记录不符")
    if exit_code is not None and (exit_code not in (0, 1) or exit_code != int(high_count > 0)):
        raise ValueError("审计命令执行失败，退出码: " + str(exit_code))


def run_audit() -> tuple[dict, int]:
    """执行 pnpm audit，返回 JSON 报告和退出码；stderr 原样转给调用方查看。"""
    root = Path(__file__).resolve().parent.parent
    result = subprocess.run(['pnpm', '--dir', 'frontend', 'audit', '--prod', '--audit-level=high', '--json'],
                            cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.stderr:
        print(result.stderr, file=sys.stderr)
    return json.loads(result.stdout), result.returncode


def normalize_severity(severity: str) -> str:
    # 漏洞等级按小写比较。
    return (severity or "").strip().lower()


def normalize_package(name: str) -> str:
    # 将包名转成字符串并去掉首尾空白。
    if name is None:
        return ""
    return str(name).strip()


def normalize_advisory(advisory: str) -> str:
    # 公告标识转成小写后匹配。
    # pnpm 的 source 字段可能是数字，这里统一转为字符串以保证可比较。
    if advisory is None:
        return ""
    return str(advisory).strip().lower()


def parse_date(value: str) -> date | None:
    # 仅接受 ISO8601 日期格式，非法值视为无效。
    try:
        return date.fromisoformat(value)
    except ValueError:
        return None


def main() -> int:
    parser = argparse.ArgumentParser()
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--audit")
    source.add_argument("--run", action="store_true")
    parser.add_argument("--exit-code", type=int)
    parser.add_argument("--exceptions", required=True)
    args = parser.parse_args()

    try:
        if args.run:
            audit, exit_code = run_audit()
        else:
            with open(args.audit, "r", encoding="utf-8") as handle:
                audit = json.load(handle)
            exit_code = args.exit_code
        validate_audit(audit, exit_code)
    except (ValueError, OSError, AttributeError, TypeError, subprocess.CalledProcessError) as error:
        print("审计未完成: " + str(error), file=sys.stderr)
        return 1

    # 读取异常清单并建立索引，便于快速匹配包名 + advisory。
    exceptions = parse_exceptions(args.exceptions)
    exception_index = {}
    errors = []

    for exc in exceptions:
        missing = [field for field in REQUIRED_FIELDS if not exc.get(field)]
        if missing:
            errors.append(
                f"Exception missing required fields {missing}: {exc.get('package', '<unknown>')}"
            )
            continue
        exc_severity = normalize_severity(exc.get("severity"))
        exc_package = normalize_package(exc.get("package"))
        exc_advisory = normalize_advisory(exc.get("advisory"))
        exc_date = parse_date(exc.get("expires_on"))
        if exc_date is None:
            errors.append(
                f"Exception has invalid expires_on date: {exc.get('package', '<unknown>')}"
            )
            continue
        if not exc_package or not exc_advisory:
            errors.append("Exception missing package or advisory value")
            continue
        key = (exc_package, exc_advisory)
        if key in exception_index:
            errors.append(
                f"Duplicate exception for {exc_package} advisory {exc.get('advisory')}"
            )
            continue
        exception_index[key] = {
            "raw": exc,
            "severity": exc_severity,
            "expires_on": exc_date,
        }

    today = date.today()
    missing_exceptions = []
    expired_exceptions = []

    # 去重处理：同一包名 + advisory 可能在不同字段重复出现。
    seen = set()
    for name, severity, advisory_id, title in iter_vulns(audit):
        sev = normalize_severity(severity)
        if sev not in HIGH_SEVERITIES or not name:
            continue
        advisory_key = normalize_advisory(advisory_id)
        if not advisory_key:
            errors.append(
                f"High/Critical vulnerability missing advisory id: {name} ({sev})"
            )
            continue
        key = (normalize_package(name), advisory_key)
        if key in seen:
            continue
        seen.add(key)
        exc = exception_index.get(key)
        if exc is None:
            missing_exceptions.append((name, sev, advisory_id, title))
            continue
        if exc["severity"] and exc["severity"] != sev:
            errors.append(
                "Exception severity mismatch: "
                f"{name} ({advisory_id}) expected {sev}, got {exc['severity']}"
            )
        if exc["expires_on"] and exc["expires_on"] < today:
            expired_exceptions.append(
                (name, sev, advisory_id, exc["expires_on"].isoformat())
            )

    if missing_exceptions:
        errors.append("High/Critical vulnerabilities missing exceptions:")
        for name, sev, advisory_id, title in missing_exceptions:
            label = f"{name} ({sev})"
            if advisory_id:
                label = f"{label} [{advisory_id}]"
            if title:
                label = f"{label}: {title}"
            errors.append(f"- {label}")

    if expired_exceptions:
        errors.append("Exceptions expired:")
        for name, sev, advisory_id, expires_on in expired_exceptions:
            errors.append(
                f"- {name} ({sev}) [{advisory_id}] expired on {expires_on}"
            )

    if errors:
        sys.stderr.write("\n".join(errors) + "\n")
        return 1

    print("Audit exceptions validated.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
