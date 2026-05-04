import asyncio
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
ANTHROPIC_VERSION = "2023-06-01"
ANTHROPIC_KEEPALIVE_SECONDS = 5.0
LOCAL_THINKING_SIGNATURE = "local-unsigned"


def install_compat(app, server_mod, default_max_tokens: int) -> None:
    """Install request/response compatibility shims around mlx_vlm.server."""

    _patch_default_thinking(server_mod)
    _install_openai_chat_routes(app, server_mod, default_max_tokens)

    @app.get("/", response_model=None, include_in_schema=False)
    @app.head("/", response_model=None, include_in_schema=False)
    async def root_health_endpoint():
        return JSONResponse(
            content={
                "status": "ok",
                "service": "mlx-vlm",
                "endpoints": [
                    "/v1/models",
                    "/v1/chat/completions",
                    "/v1/responses",
                    "/v1/messages",
                    "/health",
                ],
            }
        )

    @app.middleware("http")
    async def openai_compat_middleware(request: Request, call_next):
        if request.method != "POST" or request.url.path not in OPENAI_RESPONSE_PATHS:
            return await call_next(request)

        body = await request.body()
        payload = _loads_json_object(body)
        if payload is not None:
            _normalize_openai_request(
                payload,
                path=request.url.path,
                default_max_tokens=default_max_tokens,
            )
            body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        request = _request_with_body(request, body)

        try:
            response = await call_next(request)
        except Exception as exc:
            return _openai_error_response(str(exc), status_code=500)

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

        if _normalize_openai_error(data):
            return JSONResponse(
                content=data,
                status_code=response.status_code,
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
            return _anthropic_error_response(f"Invalid JSON: {exc}", status_code=400)
        if not isinstance(payload, dict):
            return _anthropic_error_response(
                "Anthropic request must be a JSON object.",
                status_code=400,
            )

        try:
            openai_payload = _anthropic_to_openai(payload, default_max_tokens)
        except HTTPException as exc:
            return _anthropic_error_response(
                _stringify_block_text(exc.detail),
                status_code=exc.status_code,
            )
        except Exception as exc:
            return _anthropic_error_response(str(exc), status_code=400)
        if openai_payload.get("stream") is True:
            return StreamingResponse(
                _anthropic_stream(
                    server_mod,
                    openai_payload,
                    request_payload=payload,
                ),
                media_type="text/event-stream",
                headers={
                    "Cache-Control": "no-cache",
                    "Connection": "keep-alive",
                    "X-Accel-Buffering": "no",
                },
            )

        try:
            result = await server_mod.chat_completions_endpoint(
                server_mod.ChatRequest(**openai_payload)
            )
        except HTTPException as exc:
            return _anthropic_error_response(
                _stringify_block_text(exc.detail),
                status_code=exc.status_code,
            )
        except Exception as exc:
            return _anthropic_error_response(str(exc), status_code=500)
        data = result.model_dump() if hasattr(result, "model_dump") else result
        _split_openai_reasoning(
            data,
            force_reasoning=openai_payload.get("enable_thinking") is True,
        )
        return JSONResponse(
            content=_openai_to_anthropic(data, openai_payload, request_payload=payload)
        )


def _install_openai_chat_routes(app, server_mod, default_max_tokens: int) -> None:
    app.router.routes = [
        route
        for route in app.router.routes
        if not (
            getattr(route, "path", None) in OPENAI_CHAT_PATHS
            and "POST" in getattr(route, "methods", set())
        )
    ]

    @app.post("/chat/completions", response_model=None, include_in_schema=False)
    @app.post("/v1/chat/completions", response_model=None, include_in_schema=False)
    async def openai_chat_completions_endpoint(request: Request):
        try:
            payload = await request.json()
        except json.JSONDecodeError as exc:
            return _openai_error_response(f"Invalid JSON: {exc}", status_code=400)
        if not isinstance(payload, dict):
            return _openai_error_response(
                "OpenAI chat request must be a JSON object.",
                status_code=400,
            )

        _normalize_openai_request(
            payload,
            path=request.url.path,
            default_max_tokens=default_max_tokens,
        )

        try:
            result = await server_mod.chat_completions_endpoint(
                server_mod.ChatRequest(**payload)
            )
        except HTTPException as exc:
            return _openai_error_response(
                _stringify_block_text(exc.detail),
                status_code=exc.status_code,
            )
        except Exception as exc:
            return _openai_error_response(str(exc), status_code=500)

        if payload.get("stream") is True:
            return StreamingResponse(
                _openai_sse_with_reasoning(
                    result.body_iterator,
                    payload.get("enable_thinking") is True,
                ),
                media_type="text/event-stream",
                headers={
                    "Cache-Control": "no-cache",
                    "Connection": "keep-alive",
                    "X-Accel-Buffering": "no",
                },
            )

        data = result.model_dump() if hasattr(result, "model_dump") else result
        if isinstance(data, dict):
            _split_openai_reasoning(
                data,
                force_reasoning=payload.get("enable_thinking") is True,
            )
            _normalize_openai_chat_response(data, model=str(payload.get("model", "")))
        return JSONResponse(content=data)


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


def _request_with_body(request: Request, body: bytes) -> Request:
    sent = False

    async def receive():
        nonlocal sent
        if sent:
            return {"type": "http.disconnect"}
        sent = True
        return {"type": "http.request", "body": body, "more_body": False}

    headers = [
        (key, value)
        for key, value in request.scope.get("headers", [])
        if key.lower() != b"content-length"
    ]
    headers.append((b"content-length", str(len(body)).encode("ascii")))
    request.scope["headers"] = headers
    request._body = body
    request._receive = receive
    return request


def _normalize_openai_request(
    payload: dict[str, Any],
    *,
    path: str,
    default_max_tokens: int,
) -> bool:
    changed = _apply_openai_defaults(
        payload,
        path=path,
        default_max_tokens=default_max_tokens,
    )
    if path in OPENAI_CHAT_PATHS:
        changed = _normalize_chat_messages(payload) or changed
    elif path in OPENAI_RESPONSE_PATHS:
        changed = _normalize_responses_input(payload) or changed
    return changed


def _apply_openai_defaults(
    payload: dict[str, Any],
    *,
    path: str,
    default_max_tokens: int,
) -> bool:
    changed = False

    thinking_budget = _payload_thinking_budget(payload)
    thinking_enabled = _payload_thinking_enabled(payload)
    if thinking_enabled is None and thinking_budget is not None:
        thinking_enabled = True

    if thinking_enabled is not None:
        if payload.get("enable_thinking") is not thinking_enabled:
            payload["enable_thinking"] = thinking_enabled
            changed = True
    elif "enable_thinking" not in payload:
        # Qwen3.5 defaults to thinking mode. Local agent routes should not leak
        # reasoning unless a caller explicitly opts in.
        payload["enable_thinking"] = False
        changed = True

    if payload.get("enable_thinking") is True:
        if payload.get("separate_reasoning") is not True:
            payload["separate_reasoning"] = True
            changed = True
        if payload.get("thinking_start_token") != THINK_START:
            payload["thinking_start_token"] = THINK_START
            changed = True
        if payload.get("thinking_end_token") != THINK_END:
            payload["thinking_end_token"] = THINK_END
            changed = True
        if thinking_budget is not None and payload.get("thinking_budget") != thinking_budget:
            payload["thinking_budget"] = thinking_budget
            changed = True
    else:
        for key in ("thinking_budget", "thinking_start_token", "thinking_end_token"):
            if key in payload:
                payload.pop(key, None)
                changed = True

    if default_max_tokens > 0:
        if path in OPENAI_CHAT_PATHS and "max_tokens" not in payload:
            payload["max_tokens"] = default_max_tokens
            changed = True
        if path in OPENAI_RESPONSE_PATHS and "max_output_tokens" not in payload:
            payload["max_output_tokens"] = default_max_tokens
            changed = True

    return changed


def _payload_thinking_enabled(payload: dict[str, Any]) -> bool | None:
    value = _coerce_boolish(payload.get("enable_thinking"))
    if value is not None:
        return value

    thinking = payload.get("thinking")
    value = _coerce_boolish(thinking)
    if value is not None:
        return value
    if isinstance(thinking, dict):
        thinking_type = str(thinking.get("type", "")).strip().lower()
        if thinking_type == "enabled":
            return True
        if thinking_type == "disabled":
            return False

    reasoning = payload.get("reasoning")
    if isinstance(reasoning, dict):
        effort = reasoning.get("effort")
        if isinstance(effort, str):
            return _reasoning_effort_enabled(effort)

    effort = payload.get("reasoning_effort")
    if isinstance(effort, str):
        return _reasoning_effort_enabled(effort)
    return None


def _coerce_boolish(value: Any) -> bool | None:
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        if value == 1:
            return True
        if value == 0:
            return False
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"1", "true", "yes", "y", "on", "enabled", "enable"}:
            return True
        if normalized in {"0", "false", "no", "n", "off", "disabled", "disable"}:
            return False
    return None


