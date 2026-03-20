#!/usr/bin/env python3
import argparse
import json
import os

import uvicorn
from starlette.requests import Request


def main():
    parser = argparse.ArgumentParser(description="Qwen 4B VLM local server wrapper")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=18082)
    parser.add_argument("--trust-remote-code", action="store_true")
    parser.add_argument("--prefill-step-size", type=int, default=3072)
    parser.add_argument("--kv-bits", type=int, default=0)
    parser.add_argument("--kv-group-size", type=int, default=64)
    parser.add_argument("--max-kv-size", type=int, default=0)
    parser.add_argument("--quantized-kv-start", type=int, default=5000)
    args = parser.parse_args()

    if args.trust_remote_code:
        os.environ["MLX_TRUST_REMOTE_CODE"] = "true"
    os.environ["PREFILL_STEP_SIZE"] = str(args.prefill_step_size)
    os.environ["KV_BITS"] = str(args.kv_bits)
    os.environ["KV_GROUP_SIZE"] = str(args.kv_group_size)
    os.environ["MAX_KV_SIZE"] = str(args.max_kv_size)
    os.environ["QUANTIZED_KV_START"] = str(args.quantized_kv_start)

    from mlx_vlm import server as server_mod

    app = server_mod.app

    @app.middleware("http")
    async def disable_thinking_by_default(request: Request, call_next):
        if request.method == "POST" and request.url.path in {
            "/chat/completions",
            "/v1/chat/completions",
            "/responses",
            "/v1/responses",
        }:
            body = await request.body()
            if body:
                try:
                    payload = json.loads(body)
                except json.JSONDecodeError:
                    payload = None
                if isinstance(payload, dict) and "enable_thinking" not in payload:
                    payload["enable_thinking"] = False
                    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")

                    async def receive():
                        return {
                            "type": "http.request",
                            "body": body,
                            "more_body": False,
                        }

                    request._receive = receive

        return await call_next(request)

    uvicorn.run(app, host=args.host, port=args.port, workers=1, reload=False)


if __name__ == "__main__":
    main()
