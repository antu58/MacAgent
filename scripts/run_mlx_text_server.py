#!/usr/bin/env python3
import os
from pathlib import Path


def _get_int_env(name: str, default: int) -> int:
    value = os.environ.get(name)
    if value is None or value == "":
        return default
    return int(value)


def main() -> None:
    max_kv_size = _get_int_env("MAX_KV_SIZE", 131072)

    # mlx_lm.server imports make_prompt_cache into its own module namespace, so
    # override that binding before entering main() to enforce a bounded KV cache.
    from mlx_lm import server as server_mod
    from mlx_lm.models import cache as cache_mod

    original_make_prompt_cache = server_mod.make_prompt_cache

    def bounded_make_prompt_cache(model, explicit_max_kv_size=None):
        resolved = explicit_max_kv_size
        if resolved is None and max_kv_size > 0:
            resolved = max_kv_size
        return cache_mod.make_prompt_cache(model, resolved)

    server_mod.make_prompt_cache = bounded_make_prompt_cache

    if Path(__file__).name == "run_mlx_text_server.py":
        server_mod.main()
    else:
        # Defensive fallback in case the file is copied/renamed.
        server_mod.make_prompt_cache = original_make_prompt_cache
        raise RuntimeError("Unexpected launcher path for MLX text server wrapper")


if __name__ == "__main__":
    main()