def _reasoning_effort_enabled(effort: str) -> bool:
    return effort.strip().lower() not in {
        "",
        "none",
        "off",
        "no",
        "false",
        "disabled",
        "disable",
    }


def _payload_thinking_budget(payload: dict[str, Any]) -> int | None:
    candidates = []
    thinking = payload.get("thinking")
    if isinstance(thinking, dict):
        candidates.append(thinking.get("budget_tokens"))
    reasoning = payload.get("reasoning")
    if isinstance(reasoning, dict):
        candidates.extend(
            [
                reasoning.get("budget_tokens"),
                reasoning.get("max_tokens"),
                reasoning.get("max_output_tokens"),
            ]
        )
    candidates.append(payload.get("thinking_budget"))

    for candidate in candidates:
        if isinstance(candidate, bool) or candidate is None:
            continue
        try:
            budget = int(candidate)
        except (TypeError, ValueError):
            continue
        if budget > 0:
            return budget
    return None


def _normalize_chat_messages(payload: dict[str, Any]) -> bool:
    messages = payload.get("messages")
    if not isinstance(messages, list):
        return False

    changed = False
    normalized_messages = []
    for message in messages:
        if not isinstance(message, dict):
            changed = True
            continue

        normalized = dict(message)
        role = str(normalized.get("role", "user"))
        content, content_changed = _normalize_openai_content(
            role,
            normalized.get("content"),
            for_responses=False,
        )
        normalized["content"] = content
        changed = changed or content_changed or normalized != message
        normalized_messages.append(normalized)

    if normalized_messages != messages:
        payload["messages"] = normalized_messages
        changed = True
    return changed


