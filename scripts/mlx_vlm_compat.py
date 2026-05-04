import json
import time
import uuid
from typing import Any

from fastapi import HTTPException
from fastapi.responses import JSONResponse, Response, StreamingResponse
from starlette.requests import Request


OPENAI_CHAT_PATHS = {"/chat/completions", "/v1/chat/completions"}
OPENAI_RESPONSE_PATHS = {"/responses", "/v1/responses"}
OPENAI_PATHS = OPENAI_CHAT_PATHS | OPENAI_RESPONSE_PATHS
ANTHROPIC_PATHS = {"/messages", "/v1/messages"}
THINK_START = "<think>"
THINK_END = "</think>"


def install_compat(app, server_mod, default_max_tokens: int) -> None:
    """Install request/response compatibility shims around mlx_vlm.server."""

    _patch_default_thinking(server_mod)

    @app.middleware("http")
    async def openai_compat_middleware(request: Request, call_next):
        if request.method != "POST" or request.url.path not in OPENAI_PATHS:
            return await call_next(request)

        response = await call_next(request)

        content_type = response.headers.get("content-type", "")
        if content_type.startswith("text/event-stream"):
            return StreamingResponse(
                _openai_sse_with_reasoning(response.body_iterator, False),
                status_code=response.status_code,
                media_type="text/event-stream",
                headers=_response_headers(response.headers),
            )

        if "application/json" not in content_type:
            return response

        raw = b"".join([chunk async for chunk in response.body_iterator])
        try:
            data = json.loads(raw)
        except json.JSONDecodeError:
            return Response(
                content=raw,
                status_code=response.status_code,
                media_type=content_type,
                headers=_response_headers(response.headers),
            )

        _split_openai_reasoning(data, force_reasoning=False)
        return JSONResponse(
            content=data,
            status_code=response.status_code,
            headers=_response_headers(response.headers),
        )

    @app.post("/messages", response_model=None, include_in_schema=False)
    @app.post("/v1/messages", response_model=None, include_in_schema=False)
    async def anthropic_messages_endpoint(request: Request):
        try:
            payload = await request.json()
        except json.JSONDecodeError as exc:
            raise HTTPException(status_code=400, detail=f"Invalid JSON: {exc}") from exc
        if not isinstance(payload, dict):
            raise HTTPException(status_code=400, detail="Anthropic request must be a JSON object.")

        openai_payload = _anthropic_to_openai(payload, default_max_tokens)
        if openai_payload.get("stream") is True:
            return StreamingResponse(
                _anthropic_stream(server_mod, openai_payload),
                media_type="text/event-stream",
                headers={"Cache-Control": "no-cache", "Connection": "keep-alive"},
            )

        result = await server_mod.chat_completions_endpoint(
            server_mod.ChatRequest(**openai_payload)
        )
        data = result.model_dump() if hasattr(result, "model_dump") else result
        _split_openai_reasoning(
            data,
            force_reasoning=openai_payload.get("enable_thinking") is True,
        )
        return JSONResponse(content=_openai_to_anthropic(data, openai_payload))


def _patch_default_thinking(server_mod) -> None:
    if getattr(server_mod, "_macagent_default_thinking_patched", False):
        return

    original_apply_chat_template = server_mod.apply_chat_template

    def apply_chat_template_with_default_thinking(*args, **kwargs):
        kwargs.setdefault("enable_thinking", False)
        return original_apply_chat_template(*args, **kwargs)

    server_mod.apply_chat_template = apply_chat_template_with_default_thinking
    server_mod._macagent_default_thinking_patched = True


def _loads_json_object(body: bytes) -> dict[str, Any] | None:
    if not body:
        return None
    try:
        payload = json.loads(body)
    except json.JSONDecodeError:
        return None
    return payload if isinstance(payload, dict) else None


def _apply_openai_defaults(
    payload: dict[str, Any],
    *,
    path: str,
    default_max_tokens: int,
) -> bool:
    changed = False

    # Qwen3.5 defaults to thinking mode. Local agent routes should not leak
    # reasoning unless a caller explicitly opts in.
    if "enable_thinking" not in payload:
        payload["enable_thinking"] = False
        changed = True

    if default_max_tokens > 0:
        if path in OPENAI_CHAT_PATHS and "max_tokens" not in payload:
            payload["max_tokens"] = default_max_tokens
            changed = True
        if path in OPENAI_RESPONSE_PATHS and "max_output_tokens" not in payload:
            payload["max_output_tokens"] = default_max_tokens
            changed = True

    return changed


