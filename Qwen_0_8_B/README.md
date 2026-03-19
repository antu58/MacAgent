# Qwen_0_8_B 项目说明

更新时间：2026-03-19

## 项目目标

本项目用于在本地运行 **Qwen3.5 0.8B (MLX 8bit)** 模型服务，提供 OpenAI 兼容接口：

- 服务地址：`http://127.0.0.1:18080/v1`
- 当前仅包含 `0.8B` 模型服务能力
- 后续会在此基础上继续补充更多模型与功能模块

## 环境需求

1. 操作系统：`macOS`（推荐 Apple Silicon，MLX 原生运行）
2. Python：`3.11+`（已验证 `3.11.15`）
3. Homebrew：用于安装 Python（可选但推荐）
4. 网络：首次初始化需要下载 `mlx-lm` 依赖和模型文件
5. 磁盘：至少预留 `5GB+`（模型缓存、虚拟环境、日志）

## 快速开始

```bash
cd /Users/zhangfeng/Desktop/Project/Apple/MacAgent/Qwen_0_8_B
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

1. `scripts/`：服务脚本与测试脚本目录
   - `common.env`：统一配置（模型名、端口、缓存路径）
   - `setup_mlx_service.sh`：初始化依赖并预下载模型
   - `start_mlx_service.sh`：前台启动 MLX 服务
   - `start_service.sh`：后台启动（会自动停止旧实例再启动）
   - `stop_service.sh`：停止后台服务
   - `status_service.sh`：查看服务状态
   - `test_tool_call_accuracy.sh`：工具调用准确率 + 性能指标测试入口
   - `tool_call_benchmark.py`：性能统计实现（TTFT/总耗时/tokens/s）

2. `run/`：运行时产物目录
   - `qwen35-mlx.log`：服务日志
   - `qwen35-mlx.pid`：后台进程 PID
   - `tool_call_benchmark_*.jsonl`：工具调用测试原始报告

3. `README.md`：项目说明文档（本文件）

4. `TOOL_CALL_CASES.md`：工具调用案例与测试说明

## 缓存与持久化

默认缓存根目录（在 `scripts/common.env` 中配置）：

- `MLX_SERVICE_HOME=/Users/zhangfeng/.mlx-qwen35`
- `VENV_DIR=/Users/zhangfeng/.mlx-qwen35/venv`
- `HF_HOME=/Users/zhangfeng/.mlx-qwen35/hf`
- `HUGGINGFACE_HUB_CACHE=/Users/zhangfeng/.mlx-qwen35/hf/hub`

只要保留上述目录，重启服务不会重复下载模型。

## Docker 调用方式

如需在 Docker 容器内访问本机服务，请使用：

- `http://host.docker.internal:18080/v1`

## 当前范围与后续补充

当前范围：

- 仅支持 `Qwen3.5-0.8B-8bit (MLX)` 本地服务

后续计划（待补充）：

- 更多模型版本（如更大参数量）
- 更完整的基准测试与回归报告
- 服务编排与多模型切换能力
