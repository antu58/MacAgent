#!/usr/bin/env python3
import json
import os
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime


def normalize_endpoint(ep: str) -> str:
    ep = ep.rstrip("/")
    if ep.endswith("/chat/completions"):
        return ep
    if ep.endswith("/v1"):
        return ep + "/chat/completions"
    return ep + "/v1/chat/completions"


def post_json(url: str, payload: dict, timeout: float = 60.0):
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        body = resp.read().decode("utf-8", errors="replace")
    return body


def measure_nonstream(url: str, payload: dict):
    p = dict(payload)
    p.pop("stream", None)
    start = time.perf_counter()
    body = post_json(url, p)
    elapsed = time.perf_counter() - start
    obj = json.loads(body)
    return obj, elapsed, body


def measure_stream(url: str, payload: dict, timeout: float = 120.0):
    p = dict(payload)
    p["stream"] = True
    data = json.dumps(p, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )

    start = time.perf_counter()
    ttft = None
    first_event_at = None
    events = 0
    errors = []

    with urllib.request.urlopen(req, timeout=timeout) as resp:
        for raw in resp:
            line = raw.decode("utf-8", errors="replace").strip()
            if not line or not line.startswith("data:"):
                continue

            data_part = line[5:].strip()
            if data_part == "[DONE]":
                break

            now = time.perf_counter()
            if first_event_at is None:
                first_event_at = now - start

            try:
                obj = json.loads(data_part)
            except Exception as e:
                errors.append(f"json_parse_error: {e}")
                continue

            events += 1
            choices = obj.get("choices") or []
            if not choices:
                continue

            delta = choices[0].get("delta") or {}
            has_content = bool(delta.get("content"))
            has_tool = bool(delta.get("tool_calls"))
            has_reasoning = bool(delta.get("reasoning"))
            if ttft is None and (has_content or has_tool or has_reasoning):
                ttft = now - start

    total = time.perf_counter() - start
    if ttft is None:
        ttft = first_event_at
    return ttft, total, events, errors


def arg_check_pass(mode: str, args_text: str) -> bool:
    if mode.startswith("contains:"):
        needle = mode.split(":", 1)[1]
        return needle in args_text
    if mode == "non_empty":
        return bool(args_text)
    return True


