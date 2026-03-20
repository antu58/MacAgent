# Gateway Web

独立网页体验层，用于：

- 编辑和注入自定义意图
- 发送用户消息到 `Gateway`
- 查看 `client payload`、`route result`、`chat` 日志
- 体验 `/chat` 的 SSE 输出

默认端口：

- Web: `19091`
- Gateway: `19090`

协议与参数规范以：

- [Gateway/INTENT_PROTOCOL.md](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/INTENT_PROTOCOL.md)

为准。

## 当前行为

- 网页不再自带默认意图
- `GET /api/config` 返回空的 `default_intents`
- 不配置任何自定义意图也可以直接发送
- 这时会使用 `Gateway` 启动时加载的默认意图
- 如果网页注入了同名意图，会覆盖 Gateway 默认定义
- 网页最终只消费两类正式结果：`ask_user` 和 `result`

## 当前推荐做法

- 高频稳定意图，沉淀到 Gateway 默认意图文件
- 临时调试、实验性意图，从网页注入
- 参数对象尽量保持结构化和简洁
- `route_description` 用于分类边界
- `execution_description` 用于参数语义、例子、边界说明
- `deepmodel`、模型选择、执行分发只作为网关内部过程存在

默认意图文件见：

- [Gateway/default_intents.json](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/default_intents.json)

## 部署

```bash
cd GatewayWeb/deploy
cp .env.example .env
./deploy.sh
```

打开：

[http://127.0.0.1:19091](http://127.0.0.1:19091)
