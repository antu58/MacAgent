# Qwen_4_B_VLM 项目说明

更新时间：2026-03-19

## 项目目标

本项目用于在本地运行 **Qwen3.5 4B VLM (MLX 8bit)** 模型服务，提供 OpenAI 兼容接口，支持图文输入：

- 服务地址：`http://127.0.0.1:18082/v1`
- 当前包含 `4B VLM` 模型服务能力
- 部署方式与现有 `Qwen_0_8_B` / `Qwen_9_B` 保持一致

## 环境需求

1. 操作系统：`macOS`（推荐 Apple Silicon）
2. Python：`3.11+`
3. 网络：首次初始化需要下载 `mlx-vlm[torch]` 依赖和模型文件
4. 磁盘：至少预留 `10GB+`（模型缓存、虚拟环境、日志）

## 快速开始

```bash
cd /Users/zhangfeng/Desktop/Project/Apple/MacAgent/Qwen_4_B_VLM
chmod +x scripts/*.sh
./scripts/setup_mlx_service.sh
./scripts/start_service.sh
./scripts/status_service.sh
```

停止服务：

```bash
./scripts/stop_service.sh
```

## 目录说明

1. `scripts/`
   - `common.env`：统一配置（模型名、端口、缓存路径）
   - `setup_mlx_service.sh`：初始化依赖并预下载模型
   - `start_mlx_service.sh`：前台启动 VLM 服务
   - `start_service.sh`：后台启动（自动停止旧实例）
   - `stop_service.sh`：停止后台服务
   - `status_service.sh`：查看服务状态
   - `test_image_chat.sh`：发送一条图文请求验证服务能力

2. `run/`
   - `qwen35-4b-vlm.log`：服务日志
   - `qwen35-4b-vlm.pid`：后台进程 PID

## 默认配置

- `MODEL_ID=mlx-community/Qwen3.5-4B-MLX-8bit`
- `MLX_SERVICE_HOME=/Users/zhangfeng/.mlx-qwen35-4b-vlm`
- `PORT=18082`

## 图像请求测试

```bash
./scripts/test_image_chat.sh
```

也可以自定义 endpoint 和图片 URL / 本地图片绝对路径：

```bash
./scripts/test_image_chat.sh http://127.0.0.1:18082/v1/chat/completions https://images.cocodataset.org/val2017/000000039769.jpg
./scripts/test_image_chat.sh http://127.0.0.1:18082/v1/chat/completions /Users/zhangfeng/Desktop/截屏2026-03-19\ 12.45.04.png
```