def main():
    endpoint = normalize_endpoint(
        sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:18080/v1"
    )
    model = os.environ.get("MODEL_ID", "mlx-community/Qwen3.5-0.8B-8bit")
    out_dir = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "run"
    )
    os.makedirs(out_dir, exist_ok=True)
    out_file = os.path.join(
        out_dir, f"tool_call_benchmark_{datetime.now().strftime('%Y%m%d_%H%M%S')}.jsonl"
    )

    cases = [
        {
            "id": "C1_reminder",
            "expected_tool": "create_reminder",
            "arg_check": "non_empty",
            "payload": {
                "model": model,
                "messages": [
                    {"role": "system", "content": "你是任务助手，必须优先调用工具。"},
                    {"role": "user", "content": "帮我创建一个明天早上8点提醒喝水的任务"},
                ],
                "tools": [
                    {
                        "type": "function",
                        "function": {
                            "name": "create_reminder",
                            "description": "创建提醒任务",
                            "parameters": {
                                "type": "object",
                                "properties": {
                                    "title": {"type": "string"},
                                    "datetime": {"type": "string"},
                                    "note": {"type": "string"},
                                },
                                "required": ["title", "datetime"],
                            },
                        },
                    }
                ],
                "tool_choice": "auto",
                "max_tokens": 256,
            },
        },
        {
            "id": "C2_calendar",
            "expected_tool": "create_calendar_event",
            "arg_check": "contains:2026-03-20",
            "payload": {
                "model": model,
                "messages": [
                    {"role": "system", "content": "你是任务助手，必须优先调用工具。"},
                    {
                        "role": "user",
                        "content": "请帮我创建日程：2026-03-20 15:00 和 张三 开会，时长30分钟，地点A会议室",
                    },
                ],
                "tools": [
                    {
                        "type": "function",
                        "function": {
                            "name": "create_calendar_event",
                            "description": "创建日历事件",
                            "parameters": {
                                "type": "object",
                                "properties": {
                                    "title": {"type": "string"},
                                    "start_time": {"type": "string"},
                                    "duration_minutes": {"type": "integer"},
                                    "location": {"type": "string"},
                                },
                                "required": ["title", "start_time"],
                            },
                        },
                    },
                    {
                        "type": "function",
                        "function": {
                            "name": "create_todo",
                            "description": "创建待办",
                            "parameters": {
                                "type": "object",
                                "properties": {"title": {"type": "string"}},
                                "required": ["title"],
                            },
                        },
                    },
                ],
                "tool_choice": "auto",
                "max_tokens": 256,
            },
        },
        {
            "id": "C3_email",
            "expected_tool": "send_email",
            "arg_check": "non_empty",
            "payload": {
                "model": model,
                "messages": [
                    {"role": "system", "content": "你是任务助手，必须优先调用工具。"},
                    {
                        "role": "user",
                        "content": "给王总发邮件：我今天会晚到10分钟，主题是 会议迟到说明",
                    },
                ],
                "tools": [
                    {
                        "type": "function",
                        "function": {
                            "name": "send_email",
                            "description": "发送邮件",
                            "parameters": {
                                "type": "object",
                                "properties": {
                                    "to": {"type": "string"},
                                    "subject": {"type": "string"},
                                    "body": {"type": "string"},
                                },
                                "required": ["to", "subject", "body"],
                            },
                        },
                    },
                    {
                        "type": "function",
                        "function": {
                            "name": "create_todo",
                            "description": "创建待办",
                            "parameters": {
                                "type": "object",
                                "properties": {"title": {"type": "string"}},
                                "required": ["title"],
                            },
                        },
                    },
                ],
                "tool_choice": "auto",
                "max_tokens": 256,
            },
        },
        {
            "id": "C4_expense",
            "expected_tool": "add_expense",
            "arg_check": "contains:38",
            "payload": {
                "model": model,
                "messages": [
                    {"role": "system", "content": "你是任务助手，必须优先调用工具。"},
                    {
                        "role": "user",
                        "content": "记一笔午餐支出38元，分类餐饮，备注牛肉面",
                    },
                ],
                "tools": [
                    {
                        "type": "function",
                        "function": {
                            "name": "add_expense",
                            "description": "记账",
                            "parameters": {
                                "type": "object",
                                "properties": {
                                    "amount": {"type": "number"},
                                    "currency": {"type": "string"},
                                    "category": {"type": "string"},
                                    "note": {"type": "string"},
                                },
                                "required": ["amount", "category"],
                            },
                        },
                    },
                    {
                        "type": "function",
                        "function": {
                            "name": "create_note",
                            "description": "创建笔记",
                            "parameters": {
                                "type": "object",
                                "properties": {"text": {"type": "string"}},
                                "required": ["text"],
                            },
                        },
                    },
                ],
                "tool_choice": "auto",
                "max_tokens": 256,
            },
        },
        {
            "id": "C5_weather_chain",
            "expected_tool": "get_weather",
            "arg_check": "non_empty",
            "payload": {
                "model": model,
                "messages": [
                    {"role": "system", "content": "你是任务助手，必须优先调用工具。"},
                    {
                        "role": "user",
                        "content": "查一下明天上海天气，如果下雨就提醒我带伞",
                    },
                ],
                "tools": [
                    {
                        "type": "function",
                        "function": {
                            "name": "get_weather",
                            "description": "获取天气",
                            "parameters": {
                                "type": "object",
                                "properties": {
                                    "city": {"type": "string"},
                                    "date": {"type": "string"},
                                },
                                "required": ["city", "date"],
                            },
                        },
                    },
                    {
                        "type": "function",
                        "function": {
                            "name": "create_reminder",
                            "description": "创建提醒",
                            "parameters": {
                                "type": "object",
                                "properties": {
                                    "title": {"type": "string"},
                                    "datetime": {"type": "string"},
                                },
                                "required": ["title", "datetime"],
                            },
                        },
                    },
                ],
                "tool_choice": "auto",
                "max_tokens": 256,
            },
        },
    ]

    selection_pass = 0
    args_pass = 0
    total = len(cases)
    ttft_vals = []
    total_vals = []
    tps_vals = []

    for case in cases:
        cid = case["id"]
        payload = case["payload"]
        expected_tool = case["expected_tool"]

        try:
            resp_obj, total_elapsed, raw_text = measure_nonstream(endpoint, payload)
        except urllib.error.URLError as e:
            print(f"[{cid}] request_error={e}")
            continue
        except Exception as e:
            print(f"[{cid}] parse_error={e}")
            continue

        try:
            ttft, stream_total, stream_events, stream_errors = measure_stream(
                endpoint, payload
            )
        except Exception as e:
            ttft, stream_total, stream_events, stream_errors = None, None, 0, [str(e)]

        choice0 = (resp_obj.get("choices") or [{}])[0]
        finish_reason = choice0.get("finish_reason") or ""
        msg = choice0.get("message") or {}
        tool_calls = msg.get("tool_calls") or []
        tool_name = ""
        tool_args = ""
        if tool_calls:
            fn = (tool_calls[0] or {}).get("function") or {}
            tool_name = fn.get("name") or ""
            tool_args = fn.get("arguments") or ""

        usage = resp_obj.get("usage") or {}
        completion_tokens = usage.get("completion_tokens")

        selection_ok = finish_reason == "tool_calls" and tool_name == expected_tool
        args_ok = selection_ok and arg_check_pass(case["arg_check"], tool_args)

        if selection_ok:
            selection_pass += 1
        if args_ok:
            args_pass += 1

        # tokens/s:
        # - overall_tps: completion_tokens / total_elapsed (more stable)
        # - gen_tps: completion_tokens / (total_elapsed - ttft) (can be unstable if gap is tiny)
        overall_tps = None
        gen_tps = None
        if completion_tokens is not None:
            if total_elapsed > 0:
                overall_tps = completion_tokens / total_elapsed
            if ttft is not None and total_elapsed > ttft:
                gen_dur = total_elapsed - ttft
                # avoid misleading spikes when the measured gap is near zero
                if gen_dur >= 0.05:
                    gen_tps = completion_tokens / gen_dur

        if ttft is not None:
            ttft_vals.append(ttft)
        total_vals.append(total_elapsed)
        if overall_tps is not None:
            tps_vals.append(overall_tps)

        print(
            f"[{cid}] tool={tool_name or 'none'} expect={expected_tool} "
            f"sel={'PASS' if selection_ok else 'FAIL'} args={'PASS' if args_ok else 'FAIL'} "
            f"ttft={ttft:.3f}s total={total_elapsed:.3f}s "
            f"overall_tps={(f'{overall_tps:.2f}' if overall_tps is not None else 'n/a')} "
            f"gen_tps={(f'{gen_tps:.2f}' if gen_tps is not None else 'n/a')}"
        )

        row = {
            "case_id": cid,
            "expected_tool": expected_tool,
            "finish_reason": finish_reason,
            "tool_name": tool_name,
            "tool_args": tool_args,
            "selection_pass": selection_ok,
            "args_pass": args_ok,
            "ttft_sec": ttft,
            "total_sec": total_elapsed,
            "stream_total_sec": stream_total,
            "completion_tokens": completion_tokens,
            "overall_tokens_per_sec": overall_tps,
            "gen_tokens_per_sec": gen_tps,
            "stream_events": stream_events,
            "stream_errors": stream_errors,
            "raw_nonstream": raw_text,
        }
        with open(out_file, "a", encoding="utf-8") as f:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")

    def avg(vals):
        return sum(vals) / len(vals) if vals else None

    avg_ttft = avg(ttft_vals)
    avg_total = avg(total_vals)
    avg_tps = avg(tps_vals)

    print("")
    print(f"Selection accuracy: {selection_pass}/{total}")
    print(f"Argument sanity: {args_pass}/{total}")
    print(f"Avg TTFT: {(f'{avg_ttft:.3f}s' if avg_ttft is not None else 'n/a')}")
    print(f"Avg total: {(f'{avg_total:.3f}s' if avg_total is not None else 'n/a')}")
    print(f"Avg overall tokens/s: {(f'{avg_tps:.2f}' if avg_tps is not None else 'n/a')}")
    print(f"Raw report: {out_file}")


if __name__ == "__main__":
    main()
