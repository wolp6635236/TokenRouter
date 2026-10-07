"""校验 pnpm audit 报告解析和高危漏洞例外的到期规则。"""
import contextlib
import io
import json
import unittest
from unittest.mock import patch

from check_pnpm_audit_exceptions import main as audit_main, validate_audit


def audit_report(advisories=None):
    """构造含统计信息的 pnpm 审计报告。"""
    advisories = advisories or {}
    counts = dict.fromkeys(('info', 'low', 'moderate', 'high', 'critical'), 0)
    for advisory in advisories.values():
        counts[advisory['severity']] += 1
    return {'advisories': advisories, 'metadata': {'vulnerabilities': counts}}


class AuditValidationTest(unittest.TestCase):
    """扫描失败与已知漏洞分别影响验证结果。"""

    def test_valid_empty_report(self):
        validate_audit(audit_report(), 0)

    def test_rejects_failed_or_incomplete_reports(self):
        for report in ({}, {'error': {'code': 'UNAVAILABLE'}}, {'advisories': {}}, [],
                       audit_report({'a': {'severity': 'high'}})):
            with self.subTest(report=report), self.assertRaises((ValueError, TypeError)):
                validate_audit(report, 0)
        with self.assertRaises(ValueError):
            validate_audit(audit_report(), 1)
        with self.assertRaises(ValueError):
            validate_audit(audit_report(), 2)

    def test_high_vulnerability_exceptions_and_expiry(self):
        report = audit_report({'a': {'module_name': 'example', 'severity': 'high',
                                     'github_advisory_id': 'GHSA-example'}})
        for expiry, expected in (('2999-01-01', 0), ('2000-01-01', 1), (None, 1)):
            exceptions = 'version: 1\nexceptions:\n'
            if expiry:
                exceptions += ('  - package: example\n    advisory: GHSA-example\n    severity: high\n'
                               '    mitigation: verified fixture\n    expires_on: ' + expiry + '\n')
            def fixture(path, *args, **kwargs):
                return io.StringIO(json.dumps(report) if path == 'audit.json' else exceptions)
            with self.subTest(expiry=expiry), patch('builtins.open', fixture), patch('sys.argv',
                    ['audit', '--audit', 'audit.json', '--exit-code', '1', '--exceptions', 'exceptions.yml']), \
                    contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(audit_main(), expected)

    def test_missing_advisory_and_inconsistent_counts(self):
        report = audit_report({'a': {'module_name': 'example', 'severity': 'high'}})
        with self.assertRaises(ValueError):
            validate_audit(report)
        report = audit_report()
        report['metadata']['vulnerabilities']['critical'] = 1
        with self.assertRaises(ValueError):
            validate_audit(report)
        report = audit_report({'a': {'module_name': 'example', 'severity': 'high',
                                     'github_advisory_id': 'GHSA-example'}})
        with self.assertRaises(ValueError):
            validate_audit(report, 0)


if __name__ == '__main__':
    unittest.main()
