# Model Services

This directory is the root-level entrypoint for local model services.

Run the script named after the model you want:

```bash
./model-services/qwen-0.8b-vlm
./model-services/qwen-4b-vlm
./model-services/qwen-9b-vlm
```

Default action is `start`. Starting a service first stops the previous process
for the same model/port, then starts a fresh background process.

## Services

| Script | Model | Endpoint | Intent |
| --- | --- | --- | --- |
| `qwen-0.8b-vlm` | `mlx-community/Qwen3.5-0.8B-MLX-8bit` | `http://127.0.0.1:18081/v1` | Fast low-precision intent/classification service |
| `qwen-4b-vlm` | `mlx-community/Qwen3.5-4B-MLX-8bit` | `http://127.0.0.1:18082/v1` | Vision-language service |
| `qwen-9b-vlm` | `mlx-community/Qwen3.5-9B-MLX-8bit` | `http://127.0.0.1:18083/v1` | High-precision vision-language service |

## Actions

```bash
./model-services/qwen-4b-vlm start       # default; stop old process, then start in background
./model-services/qwen-4b-vlm status      # show PID, endpoint, and log path
./model-services/qwen-4b-vlm stop        # stop the background service
./model-services/qwen-4b-vlm restart     # stop then start
./model-services/qwen-4b-vlm setup       # install dependencies and download/reuse weights
./model-services/qwen-4b-vlm foreground  # run in the current shell for debugging
./model-services/qwen-4b-vlm logs        # list log files
./model-services/qwen-4b-vlm tail        # follow the newest log file
```

Aliases:

- `run` and `serve` mean `start`
- `dev` means `foreground`
- `log` means `logs`
- `help`, `-h`, and `--help` print usage

The older per-model scripts under `Qwen_*/scripts/` remain implementation
details. Use this directory when you want clear root-level intent: choose a
model by script name and an operation by action name.
