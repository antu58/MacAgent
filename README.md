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
