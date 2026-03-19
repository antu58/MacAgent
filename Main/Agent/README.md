# Agent (macOS) Project Notes

## Current Inference Architecture

- App UI: `Main/Agent/Agent`
- Inference bridge: `Main/Agent/InferenceService` (XPC Service)
- Mode: XPC only

## Model Source (Current)

- Model ID: `mlx-community/Qwen3.5-0.8B-8bit`
- Family: Qwen 3.5
- Parameter scale: `0.8B`
- Quantization: `8-bit`
- Runtime target: MLX (`mlx_lm.server`)
- Model is **not embedded in app bundle**.
- User selects local model folder in Settings.
- App imports/copies the selected model into App Group managed store:
  `App Group Container/Library/Application Support/Models/ImportedModel`
- XPC always reads from this managed path (no bookmark dependency).
- Project-local model staging path (ignored by git):
  `Main/Agent/LocalModels/Qwen3.5-0.8B-8bit`

## Embedded Runtime (Current)

- Runtime mode: bundled Python + MLX server bootstrap (started by XPC)
- Bundled runtime archive path:
  `Main/Agent/InferenceService/EmbeddedRuntime/venv.tar.gz`
- First-launch extraction path (sandbox):
  `App Group Container/Library/Application Support/InferenceService/EmbeddedRuntime/venv`
- Server module: `mlx_lm.server`
- Startup behavior: App launch warms XPC, XPC auto-starts backend if not running

## Git Policy For Model Files

Large local model/runtime directories are intentionally ignored in git due to size.
Ignore rule is defined in repository root `.gitignore`.