def _normalize_responses_input(payload: dict[str, Any]) -> bool:
    input_value = payload.get("input")
    if isinstance(input_value, str):
        return False
    if not isinstance(input_value, list):
        return False

    changed = False
    normalized_messages = []
    for message in input_value:
        if not isinstance(message, dict):
            changed = True
            continue
        normalized = dict(message)
        role = str(normalized.get("role", "user"))
        content, content_changed = _normalize_openai_content(
            role,
            normalized.get("content"),
            for_responses=True,
        )
        normalized["content"] = content
        changed = changed or content_changed or normalized != message
        normalized_messages.append(normalized)

    if normalized_messages != input_value:
        payload["input"] = normalized_messages
        changed = True
    return changed


def _normalize_openai_content(
    role: str,
    content: Any,
    *,
    for_responses: bool,
) -> tuple[Any, bool]:
    if isinstance(content, str):
        return content, False
    if content is None:
        return "", True
    if isinstance(content, dict):
        content = [content]

    if not isinstance(content, list):
        return str(content), True

    text_parts: list[str] = []
    media_parts: list[dict[str, Any]] = []
    changed = False

    for block in content:
        text, media, block_changed = _normalize_content_block(block, for_responses=for_responses)
        changed = changed or block_changed
        if text:
            text_parts.append(text)
        if media and role == "user":
            media_parts.append(media)

    text = "\n".join(part for part in text_parts if part)
    if media_parts and role == "user":
        blocks: list[dict[str, Any]] = []
        if text:
            blocks.append(
                {"type": "input_text" if for_responses else "text", "text": text}
            )
        blocks.extend(media_parts)
        return blocks, True
    return text, True if changed or not isinstance(content, str) else changed


def _normalize_content_block(
    block: Any,
    *,
    for_responses: bool,
) -> tuple[str, dict[str, Any] | None, bool]:
    if isinstance(block, str):
        return block, None, True
    if not isinstance(block, dict):
        return str(block), None, True

    block_type = str(block.get("type", "")).lower()
    if block_type in {"text", "input_text", "output_text"}:
        return _stringify_block_text(block.get("text", block.get("content", ""))), None, True
    if block_type in {"thinking", "reasoning", "redacted_thinking"}:
        return "", None, True
    if block_type in {"image_url", "input_image", "image"}:
        image_url = _openai_image_url_from_block(block)
        if image_url:
            if for_responses:
                return "", {"type": "input_image", "image_url": image_url}, True
            return "", {"type": "image_url", "image_url": {"url": image_url}}, True
        return "", None, True
    if "text" in block:
        return _stringify_block_text(block.get("text")), None, True
    if "content" in block:
        return _stringify_block_text(block.get("content")), None, True
    return "", None, True


