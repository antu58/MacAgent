# Qwen3.5 0.8B (MLX 8bit) 本地部署记录

更新时间：2026-03-19

## 当前状态

- 已安装 Homebrew：`/opt/homebrew/bin/brew`
- 已安装 Python 3.11：`/opt/homebrew/bin/python3.11`
- 已完成模型与依赖初始化：`mlx-community/Qwen3.5-0.8B-8bit`
- 已启用持久化缓存目录：`/Users/zhangfeng/.mlx-qwen35`
- 已取消自动启动（不使用 launchd），改为项目内脚本手动控制

## 架构说明（与你需求一致）

- 模型服务使用 Apple MLX 原生服务：`mlx_lm.server`
- 模型版本：`Qwen3.5 0.8B 8bit (MLX Community)`
- 缓存持久化在宿主机目录，重启服务不会重复下载模型
- Docker 内应用通过 `host.docker.internal` 调用宿主机服务

## 目录与脚本

- `scripts/common.env`：统一配置（模型名、端口、缓存目录等）
- `scripts/setup_mlx_service.sh`：初始化环境、安装依赖、预下载模型
- `scripts/start_mlx_service.sh`：前台启动服务（调试用）
- `scripts/start_service.sh`：后台启动服务（项目内 PID 管理）
- `scripts/stop_service.sh`：停止后台服务
- `scripts/status_service.sh`：查看后台服务状态

## 一次性初始化

```bash
cd /Users/zhangfeng/Desktop/Project/Apple/MacAgent/Qwen_0_8_B
chmod +x scripts/*.sh
./scripts/setup_mlx_service.sh
```

## 日常启动/停止

```bash
# 后台启动（若有旧实例会自动先停止再启动新实例）
./scripts/start_service.sh

# 查看状态
./scripts/status_service.sh

# 停止服务
./scripts/stop_service.sh
```

默认接口：

- `http://127.0.0.1:18080/v1`

运行日志与 PID：

- 日志：`/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Qwen_0_8_B/run/qwen35-mlx.log`
- PID：`/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Qwen_0_8_B/run/qwen35-mlx.pid`

## 连通性验证

```bash
curl -s -X POST http://127.0.0.1:18080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "mlx-community/Qwen3.5-0.8B-8bit",
    "messages": [{"role": "user", "content": "你好，回复ok"}],
    "max_tokens": 16
  }'
```

## Docker 中调用方式

容器内基地址使用：

- `http://host.docker.internal:18080/v1`

示例（OpenAI 兼容 SDK 的 base_url）：

- `http://host.docker.internal:18080/v1`

## 关键持久化配置

`scripts/common.env` 中默认值：

- `MLX_SERVICE_HOME=/Users/zhangfeng/.mlx-qwen35`
- `VENV_DIR=/Users/zhangfeng/.mlx-qwen35/venv`
- `HF_HOME=/Users/zhangfeng/.mlx-qwen35/hf`
- `HUGGINGFACE_HUB_CACHE=/Users/zhangfeng/.mlx-qwen35/hf/hub`
- `TRANSFORMERS_CACHE=/Users/zhangfeng/.mlx-qwen35/hf/transformers`

只要保留该目录，服务重启不会重复下载模型。
