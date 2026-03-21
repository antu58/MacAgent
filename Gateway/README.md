# AI Gateway (Go)

`Gateway` 是这套系统里的协议中心，当前职责应理解为：

- 合并默认意图与请求意图
- 使用低精度模型做意图分类
- 按命中的意图选择对应执行模型
- 向执行模型注入统一参数规则与意图说明
- 对执行结果做机械校验、重试、降级
- 对前端只输出统一的 `ask_user | result` 两类结果

当前项目的正式协议说明见：

- [INTENT_PROTOCOL.md](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/INTENT_PROTOCOL.md)

## 当前文档口径

这份 README 只做入口说明，详细规范以协议文档为准。

当前推荐理解是：

- `Gateway` 负责分类、分发、机械校验
- 具体意图的理解，交给对应执行模型
- `deepmodel` 和模型选择属于内部执行过程，不属于前端返回协议
- 参数对象尽量简洁
- `route_description` 负责分类边界
- `execution_description` 负责参数语义、例子、特殊情况
- `time + time_period` 属于网关共享参数规范：`time` 只保留格式化时钟字符串，`time_period` 单独表达上午/下午/晚上/24小时制
- 默认意图由 [default_intents.json](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/default_intents.json) 提供
- 网页请求仍可注入自定义意图覆盖同名默认意图

## 对外接口

- `GET /healthz`
- `POST /route`
- `POST /chat`

## 飞书 Socket 通道

Gateway 现在支持作为飞书机器人的 Socket 模式入口：

- 通过 `FEISHU_SOCKET_ENABLED=1` 打开
- 通过 `FEISHU_APP_ID` / `FEISHU_APP_SECRET` 鉴权
- 收到飞书文本消息后，内部仍然复用同一套 `resolveFrontendResult(...)` 流程
- `ask_user` 会直接回飞书追问
- `assistant_message` 会直接回飞书文本
- `intent_result` 会整理成文本摘要回飞书，方便先验证网关路由结果

飞书说明：

- 当前飞书通道先只处理文本消息
- 建议把密钥放到本地环境变量或 `deploy/.env`，不要写进代码仓库

说明：

- `POST /route` 更偏调试接口
- 面向前端的正式结果协议，应理解为 `ask_user | result`

请求体：

```json
{
  "session_id": "u1",
  "message": "明天早上8点提醒我喝水",
  "intents": []
}
```

说明：

- `message` 必须保留用户原始表达
- `intents` 可为空；为空时直接使用 Gateway 启动时加载的默认意图
- 同名意图时，请求里的定义优先

## 默认模型分层

协议定义四类模型：

- `low_precision_multimodal`
- `high_precision_multimodal`
- `low_precision`
- `high_precision`

当前收敛方向：

- 分类：默认 `low_precision`
- 意图执行：由意图自己的 `model_type` 决定
- 未命中或连续失败：内部走 `deepmodel`
- 但前端最终看到的仍然只有 `ask_user` 或 `result`

## 本地运行

```bash
cd Gateway
export DEFAULT_INTENTS_FILE="./default_intents.json"
export FEISHU_SOCKET_ENABLED="1"
export FEISHU_APP_ID="<your-feishu-app-id>"
export FEISHU_APP_SECRET="<your-feishu-app-secret>"
export INTENT_MODEL_BASE_URL="http://127.0.0.1:18081/v1"
export INTENT_MODEL_NAME="mlx-community/Qwen3.5-0.8B-8bit"
export INTENT_MODEL_API_KEY="sk-local"
export LOW_PRECISION_MULTIMODAL_MODEL_BASE_URL="http://127.0.0.1:18082/v1"
export LOW_PRECISION_MULTIMODAL_MODEL_NAME="mlx-community/Qwen3.5-4B-MLX-8bit"
export LOW_PRECISION_MULTIMODAL_MODEL_API_KEY="sk-local"
export DEEP_MODEL_BASE_URL="https://api.newcoin.tech/v1"
export DEEP_MODEL_NAME="doubao-seed-1-6-251015"
export DEEP_MODEL_API_KEY="<your-api-key>"
go run ./cmd/server
```

## Docker 部署

```bash
cd Gateway/deploy
cp .env.example .env
# edit .env and fill FEISHU_APP_ID / FEISHU_APP_SECRET when needed
./deploy.sh
```

停止：

```bash
cd Gateway/deploy
./down.sh
```