def _response_headers(headers) -> dict[str, str]:
    return {
        key: value
        for key, value in headers.items()
        if key.lower() not in {"content-length", "content-type"}
    }


def _split_openai_reasoning(data: dict[str, Any], *, force_reasoning: bool) -> None:
    for choice in data.get("choices") or []:
        message = choice.get("message")
        if isinstance(message, dict):
            _split_message_reasoning(message, force_reasoning=force_reasoning)
        delta = choice.get("delta")
        if isinstance(delta, dict):
            _split_delta_reasoning(delta, force_reasoning=force_reasoning)


def _split_message_reasoning(message: dict[str, Any], *, force_reasoning: bool) -> None:
    content = message.get("content")
    if not isinstance(content, str):
        return
    reasoning, final = _split_reasoning_text(content, force_reasoning=force_reasoning)
    if reasoning == "":
        return
    existing = message.get("reasoning_content")
    if isinstance(existing, str) and existing.strip():
        reasoning = f"{existing.rstrip()}\n{reasoning}"
    message["reasoning_content"] = reasoning
    message["reasoning"] = reasoning
    message["content"] = final


def _split_delta_reasoning(delta: dict[str, Any], *, force_reasoning: bool) -> None:
    content = delta.get("content")
    if not isinstance(content, str) or content == "":
        return
    reasoning, final = _split_reasoning_text(content, force_reasoning=force_reasoning)
    if reasoning:
        delta["reasoning_content"] = reasoning
        delta["reasoning"] = reasoning
        delta["content"] = final


def _split_reasoning_text(text: str, *, force_reasoning: bool) -> tuple[str, str]:
    if THINK_END in text:
        reasoning, final = text.split(THINK_END, 1)
        return _clean_reasoning(reasoning), final.lstrip()
    if text.lstrip().startswith(THINK_START) or _looks_like_reasoning(text):
        return _clean_reasoning(text), ""
    if force_reasoning:
        return _clean_reasoning(text), ""
    return "", text


def _looks_like_reasoning(text: str) -> bool:
    stripped = text.lstrip()
    return stripped.startswith("Thinking Process:") or stripped.startswith("We need ")


def _clean_reasoning(text: str) -> str:
    text = text.strip()
    if text.startswith(THINK_START):
        text = text[len(THINK_START) :]
    return text.strip()


def _stream_prefix_decision(text: str) -> str:
    stripped = text.lstrip()
    if not stripped:
        return "wait"
    markers = (THINK_START, "Thinking Process:")
    if any(marker.startswith(stripped) for marker in markers):
        return "wait"
    if any(stripped.startswith(marker) for marker in markers):
        return "reasoning"
    return "content"


async def _openai_sse_with_reasoning(body_iterator, force_reasoning: bool):
    in_reasoning = force_reasoning
    prefix_mode = "reasoning" if force_reasoning else "unknown"
    prefix_buffer = ""
    async for event in _iter_sse_data(body_iterator):
        if event == "[DONE]":
            yield "data: [DONE]\n\n"
            continue
        try:
            data = json.loads(event)
        except json.JSONDecodeError:
            yield f"data: {event}\n\n"
            continue

        for choice in data.get("choices") or []:
            delta = choice.get("delta")
            if not isinstance(delta, dict):
                continue
            content = delta.get("content")
            if not isinstance(content, str) or content == "":
                continue

            if prefix_mode == "unknown":
                prefix_buffer += content
                decision = _stream_prefix_decision(prefix_buffer)
                if decision == "wait":
                    delta["content"] = ""
                    continue
                if decision == "reasoning":
                    content = prefix_buffer
                    prefix_buffer = ""
                    prefix_mode = "reasoning"
                    in_reasoning = True
                else:
                    content = prefix_buffer
                    prefix_buffer = ""
                    prefix_mode = "content"

            if not in_reasoning and _looks_like_reasoning(content):
                in_reasoning = True
                prefix_mode = "reasoning"

            if in_reasoning:
                if THINK_END in content:
                    reasoning, final = content.split(THINK_END, 1)
                    reasoning = _clean_reasoning(reasoning)
                    in_reasoning = False
                    if reasoning:
                        delta["reasoning_content"] = reasoning
                        delta["reasoning"] = reasoning
                    delta["content"] = final.lstrip()
                else:
                    delta["reasoning_content"] = _clean_reasoning(content)
                    delta["reasoning"] = delta["reasoning_content"]
                    delta["content"] = ""
            elif THINK_END in content:
                reasoning, final = content.split(THINK_END, 1)
                reasoning = _clean_reasoning(reasoning)
                if reasoning:
                    delta["reasoning_content"] = reasoning
                    delta["reasoning"] = reasoning
                delta["content"] = final.lstrip()

        yield f"data: {json.dumps(data, ensure_ascii=False)}\n\n"