def _stringify_block_text(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        parts = []
        for item in value:
            if isinstance(item, dict):
                item_type = str(item.get("type", "")).lower()
                if item_type in {"thinking", "reasoning", "redacted_thinking"}:
                    continue
                if "text" in item or "content" in item:
                    parts.append(_stringify_block_text(item.get("text", item.get("content"))))
            else:
                parts.append(str(item))
        return "\n".join(part for part in parts if part)
    if isinstance(value, dict):
        if "text" in value or "content" in value:
            return _stringify_block_text(value.get("text", value.get("content")))
        return json.dumps(value, ensure_ascii=False)
    return str(value)


def _openai_image_url_from_block(block: dict[str, Any]) -> str:
    if block.get("type") == "image":
        source_url = _anthropic_image_url(block.get("source"))
        if source_url:
            return source_url

    image_url = block.get("image_url", block.get("url"))
    if isinstance(image_url, dict):
        image_url = image_url.get("url")
    if image_url is None and isinstance(block.get("source"), dict):
        image_url = _anthropic_image_url(block.get("source"))
    return "" if image_url is None else str(image_url)


def _response_headers(headers) -> dict[str, str]:
    return {
        key: value
        for key, value in headers.items()
        if key.lower() not in {"content-length", "content-type"}
    }


def _openai_error_response(message: str, *, status_code: int) -> JSONResponse:
    return JSONResponse(
        content={"error": _openai_error_object(message)},
        status_code=status_code,
    )


def _normalize_openai_error(data: Any) -> bool:
    if not isinstance(data, dict):
        return False
    if isinstance(data.get("error"), str):
        data["error"] = _openai_error_object(data["error"])
        return True
    if isinstance(data.get("error"), dict):
        data["error"] = _openai_error_object(data["error"].get("message", ""), data["error"])
        return True
    if "detail" in data and "choices" not in data:
        data["error"] = _openai_error_object(_stringify_block_text(data.get("detail")))
        data.pop("detail", None)
        return True
    return False


def _openai_error_object(message: str, existing: dict[str, Any] | None = None) -> dict[str, Any]:
    existing = dict(existing or {})
    existing.setdefault("message", message or "Unknown error")
    existing.setdefault("type", "server_error")
    existing.setdefault("param", None)
    existing.setdefault("code", None)
    return existing


def _normalize_openai_chat_response(data: dict[str, Any], *, model: str) -> None:
    data.setdefault("id", f"chatcmpl-{uuid.uuid4()}")
    data.setdefault("object", "chat.completion")
    data.setdefault("created", int(time.time()))
    if model:
        data.setdefault("model", model)
    choices = data.get("choices")
    if not isinstance(choices, list):
        data["choices"] = []
        choices = data["choices"]
    for index, choice in enumerate(choices):
        if not isinstance(choice, dict):
            continue
        choice.setdefault("index", index)
        choice.setdefault("finish_reason", "stop")
        choice.setdefault("logprobs", None)
        message = choice.get("message")
        if isinstance(message, dict):
            message.setdefault("role", "assistant")
            message.setdefault("content", "")
            message.setdefault("tool_calls", [])
    data["usage"] = _openai_usage(data.get("usage"))


def _openai_usage(usage: Any) -> dict[str, int]:
    usage = usage if isinstance(usage, dict) else {}
    prompt_tokens = _int_value(usage.get("prompt_tokens"), usage.get("input_tokens"), 0)
    completion_tokens = _int_value(
        usage.get("completion_tokens"),
        usage.get("output_tokens"),
        0,
    )
    total_tokens = _int_value(
        usage.get("total_tokens"),
        prompt_tokens + completion_tokens,
    )
    normalized = dict(usage)
    normalized["prompt_tokens"] = prompt_tokens
    normalized["completion_tokens"] = completion_tokens
    normalized["total_tokens"] = total_tokens
    return normalized


def _int_value(*values: Any) -> int:
    for value in values:
        if isinstance(value, bool) or value is None:
            continue
        try:
            return int(value)
        except (TypeError, ValueError):
            continue
    return 0


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
        if _normalize_openai_error(data):
            yield f"data: {json.dumps(data, ensure_ascii=False)}\n\n"
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
                    reasoning = _clean_reasoning(content)
                    if reasoning:
                        delta["reasoning_content"] = reasoning
                        delta["reasoning"] = reasoning
                    delta["content"] = ""
            elif THINK_END in content:
                reasoning, final = content.split(THINK_END, 1)
                reasoning = _clean_reasoning(reasoning)
                if reasoning:
                    delta["reasoning_content"] = reasoning
                    delta["reasoning"] = reasoning
                delta["content"] = final.lstrip()

        if _openai_stream_chunk_has_visible_delta(data):
            yield f"data: {json.dumps(data, ensure_ascii=False)}\n\n"


def _openai_stream_chunk_has_visible_delta(data: Any) -> bool:
    if not isinstance(data, dict):
        return True
    choices = data.get("choices")
    if not isinstance(choices, list):
        return True

    for choice in choices:
        if not isinstance(choice, dict):
            continue
        if choice.get("finish_reason") is not None:
            return True

        delta = choice.get("delta")
        if not isinstance(delta, dict):
            continue
        if _delta_has_nonempty_text(delta):
            return True
        if delta.get("tool_calls"):
            return True
        if delta.get("function_call"):
            return True
    return False


def _delta_has_nonempty_text(delta: dict[str, Any]) -> bool:
    for key in ("content", "reasoning_content", "reasoning"):
        value = delta.get(key)
        if isinstance(value, str) and value.strip():
            return True
    return False


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

    source_messages = payload.get("messages")
    if not isinstance(source_messages, list):
        raise HTTPException(status_code=400, detail="Anthropic request requires messages.")

    max_tokens = _int_value(payload.get("max_tokens"), default_max_tokens)
    if max_tokens <= 0:
        raise HTTPException(status_code=400, detail="max_tokens must be greater than 0.")

    messages: list[dict[str, Any]] = []
    system_text = _anthropic_system_text(payload.get("system"))
    if system_text:
        messages.append({"role": "system", "content": system_text})

    for message in source_messages:
        if not isinstance(message, dict):
            continue
        messages.extend(_anthropic_message_to_openai(message))

    thinking = payload.get("thinking")
    enable_thinking = _coerce_boolish(payload.get("enable_thinking")) is True
    thinking_budget = payload.get("thinking_budget")
    if isinstance(thinking, dict):
        thinking_type = str(thinking.get("type", "")).lower()
        if thinking_type == "enabled":
            enable_thinking = True
            thinking_budget = thinking.get("budget_tokens", thinking_budget)
        elif thinking_type == "disabled":
            enable_thinking = False

    openai_payload: dict[str, Any] = {
        "model": model,
        "messages": messages,
        "max_tokens": max_tokens,
        "stream": payload.get("stream") is True,
        "enable_thinking": enable_thinking,
        "separate_reasoning": True,
    }
    for key in ("temperature", "top_p", "top_k"):
        if key in payload:
            openai_payload[key] = payload[key]
    if "stop_sequences" in payload:
        openai_payload["stop"] = payload["stop_sequences"]
    elif "stop" in payload:
        openai_payload["stop"] = payload["stop"]

    tools = _anthropic_tools_to_openai(payload.get("tools"))
    if tools:
        openai_payload["tools"] = tools
    tool_choice = _anthropic_tool_choice_to_openai(payload.get("tool_choice"))
    if tool_choice is not None:
        openai_payload["tool_choice"] = tool_choice

    if thinking_budget is not None:
        openai_payload["thinking_budget"] = _int_value(thinking_budget)
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


def _anthropic_message_to_openai(message: dict[str, Any]) -> list[dict[str, Any]]:
    role = message.get("role")
    if role not in {"user", "assistant"}:
        return []

    content = message.get("content")
    if isinstance(content, str):
        return [{"role": role, "content": content}]
    if not isinstance(content, list):
        return [{"role": role, "content": "" if content is None else str(content)}]

    if role == "assistant":
        text_parts: list[str] = []
        tool_calls: list[dict[str, Any]] = []
        for block in content:
            if not isinstance(block, dict):
                continue
            block_type = block.get("type")
            if block_type == "text":
                text_parts.append(str(block.get("text", "")))
            elif block_type == "tool_use":
                tool_calls.append(
                    {
                        "id": str(block.get("id") or f"toolu_{uuid.uuid4().hex}"),
                        "type": "function",
                        "function": {
                            "name": str(block.get("name", "")),
                            "arguments": json.dumps(
                                block.get("input") or {},
                                ensure_ascii=False,
                            ),
                        },
                    }
                )
        openai_message = {"role": "assistant", "content": "\n".join(text_parts)}
        if tool_calls:
            openai_message["tool_calls"] = tool_calls
        return [openai_message]

    user_blocks: list[dict[str, Any]] = []
    tool_messages: list[dict[str, Any]] = []
    for block in content:
        if not isinstance(block, dict):
            continue
        block_type = block.get("type")
        if block_type == "text":
            user_blocks.append({"type": "text", "text": str(block.get("text", ""))})
        elif block_type == "image":
            image_url = _anthropic_image_url(block.get("source"))
            if image_url:
                user_blocks.append({"type": "image_url", "image_url": {"url": image_url}})
        elif block_type == "tool_result":
            tool_messages.append(
                {
                    "role": "tool",
                    "tool_call_id": str(block.get("tool_use_id", "")),
                    "content": _anthropic_tool_result_text(block.get("content")),
                }
            )

    messages: list[dict[str, Any]] = []
    if user_blocks:
        text_only = all(block.get("type") == "text" for block in user_blocks)
        if text_only:
            messages.append(
                {
                    "role": "user",
                    "content": "\n".join(str(block.get("text", "")) for block in user_blocks),
                }
            )
        else:
            messages.append({"role": "user", "content": user_blocks})
    messages.extend(tool_messages)
    return messages


def _anthropic_tool_result_text(content: Any) -> str:
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return "" if content is None else str(content)
    parts = []
    for block in content:
        if isinstance(block, dict) and block.get("type") == "text":
            parts.append(str(block.get("text", "")))
        elif not isinstance(block, dict):
            parts.append(str(block))
    return "\n".join(part for part in parts if part)


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


def _anthropic_tools_to_openai(tools: Any) -> list[dict[str, Any]]:
    if not isinstance(tools, list):
        return []
    converted = []
    for tool in tools:
        if not isinstance(tool, dict):
            continue
        name = tool.get("name")
        if not isinstance(name, str) or not name:
            continue
        converted.append(
            {
                "type": "function",
                "function": {
                    "name": name,
                    "description": str(tool.get("description", "")),
                    "parameters": tool.get("input_schema")
                    if isinstance(tool.get("input_schema"), dict)
                    else {"type": "object", "properties": {}},
                },
            }
        )
    return converted


def _anthropic_tool_choice_to_openai(tool_choice: Any) -> Any:
    if tool_choice is None:
        return None
    if isinstance(tool_choice, str):
        return tool_choice
    if not isinstance(tool_choice, dict):
        return None
    choice_type = tool_choice.get("type")
    if choice_type == "auto":
        return "auto"
    if choice_type == "none":
        return "none"
    if choice_type == "any":
        return "required"
    if choice_type == "tool" and isinstance(tool_choice.get("name"), str):
        return {"type": "function", "function": {"name": tool_choice["name"]}}
    return None


def _openai_to_anthropic(
    data: dict[str, Any],
    openai_payload: dict[str, Any],
    *,
    request_payload: dict[str, Any],
) -> dict[str, Any]:
    choice = (data.get("choices") or [{}])[0]
    message = choice.get("message") or {}
    reasoning = message.get("reasoning_content") or message.get("reasoning") or ""
    content = message.get("content") or ""
    tool_calls = message.get("tool_calls") or []
    blocks = []
    if reasoning and _anthropic_show_thinking(request_payload):
        blocks.append(
            {
                "type": "thinking",
                "thinking": reasoning,
                "signature": LOCAL_THINKING_SIGNATURE,
            }
        )
    if content:
        blocks.append({"type": "text", "text": content})
    for tool_call in tool_calls if isinstance(tool_calls, list) else []:
        block = _openai_tool_call_to_anthropic(tool_call)
        if block is not None:
            blocks.append(block)
    if not blocks:
        blocks.append({"type": "text", "text": ""})

    usage = _anthropic_usage(data.get("usage"))
    stop_reason = _openai_finish_to_anthropic_stop(
        choice.get("finish_reason"),
        has_tool_use=any(block.get("type") == "tool_use" for block in blocks),
    )
    return {
        "id": f"msg_{uuid.uuid4().hex}",
        "type": "message",
        "role": "assistant",
        "model": openai_payload["model"],
        "content": blocks,
        "stop_reason": stop_reason,
        "stop_sequence": None,
        "usage": usage,
    }


async def _anthropic_stream(
    server_mod,
    openai_payload: dict[str, Any],
    *,
    request_payload: dict[str, Any],
):
    message_id = f"msg_{uuid.uuid4().hex}"
    try:
        response = await server_mod.chat_completions_endpoint(
            server_mod.ChatRequest(**openai_payload)
        )
    except Exception as exc:
        yield _anthropic_error_sse(str(exc))
        return

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
    tool_blocks_opened = False
    emitted_any_block = False
    text_index = 0
    force_reasoning = openai_payload.get("enable_thinking") is True
    in_reasoning = force_reasoning
    show_thinking = _anthropic_show_thinking(request_payload)
    finish_reason = "stop"
    usage = {"input_tokens": 0, "output_tokens": 0}
    tool_calls: dict[int, dict[str, Any]] = {}

    try:
        async for event_type, event in _iter_sse_data_with_keepalive(response.body_iterator):
            if event_type == "ping":
                yield _anthropic_sse("ping", {"type": "ping"})
                continue
            if event == "[DONE]":
                break
            try:
                chunk = json.loads(event)
            except json.JSONDecodeError:
                continue
            if _normalize_openai_error(chunk):
                yield _anthropic_error_sse(chunk["error"].get("message", "Unknown error"))
                return
            usage = _anthropic_usage(chunk.get("usage"), previous=usage)
            for choice in chunk.get("choices") or []:
                finish_reason = choice.get("finish_reason") or finish_reason
                delta = choice.get("delta") or {}
                _merge_openai_tool_call_deltas(tool_calls, delta.get("tool_calls"))
                pieces, in_reasoning = _openai_delta_to_anthropic_pieces(
                    delta,
                    in_reasoning=in_reasoning,
                    force_reasoning=force_reasoning,
                )

                for kind, value in pieces:
                    if kind == "thinking":
                        if not show_thinking:
                            continue
                        if not reasoning_open:
                            yield _anthropic_sse(
                                "content_block_start",
                                {
                                    "type": "content_block_start",
                                    "index": 0,
                                    "content_block": {
                                        "type": "thinking",
                                        "thinking": "",
                                        "signature": LOCAL_THINKING_SIGNATURE,
                                    },
                                },
                            )
                            reasoning_open = True
                            emitted_any_block = True
                            text_index = 1
                        yield _anthropic_sse(
                            "content_block_delta",
                            {
                                "type": "content_block_delta",
                                "index": 0,
                                "delta": {"type": "thinking_delta", "thinking": value},
                            },
                        )
                    elif kind == "text":
                        if reasoning_open:
                            yield _anthropic_signature_delta(0)
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
                            emitted_any_block = True
                        yield _anthropic_sse(
                            "content_block_delta",
                            {
                                "type": "content_block_delta",
                                "index": text_index,
                                "delta": {"type": "text_delta", "text": value},
                            },
                        )
    except asyncio.CancelledError:
        raise
    except Exception as exc:
        yield _anthropic_error_sse(str(exc))
        return

    if reasoning_open:
        yield _anthropic_signature_delta(0)
        yield _anthropic_sse("content_block_stop", {"type": "content_block_stop", "index": 0})
    if text_open:
        yield _anthropic_sse(
            "content_block_stop", {"type": "content_block_stop", "index": text_index}
        )
    next_index = (text_index + 1) if text_open else (1 if emitted_any_block else 0)
    for tool_call in tool_calls.values():
        tool_block = _openai_tool_call_to_anthropic(tool_call)
        if tool_block is None:
            continue
        tool_blocks_opened = True
        emitted_any_block = True
        yield _anthropic_sse(
            "content_block_start",
            {
                "type": "content_block_start",
                "index": next_index,
                "content_block": {
                    "type": "tool_use",
                    "id": tool_block["id"],
                    "name": tool_block["name"],
                    "input": {},
                },
            },
        )
        yield _anthropic_sse(
            "content_block_delta",
            {
                "type": "content_block_delta",
                "index": next_index,
                "delta": {
                    "type": "input_json_delta",
                    "partial_json": json.dumps(tool_block["input"], ensure_ascii=False),
                },
            },
        )
        yield _anthropic_sse("content_block_stop", {"type": "content_block_stop", "index": next_index})
        next_index += 1
    if not emitted_any_block:
        yield _anthropic_sse(
            "content_block_start",
            {
                "type": "content_block_start",
                "index": 0,
                "content_block": {"type": "text", "text": ""},
            },
        )
        yield _anthropic_sse("content_block_stop", {"type": "content_block_stop", "index": 0})
    yield _anthropic_sse(
        "message_delta",
        {
            "type": "message_delta",
            "delta": {
                "stop_reason": _openai_finish_to_anthropic_stop(
                    finish_reason,
                    has_tool_use=tool_blocks_opened,
                ),
                "stop_sequence": None,
            },
            "usage": {"output_tokens": usage["output_tokens"]},
        },
    )
    yield _anthropic_sse("message_stop", {"type": "message_stop"})


def _anthropic_sse(event: str, data: dict[str, Any]) -> str:
    return f"event: {event}\ndata: {json.dumps(data, ensure_ascii=False)}\n\n"


def _anthropic_error_response(message: str, *, status_code: int) -> JSONResponse:
    return JSONResponse(
        content={
            "type": "error",
            "error": {
                "type": "invalid_request_error" if status_code < 500 else "api_error",
                "message": message or "Unknown error",
            },
        },
        status_code=status_code,
    )


def _anthropic_error_sse(message: str) -> str:
    return _anthropic_sse(
        "error",
        {
            "type": "error",
            "error": {"type": "api_error", "message": message or "Unknown error"},
        },
    )


def _anthropic_signature_delta(index: int) -> str:
    return _anthropic_sse(
        "content_block_delta",
        {
            "type": "content_block_delta",
            "index": index,
            "delta": {
                "type": "signature_delta",
                "signature": LOCAL_THINKING_SIGNATURE,
            },
        },
    )


def _anthropic_show_thinking(payload: dict[str, Any]) -> bool:
    thinking = payload.get("thinking")
    if isinstance(thinking, dict):
        return str(thinking.get("display", "")).lower() != "omitted"
    return True


def _anthropic_usage(usage: Any, previous: dict[str, int] | None = None) -> dict[str, int]:
    previous = previous or {"input_tokens": 0, "output_tokens": 0}
    usage = usage if isinstance(usage, dict) else {}
    input_tokens = _int_value(
        usage.get("input_tokens"),
        usage.get("prompt_tokens"),
        previous.get("input_tokens"),
    )
    output_tokens = _int_value(
        usage.get("output_tokens"),
        usage.get("completion_tokens"),
        previous.get("output_tokens"),
    )
    return {"input_tokens": input_tokens, "output_tokens": output_tokens}


def _openai_finish_to_anthropic_stop(finish_reason: Any, *, has_tool_use: bool) -> str:
    if has_tool_use:
        return "tool_use"
    if finish_reason == "length":
        return "max_tokens"
    if finish_reason in {"content_filter", "safety"}:
        return "stop_sequence"
    return "end_turn"


def _openai_delta_to_anthropic_pieces(
    delta: dict[str, Any],
    *,
    in_reasoning: bool,
    force_reasoning: bool,
) -> tuple[list[tuple[str, str]], bool]:
    pieces: list[tuple[str, str]] = []
    reasoning = delta.get("reasoning_content") or delta.get("reasoning")
    if isinstance(reasoning, str) and reasoning.strip():
        pieces.append(("thinking", reasoning))

    text = delta.get("content")
    if not isinstance(text, str) or text == "":
        return pieces, in_reasoning

    if force_reasoning and in_reasoning:
        if THINK_END in text:
            reasoning_text, final = text.split(THINK_END, 1)
            reasoning_text = _clean_reasoning(reasoning_text)
            if reasoning_text:
                pieces.append(("thinking", reasoning_text))
            final = final.lstrip()
            if final:
                pieces.append(("text", final))
            return pieces, False
        reasoning_text = _clean_reasoning(text)
        if reasoning_text:
            pieces.append(("thinking", reasoning_text))
        return pieces, True

    if THINK_END in text:
        reasoning_text, final = text.split(THINK_END, 1)
        reasoning_text = _clean_reasoning(reasoning_text)
        if reasoning_text:
            pieces.append(("thinking", reasoning_text))
        final = final.lstrip()
        if final:
            pieces.append(("text", final))
        return pieces, False

    if text.lstrip().startswith(THINK_START) or _looks_like_reasoning(text):
        reasoning_text = _clean_reasoning(text)
        if reasoning_text:
            pieces.append(("thinking", reasoning_text))
        return pieces, True

    pieces.append(("text", text))
    return pieces, in_reasoning


def _merge_openai_tool_call_deltas(
    accumulator: dict[int, dict[str, Any]],
    tool_calls: Any,
) -> None:
    if not isinstance(tool_calls, list):
        return
    for fallback_index, tool_call in enumerate(tool_calls):
        if not isinstance(tool_call, dict):
            continue
        index = _int_value(tool_call.get("index"), fallback_index)
        current = accumulator.setdefault(index, {"type": "function", "function": {}})
        if tool_call.get("id"):
            current["id"] = tool_call["id"]
        if tool_call.get("type"):
            current["type"] = tool_call["type"]
        function = tool_call.get("function")
        if isinstance(function, dict):
            target_function = current.setdefault("function", {})
            if function.get("name"):
                target_function["name"] = function["name"]
            if function.get("arguments") is not None:
                previous = target_function.get("arguments", "")
                target_function["arguments"] = previous + str(function.get("arguments", ""))


def _openai_tool_call_to_anthropic(tool_call: Any) -> dict[str, Any] | None:
    if not isinstance(tool_call, dict):
        return None
    function = tool_call.get("function")
    if not isinstance(function, dict):
        return None
    name = function.get("name")
    if not isinstance(name, str) or not name:
        return None
    return {
        "type": "tool_use",
        "id": str(tool_call.get("id") or f"toolu_{uuid.uuid4().hex}"),
        "name": name,
        "input": _json_object(function.get("arguments")),
    }


def _json_object(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        return value
    if not isinstance(value, str) or not value.strip():
        return {}
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError:
        return {"arguments": value}
    return parsed if isinstance(parsed, dict) else {"value": parsed}


async def _iter_sse_data_with_keepalive(body_iterator):
    queue: asyncio.Queue[tuple[str, Any]] = asyncio.Queue()

    async def producer():
        try:
            async for event in _iter_sse_data(body_iterator):
                await queue.put(("event", event))
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            await queue.put(("error", exc))
        finally:
            await queue.put(("done", None))

    task = asyncio.create_task(producer())
    try:
        while True:
            try:
                kind, value = await asyncio.wait_for(
                    queue.get(),
                    timeout=ANTHROPIC_KEEPALIVE_SECONDS,
                )
            except asyncio.TimeoutError:
                yield "ping", None
                continue
            if kind == "event":
                yield "event", value
            elif kind == "error":
                raise value
            else:
                break
    finally:
        if not task.done():
            task.cancel()
