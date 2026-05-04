# MacAgent

当前仓库里已经落地了一套可运行的 `Gateway + GatewayWeb` 双层链路，并且正在向统一的“意图分发协议”收敛。

## 当前组件

- `Gateway`
  - 负责意图分类、执行模型分发、机械校验、重试、降级
- `GatewayWeb`
  - 负责网页体验、自定义意图注入、日志查看

默认端口：

- Gateway: `19090`
- GatewayWeb: `19091`

## 本地模型服务与接口格式

本地模型统一从根目录的 `model-services/` 启动。比如启动 4B VLM：

```bash
cd /Users/zhangfeng/Desktop/Project/Apple/MacAgent
./model-services/qwen-4b-vlm start
./model-services/qwen-4b-vlm status
./model-services/qwen-4b-vlm stop
```

可用模型服务：

| 脚本 | 模型 | OpenAI Base URL | 用途 |
| --- | --- | --- | --- |
| `./model-services/qwen-0.8b-vlm` | `mlx-community/Qwen3.5-0.8B-MLX-8bit` | `http://127.0.0.1:18081/v1` | 低延迟意图/分类 |
| `./model-services/qwen-4b-vlm` | `mlx-community/Qwen3.5-4B-MLX-8bit` | `http://127.0.0.1:18082/v1` | 通用 VLM |
| `./model-services/qwen-9b-vlm` | `mlx-community/Qwen3.5-9B-MLX-8bit` | `http://127.0.0.1:18083/v1` | 高精度 VLM |

如果 Cherry Studio 报 `Failed to fetch`，先确认服务真的在监听：

```bash
./model-services/qwen-4b-vlm status
curl http://127.0.0.1:18082/v1/models
```

### OpenAI Compatible

OpenAI 兼容格式支持：

- `GET /`
- `HEAD /`
- `GET /v1/models`
- `GET /models`
- `POST /v1/chat/completions`
- `POST /chat/completions`
- `POST /v1/responses`
- `POST /responses`
- `GET /health`
- `POST /unload`

Cherry Studio 使用 OpenAI Compatible 时，4B VLM 示例配置：

- Base URL: `http://127.0.0.1:18082/v1`
- API Key: `sk-local`
- Model: `mlx-community/Qwen3.5-4B-MLX-8bit`

Chat Completions 示例：

```json
{
  "model": "mlx-community/Qwen3.5-4B-MLX-8bit",
  "messages": [
    {"role": "user", "content": "请回答：1+1等于几？"}
  ],
  "stream": true
}
```

OpenAI 格式下，默认不启用 thinking。需要思考模式时可传任一兼容写法：

```json
{
  "enable_thinking": true,
  "thinking_budget": 512
}
```

也兼容 Cherry/常见客户端可能传入的 `enable_thinking: "true"`、`thinking: {"type": "enabled", "budget_tokens": 512}`、`reasoning_effort`、`reasoning: {"effort": "medium"}` 等形式。

thinking 会和最终答案分离：

- 非流式：最终答案在 `choices[0].message.content`，思考内容在 `choices[0].message.reasoning_content` 和 `choices[0].message.reasoning`
- 流式：最终答案在 `choices[].delta.content`，思考内容在 `choices[].delta.reasoning_content` 和 `choices[].delta.reasoning`

服务端会过滤空的 thinking/content 流片段；只保留有实际文本的 delta，以及正常的 `finish_reason` 结束包。

### Anthropic Messages

同一套模型服务也支持 Anthropic Messages 格式：

- `POST /v1/messages`
- `POST /messages`

Cherry Studio 使用 Anthropic 格式时，4B VLM 示例配置：

- Base URL: `http://127.0.0.1:18082`
- API Key: `sk-local`
- Model: `mlx-community/Qwen3.5-4B-MLX-8bit`

Anthropic thinking 示例：

```json
{
  "model": "mlx-community/Qwen3.5-4B-MLX-8bit",
  "messages": [
    {"role": "user", "content": "请回答：1+1等于几？"}
  ],
  "thinking": {"type": "enabled", "budget_tokens": 512},
  "max_tokens": 1024,
  "stream": true
}
```

## 协议主线

项目当前要统一到这条主线：

1. `Gateway` 先合并默认意图与请求意图
2. 使用低精度模型做意图分类
3. 未命中意图时，内部走 `deepmodel`
4. 命中意图后，按该意图声明的 `model_type` 选择执行模型
5. 执行模型只围绕当前意图抽参
6. `Gateway` 只做机械校验
7. 失败时重试一次，仍失败则降级 `deepmodel`
8. 但对前端只返回两类正式结果：`ask_user` 和 `result`

## 参数规范结论

当前推荐的规范是：

- 参数对象只保留结构约束
- 分类边界放进 `route_description`
- 参数语义不再依赖 `params[].description` 作为长期标准
- 参数含义、例子、常见场景、特殊处理，统一上收至 `execution_description`

也就是：

- `params[]` 回答“参数长什么样”
- `route_description` 回答“这个意图什么时候命中”
- `execution_description` 回答“参数意味着什么、怎么理解”

## 核心文档

- 协议主文档：[Gateway/INTENT_PROTOCOL.md](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/INTENT_PROTOCOL.md)
- Gateway 说明：[Gateway/README.md](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/README.md)
- GatewayWeb 说明：[GatewayWeb/README.md](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/GatewayWeb/README.md)
- 默认意图文件：[Gateway/default_intents.json](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/default_intents.json)

## 一句话理解

- 网关负责协议
- 意图负责语义
- 执行模型负责当前意图的具体理解
- `deepmodel` 负责兜底
- 前端不需要感知内部到底用了哪个模型