async def _iter_sse_data(body_iterator):
    buffer = ""
    async for raw in body_iterator:
        if isinstance(raw, bytes):
            buffer += raw.decode("utf-8", errors="replace")
        else:
            buffer += str(raw)
        while "\n\n" in buffer:
            event, buffer = buffer.split("\n\n", 1)
            for line in event.splitlines():
                if line.startswith("data:"):
                    yield line[5:].strip()
    if buffer.strip():
        for line in buffer.splitlines():
            if line.startswith("data:"):
                yield line[5:].strip()


def _anthropic_to_openai(payload: dict[str, Any], default_max_tokens: int) -> dict[str, Any]:
    model = payload.get("model")
    if not isinstance(model, str) or not model.strip():
        raise HTTPException(status_code=400, detail="Anthropic request requires model.")

    max_tokens = payload.get("max_tokens", default_max_tokens)
    if max_tokens is None:
        max_tokens = default_max_tokens

    messages = []
    system = payload.get("system")
    system_text = _anthropic_system_text(system)
    if system_text:
        messages.append({"role": "system", "content": system_text})

    for message in payload.get("messages") or []:
        if not isinstance(message, dict):
            continue
        role = message.get("role")
        if role not in {"user", "assistant"}:
            continue
        messages.append({"role": role, "content": _anthropic_content(message.get("content"))})

    thinking = payload.get("thinking")
    enable_thinking = payload.get("enable_thinking") is True
    thinking_budget = payload.get("thinking_budget")
    if isinstance(thinking, dict) and thinking.get("type") == "enabled":
        enable_thinking = True
        thinking_budget = thinking.get("budget_tokens", thinking_budget)
    elif isinstance(thinking, dict) and thinking.get("type") == "disabled":
        enable_thinking = False

    openai_payload: dict[str, Any] = {
        "model": model,
        "messages": messages,
        "max_tokens": int(max_tokens),
        "stream": payload.get("stream") is True,
        "enable_thinking": enable_thinking,
        "separate_reasoning": True,
    }
    for key in ("temperature", "top_p", "stop"):
        if key in payload:
            openai_payload[key] = payload[key]
    if thinking_budget is not None:
        openai_payload["thinking_budget"] = int(thinking_budget)
        openai_payload["thinking_start_token"] = THINK_START
        openai_payload["thinking_end_token"] = THINK_END
    return openai_payload


def _anthropic_system_text(system: Any) -> str:
    if isinstance(system, str):
        return system
    if isinstance(system, list):
        parts = []
        for block in system:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(str(block.get("text", "")))
        return "\n".join(part for part in parts if part)
    return ""


def _anthropic_content(content: Any) -> Any:
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return "" if content is None else str(content)

    blocks = []
    for block in content:
        if not isinstance(block, dict):
            continue
        block_type = block.get("type")
        if block_type == "text":
            blocks.append({"type": "text", "text": str(block.get("text", ""))})
        elif block_type == "image":
            image_url = _anthropic_image_url(block.get("source"))
            if image_url:
                blocks.append({"type": "image_url", "image_url": {"url": image_url}})
    return blocks


def _anthropic_image_url(source: Any) -> str:
    if not isinstance(source, dict):
        return ""
    source_type = source.get("type")
    if source_type == "url":
        return str(source.get("url", ""))
    if source_type == "base64":
        media_type = source.get("media_type", "image/jpeg")
        data = source.get("data", "")
        return f"data:{media_type};base64,{data}"
    return ""


