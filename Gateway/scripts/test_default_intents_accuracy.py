#!/usr/bin/env python3
import json
import sys
import time
import urllib.error
import urllib.request
from collections import defaultdict
from datetime import datetime
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CASES_PATH = ROOT / "testdata" / "default_intent_accuracy_cases.json"
RESULTS_DIR = ROOT / "test_results"
ROUTE_URL = "http://127.0.0.1:19090/route"


def load_cases():
    with CASES_PATH.open("r", encoding="utf-8") as f:
        return json.load(f)


def post_route(message, session_id):
    payload = {
        "session_id": session_id,
        "message": message,
        "intents": [],
    }
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        ROUTE_URL,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    started = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode("utf-8")
            elapsed_ms = round((time.perf_counter() - started) * 1000, 2)
            return resp.status, raw, elapsed_ms, None
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        elapsed_ms = round((time.perf_counter() - started) * 1000, 2)
        return e.code, raw, elapsed_ms, f"HTTP {e.code}: {raw}"
    except Exception as e:  # pylint: disable=broad-except
        elapsed_ms = round((time.perf_counter() - started) * 1000, 2)
        return None, "", elapsed_ms, str(e)


def normalize_missing(value):
    if value is None:
        return []
    if isinstance(value, list):
        return value
    return [value]


def check_param(actual_params, name, rule):
    if name not in actual_params:
        return False, f"param {name} missing"
    actual = actual_params[name]
    if "equals" in rule and actual != rule["equals"]:
        return False, f"param {name} expected {rule['equals']!r}, got {actual!r}"
    if "equals_any" in rule and actual not in rule["equals_any"]:
        return False, f"param {name} expected one of {rule['equals_any']!r}, got {actual!r}"
    return True, ""


def evaluate_case(case, actual):
    expect = case["expect"]
    failures = []

    if actual.get("route") != expect.get("route"):
        failures.append(f"route expected {expect.get('route')!r}, got {actual.get('route')!r}")

    if expect.get("skill") != actual.get("skill"):
        failures.append(f"skill expected {expect.get('skill')!r}, got {actual.get('skill')!r}")

    expected_missing = normalize_missing(expect.get("missing_params"))
    actual_missing = normalize_missing(actual.get("missing_params"))
    if expected_missing != actual_missing:
        failures.append(f"missing_params expected {expected_missing!r}, got {actual_missing!r}")

    actual_params = actual.get("params") or {}
    for name, rule in (expect.get("params") or {}).items():
        ok, message = check_param(actual_params, name, rule)
        if not ok:
            failures.append(message)

    return failures


def write_results(results, summary):
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    ts = datetime.now().strftime("%Y%m%d_%H%M%S")
    json_path = RESULTS_DIR / f"default_intents_accuracy_{ts}.json"
    md_path = RESULTS_DIR / f"default_intents_accuracy_{ts}.md"

    with json_path.open("w", encoding="utf-8") as f:
        json.dump({"summary": summary, "results": results}, f, ensure_ascii=False, indent=2)

    lines = []
    lines.append("# Default Intents Accuracy Report")
    lines.append("")
    lines.append(f"- Generated at: `{datetime.now().isoformat(timespec='seconds')}`")
    lines.append(f"- Route URL: `{ROUTE_URL}`")
    lines.append(f"- Total cases: `{summary['total']}`")
    lines.append(f"- Passed: `{summary['passed']}`")
    lines.append(f"- Failed: `{summary['failed']}`")
    lines.append(f"- Pass rate: `{summary['pass_rate']}`")
    lines.append(f"- Avg latency: `{summary['avg_latency_ms']} ms`")
    lines.append("")
    lines.append("## By Skill")
    lines.append("")
    for skill, item in summary["by_skill"].items():
        lines.append(f"- `{skill}`: {item['passed']}/{item['total']} passed, avg `{item['avg_latency_ms']} ms`")
    lines.append("")
    lines.append("## Cases")
    lines.append("")
    for item in results:
        status = "PASS" if item["passed"] else "FAIL"
        lines.append(f"### {status} `{item['id']}`")
        lines.append("")
        lines.append(f"- message: `{item['message']}`")
        lines.append(f"- elapsed_ms: `{item['elapsed_ms']}`")
        lines.append(f"- actual: `{json.dumps(item['actual'], ensure_ascii=False)}`")
        if item["failures"]:
            lines.append(f"- failures: `{'; '.join(item['failures'])}`")
        lines.append("")

    with md_path.open("w", encoding="utf-8") as f:
        f.write("\n".join(lines))

    return json_path, md_path


def main():
    cases = load_cases()
    results = []
    by_skill = defaultdict(lambda: {"total": 0, "passed": 0, "latencies": []})

    for idx, case in enumerate(cases, start=1):
        status, raw, elapsed_ms, err = post_route(case["message"], f"accuracy-{case['id']}")
        actual = {}
        failures = []

        if err:
            failures.append(err)
        else:
            try:
                actual = json.loads(raw)
            except json.JSONDecodeError as e:
                failures.append(f"invalid json response: {e}")
                actual = {"raw": raw}

        if not failures:
            failures.extend(evaluate_case(case, actual))

        passed = len(failures) == 0
        skill_key = case["expect"].get("skill") or case["expect"].get("route") or "unknown"
        by_skill[skill_key]["total"] += 1
        by_skill[skill_key]["latencies"].append(elapsed_ms)
        if passed:
            by_skill[skill_key]["passed"] += 1

        result = {
            "index": idx,
            "id": case["id"],
            "message": case["message"],
            "http_status": status,
            "elapsed_ms": elapsed_ms,
            "passed": passed,
            "actual": actual,
            "failures": failures,
        }
        results.append(result)
        label = "PASS" if passed else "FAIL"
        print(f"[{label}] {case['id']} {elapsed_ms}ms")
        if failures:
            for failure in failures:
                print(f"  - {failure}")

    total = len(results)
    passed = sum(1 for item in results if item["passed"])
    failed = total - passed
    avg_latency_ms = round(sum(item["elapsed_ms"] for item in results) / total, 2) if total else 0

    summary = {
        "total": total,
        "passed": passed,
        "failed": failed,
        "pass_rate": f"{(passed / total * 100):.1f}%" if total else "0.0%",
        "avg_latency_ms": avg_latency_ms,
        "by_skill": {},
    }

    for skill, item in sorted(by_skill.items()):
        avg = round(sum(item["latencies"]) / len(item["latencies"]), 2) if item["latencies"] else 0
        summary["by_skill"][skill] = {
            "total": item["total"],
            "passed": item["passed"],
            "avg_latency_ms": avg,
        }

    json_path, md_path = write_results(results, summary)
    print("")
    print(f"Summary: {passed}/{total} passed, avg {avg_latency_ms}ms")
    print(f"JSON report: {json_path}")
    print(f"Markdown report: {md_path}")

    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