def _openai_to_anthropic(data: dict[str, Any], request_payload: dict[str, Any]) -> dict[str, Any]:
    message = ((data.get("choices") or [{}])[0].get("message") or {})
    reasoning = message.get("reasoning_content") or message.get("reasoning") or ""
    content = message.get("content") or ""
    blocks = []
    if reasoning:
        blocks.append({"type": "thinking", "thinking": reasoning, "signature": ""})
    if content:
        blocks.append({"type": "text", "text": content})
    if not blocks:
        blocks.append({"type": "text", "text": ""})

    usage = data.get("usage") or {}
    return {
        "id": f"msg_{uuid.uuid4().hex}",
        "type": "message",
        "role": "assistant",
        "model": request_payload["model"],
        "content": blocks,
        "stop_reason": "end_turn",
        "stop_sequence": None,
        "usage": {
            "input_tokens": usage.get("input_tokens", usage.get("prompt_tokens", 0)),
            "output_tokens": usage.get("output_tokens", usage.get("completion_tokens", 0)),
        },
    }


async def _anthropic_stream(server_mod, openai_payload: dict[str, Any]):
    response = await server_mod.chat_completions_endpoint(
        server_mod.ChatRequest(**openai_payload)
    )
    message_id = f"msg_{uuid.uuid4().hex}"
    yield _anthropic_sse(
        "message_start",
        {
            "type": "message_start",
            "message": {
                "id": message_id,
                "type": "message",
                "role": "assistant",
                "model": openai_payload["model"],
                "content": [],
                "stop_reason": None,
                "stop_sequence": None,
                "usage": {"input_tokens": 0, "output_tokens": 0},
            },
        },
    )

    reasoning_open = False
    text_open = False
    text_index = 0
    force_reasoning = openai_payload.get("enable_thinking") is True
    in_reasoning = force_reasoning

    async for event in _iter_sse_data(response.body_iterator):
        if event == "[DONE]":
            break
        try:
            chunk = json.loads(event)
        except json.JSONDecodeError:
            continue
        for choice in chunk.get("choices") or []:
            delta = choice.get("delta") or {}
            text = delta.get("content")
            if not isinstance(text, str) or text == "":
                continue
            pieces = []
            if in_reasoning:
                if THINK_END in text:
                    reasoning, final = text.split(THINK_END, 1)
                    reasoning = _clean_reasoning(reasoning)
                    if reasoning:
                        pieces.append(("thinking", reasoning))
                    in_reasoning = False
                    if final.lstrip():
                        pieces.append(("text", final.lstrip()))
                else:
                    pieces.append(("thinking", _clean_reasoning(text)))
            else:
                pieces.append(("text", text))

            for kind, value in pieces:
                if value == "":
                    continue
                if kind == "thinking":
                    if not reasoning_open:
                        yield _anthropic_sse(
                            "content_block_start",
                            {
                                "type": "content_block_start",
                                "index": 0,
                                "content_block": {
                                    "type": "thinking",
                                    "thinking": "",
                                    "signature": "",
                                },
                            },
                        )
                        reasoning_open = True
                        text_index = 1
                    yield _anthropic_sse(
                        "content_block_delta",
                        {
                            "type": "content_block_delta",
                            "index": 0,
                            "delta": {"type": "thinking_delta", "thinking": value},
                        },
                    )
                else:
                    if reasoning_open:
                        yield _anthropic_sse(
                            "content_block_stop",
                            {"type": "content_block_stop", "index": 0},
                        )
                        reasoning_open = False
                    if not text_open:
                        yield _anthropic_sse(
                            "content_block_start",
                            {
                                "type": "content_block_start",
                                "index": text_index,
                                "content_block": {"type": "text", "text": ""},
                            },
                        )
                        text_open = True
                    yield _anthropic_sse(
                        "content_block_delta",
                        {
                            "type": "content_block_delta",
                            "index": text_index,
                            "delta": {"type": "text_delta", "text": value},
                        },
                    )

    if reasoning_open:
        yield _anthropic_sse("content_block_stop", {"type": "content_block_stop", "index": 0})
    if text_open:
        yield _anthropic_sse(
            "content_block_stop", {"type": "content_block_stop", "index": text_index}
        )
    yield _anthropic_sse(
        "message_delta",
        {
            "type": "message_delta",
            "delta": {"stop_reason": "end_turn", "stop_sequence": None},
            "usage": {"output_tokens": 0},
        },
    )
    yield _anthropic_sse("message_stop", {"type": "message_stop"})


def _anthropic_sse(event: str, data: dict[str, Any]) -> str:
    return f"event: {event}\ndata: {json.dumps(data, ensure_ascii=False)}\n\n"
