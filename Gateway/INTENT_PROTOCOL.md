# AI Intent Dispatch Protocol

本文档整理的是当前项目要收敛到的协议规范，用来约束：

- `Gateway` 的职责边界
- 意图配置文件格式
- 分类、执行、校验、降级各环节的输入输出
- 参数的统一规范

这份文档优先描述协议标准，而不是逐行复述当前实现细节。
当前仓库中的部分代码仍处于过渡阶段，但后续设计、默认意图文件、网页自定义意图，均应以本协议为准。

## 1. 目标

这个协议的目标是把整条链路拆成四层：

1. `Gateway Router`
   - 只做意图分类、分发、机械校验、重试、降级
2. `Intent Registry`
   - 提供意图定义、参数定义、模型类型、意图说明
3. `Intent Executor`
   - 命中某个意图后，调用对应模型完成该意图的参数理解和执行准备
4. `Deep Model`
   - 在未命中任何意图，或意图执行阶段连续失败时接管

一句话概括：

- `Gateway` 负责“选谁做”与“结果是否合法”
- `Intent Executor` 负责“这个意图到底该怎么理解”
- `Deep Model` 负责“没有合适意图时的开放式处理”

## 2. 角色与职责

### 2.1 Gateway

`Gateway` 是协议中心，职责固定为：

- 接收用户消息与本次注入的意图列表
- 加载并合并默认意图与请求意图
- 使用低精度分类模型做意图命中判断
- 按命中意图声明的 `model_type` 选择执行模型
- 向执行模型注入统一参数规则与该意图的详细说明
- 对执行结果做机械校验
- 校验失败时触发一次带错误上下文的重试
- 连续失败或未命中意图时降级到 `deepmodel`
- 对前端只输出统一的 `ask_user | result` 两类结果

`Gateway` 不负责：

- 在主流程里堆大量业务特例
- 用硬编码规则代替模型理解每个意图
- 把某个具体意图的执行逻辑写死在网关里

### 2.2 Intent Registry

意图注册表负责沉淀可复用的意图定义。每个意图至少回答四个问题：

- 这个意图什么时候该命中
- 这个意图不该处理什么
- 这个意图使用哪个模型类型
- 这个意图需要哪些参数，以及这些参数怎么解释

推荐把意图说明拆成两层：

- `route_description`
  - 只给分类器看
  - 用于描述命中边界、排除边界、相邻意图区分
- `execution_description`
  - 只给执行器看
  - 用于描述参数含义、使用说明、样例、特殊情况

### 2.3 Intent Executor

执行器不是固定代码模块，而是一类协议动作：

- 当某个意图被 `Gateway` 命中后
- `Gateway` 根据 `model_type` 选择对应模型
- 把“用户原始输入 + 基础参数规则 + 当前意图详细说明 + 参数结构”发给执行模型
- 执行模型只围绕这个意图输出结构化结果

### 2.4 Deep Model

`Deep Model` 仅用于：

- 没有命中任何意图
- 命中了意图，但执行结果连续两次机械校验失败
- 用户问题本身就是开放式复杂对话，不适合意图协议

## 3. 模型类型

协议定义四种模型类型，由意图自行声明：

```text
low_precision_multimodal
high_precision_multimodal
low_precision
high_precision
```

含义如下：

- `low_precision_multimodal`
  - 低精度多模态模型
  - 当前可对应 `4B VLM`
- `high_precision_multimodal`
  - 高精度多模态模型
  - 当前先保留类型占位
- `low_precision`
  - 低精度文本模型
  - 当前建议用于分类、轻量任务执行
  - 现阶段路由器默认可使用 `0.8B`
- `high_precision`
  - 高精度文本模型
  - 当前默认对应 `deepmodel`

约束：

- 分类模型与执行模型可以不是同一个模型
- 分类阶段默认优先使用 `low_precision`
- 命中意图后的执行阶段，由意图自己的 `model_type` 决定
- `deepmodel` 是兜底能力，不等于所有意图都必须使用 `high_precision`

## 4. 端到端流程

```text
Client
  -> Gateway
     -> merge(default_intents, request.intents)
     -> Classifier LLM (low_precision)
        -> hit intent
           -> Executor LLM (selected by intent.model_type)
              -> mechanical validation
                 -> success => ask_user / result
                 -> fail => retry once with error context
                 -> fail again => result
        -> no hit => result
```

流程判定：

1. `Gateway` 先做意图合并
2. 分类模型只负责回答“命中哪个意图”或“未命中”
3. 如果未命中，直接进入 `deepmodel`
4. 如果命中，执行模型负责抽取参数并生成结构化结果
5. `Gateway` 只做机械校验，不做重业务语义推理
6. 校验失败时，把错误原因作为上下文重试一次执行模型
7. 第二次仍失败，则降级到 `deepmodel`

注意：

- `deepmodel`
- 模型选择
- 执行分发
- 重试过程

都属于 `Gateway` 内部执行过程，不属于前端返回协议。

## 5. 外部接口协议

### 5.1 Chat Request

客户端到 `Gateway` 的请求体：

```json
{
  "session_id": "web-user",
  "message": "客厅的灯亮点一点",
  "intents": []
}
```

字段定义：

- `session_id`: 会话标识
- `message`: 用户原始输入，必须原样透传给执行模型
- `intents`: 本次请求动态注入的意图列表，可为空

合并规则：

1. `Gateway` 启动时加载默认意图文件
2. 请求体中的 `intents` 作为本次请求覆盖层
3. 同名意图时，请求体定义覆盖默认定义

### 5.2 Frontend Result

`Gateway` 对前端只暴露两种结果：

- `ask_user`
- `result`

也就是说：

- 前端不感知 `direct`
- 前端不感知 `deepmodel`
- 前端不感知“当前用了哪个模型”
- 前端不感知“本次是否发生了重试”

这些都属于网关内部执行细节。

#### Ask User

```json
{
  "type": "ask_user",
  "skill": "reminder.create",
  "params": {
    "content": "发布软件"
  },
  "missing_params": ["time"],
  "question": "请补充提醒时间"
}
```

字段定义：

- `type`: 固定为 `ask_user`
- `skill`: 当前已命中的意图名
- `params`: 当前已经确认合法的已收集参数
- `missing_params`: 仅允许列出必填参数
- `question`: 面向用户的补问

#### Result

```json
{
  "type": "result",
  "skill": "light.adjust",
  "response": {
    "description": "Control an indoor light or lamp.",
    "executed": false,
    "intent": "light.adjust",
    "params": {
      "room_name": "客厅",
      "brightness": "明亮"
    }
  }
}
```

字段定义：

- `type`: 固定为 `result`
- `skill`: 若命中了意图，可返回意图名；开放式综合回复时可为 `null`
- `response`: 综合响应结果

#### 综合响应结果

`response` 是网关对前端的统一结果容器。它可以承载两类内容：

1. 意图结果
2. 开放式综合回复

意图结果示例：

```json
{
  "type": "intent_result",
  "description": "Control an indoor light or lamp.",
  "executed": false,
  "intent": "light.adjust",
  "params": {
    "room_name": "客厅",
    "brightness": "明亮"
  }
}
```

开放式综合回复示例：

```json
{
  "type": "assistant_message",
  "content": "如果想让客厅的灯更亮，可以试试这些方法..."
}
```

字段定义：

- 对前端来说，拿到 `type=result` 就表示“本次已经形成最终综合响应”
- 这个综合响应可以来自意图执行，也可以来自 `deepmodel`
- 但来源不应作为前端协议字段暴露

## 6. 分类阶段协议

分类阶段是 `Gateway` 的内部协议，结果应尽量简单。

推荐输出：

```json
{
  "hit": true,
  "intent": "light.adjust",
  "confidence": 0.87,
  "reason": "matched room light control request"
}
```

或：

```json
{
  "hit": false,
  "intent": null,
  "confidence": 0.41,
  "reason": "no intent matches"
}
```

分类阶段规则：

- 只回答是否命中某个意图
- 不负责最终参数抽取
- 不负责 `ask_user`
- 不负责具体执行逻辑
- 不应在分类阶段塞过多具体业务示例

分类阶段看什么：

- 意图名
- 意图概览说明
- 意图边界（什么时候用，什么时候不用）
- 参数名与参数基本结构

分类阶段不应承担什么：

- 每个参数怎么抽最合理
- 复杂边界样例的精细处理
- 某个意图的专属回复风格

## 7. 执行阶段协议

命中某个意图后，`Gateway` 需要把以下四类内容注入给执行模型：

1. 用户原始输入
2. 基础参数解读规则
3. 当前意图的 `execution_description`
4. 当前意图的参数结构

### 7.1 基础参数解读规则

这部分由 `Gateway` 统一提供，属于所有意图共享的底层规则，例如：

- 只允许输出定义过的参数名
- 必填参数缺失时要明确列出
- 可选参数缺失不能触发 `ask_user`
- 枚举参数只能从 `enum_values` 中选择
- `number` 只能输出数字
- `text` 只能输出文本
- 默认值由 `Gateway` 负责补，不要求执行模型主动编造默认值
- 无法可靠推断时，宁可留空，也不要编造
- 对于采用 `time + time_period` 组合的时间参数，`time` 应收敛为纯时钟字符串，`time_period` 单独表达上午/下午/晚上/24小时制

### 7.2 执行模型输出

执行模型推荐输出：

```json
{
  "skill": "light.adjust",
  "params": {
    "room_name": "客厅",
    "brightness": "明亮"
  },
  "missing_params": [],
  "confidence": 0.86,
  "question": null
}
```

说明：

- 执行模型不需要决定 `deepmodel`
- `deepmodel` 由 `Gateway` 统一决定
- 执行模型的任务是：
  - 围绕当前意图抽取参数
  - 标出缺失的必填参数
  - 在需要补参时给出问题

`Gateway` 根据这个结果再生成前端可见结果：

- 必填参数缺失时输出 `ask_user`
- 其余情况输出 `result`

## 8. 意图配置协议

## 8.1 规范目标

意图配置既要足够结构化，便于网关和网页处理；也要足够语义化，便于执行模型理解。

最终建议采用“两层结构”：

- 参数层只保留机器约束
- 分类说明层承担命中边界解释
- 执行说明层承担参数语义解释

也就是说：

- `params[]` 只描述“这个参数长什么样”
- `route_description` 负责说明“什么时候该命中这个意图”
- `execution_description` 负责说明“这个参数到底意味着什么、怎么抽、有哪些特例”

这也是你当前方向下更稳的一种设计。

## 8.2 Canonical Intent Spec

协议建议的标准意图结构如下：

```json
{
  "name": "light.adjust",
  "model_type": "low_precision_multimodal",
  "route_description": "Use when the user wants to adjust an indoor light or lamp. Do not use for memo, reminder, calendar, weather, or open-ended explanation requests.",
  "execution_description": "Goal: prepare a light adjustment task. room_name means the target room and should only be filled when explicitly mentioned. brightness means the target brightness enum and may map common phrases like '亮一点' to the closest allowed enum only when safe.",
  "params": [
    {
      "name": "room_name",
      "value_type": "text",
      "required": false,
      "is_enum": true,
      "enum_values": ["卧室", "客厅", "厨房"],
      "default_value": "卧室"
    },
    {
      "name": "brightness",
      "value_type": "text",
      "required": false,
      "is_enum": true,
      "enum_values": ["微亮", "标准", "明亮"],
      "default_value": "标准"
    }
  ]
}
```

## 8.3 必填字段

每个意图至少应包含：

- `name`
- `model_type`
- `route_description`
- `execution_description`
- `params`

### `name`

要求：

- 全局唯一
- 稳定，不要频繁改名
- 推荐使用 `domain.action` 风格，例如：
  - `light.adjust`
  - `memo.create`
  - `weather.query`

### `model_type`

必须是以下枚举之一：

- `low_precision_multimodal`
- `high_precision_multimodal`
- `low_precision`
- `high_precision`

### `route_description`

这是分类器的主语义入口。

推荐承载：

- 这个意图什么时候该命中
- 这个意图什么时候不该命中
- 它和相邻意图如何区分

### `execution_description`

这是执行器的主语义入口。

推荐承载：

- 参数含义
- 参数抽取规则
- 使用说明
- 示例
- 特殊情况

设计原则：

- `route_description` 尽量短、尽量聚焦命中边界
- `execution_description` 可以更详细，但只服务于当前意图执行
- 两者职责分离，不要把大量参数抽取细节塞回分类器

### `params`

`params[]` 只保留结构约束，不承担主要语义说明。

## 9. 参数协议

### 9.1 Canonical Param Spec

```json
{
  "name": "time_period",
  "value_type": "text",
  "required": false,
  "is_enum": true,
  "enum_values": ["24小时制", "凌晨", "上午", "下午", "晚上"],
  "default_value": "24小时制"
}
```

### 9.2 字段定义

- `name`
  - 参数名
  - 在当前意图内唯一
- `value_type`
  - `text | number`
- `required`
  - `true | false`
- `is_enum`
  - `true | false`
- `enum_values`
  - 仅在 `is_enum=true` 时有效
- `default_value`
  - 仅在 `required=false` 时允许出现

### 9.3 参数设计原则

参数层只回答下面这些问题：

- 这个参数叫什么
- 它是文字还是数字
- 它是不是枚举值
- 它是不是必填
- 它有没有默认值

参数层不负责回答：

- 这个参数在业务上意味着什么
- 它和相邻参数怎么区分
- 用户自然语言里的哪些表达要映射到它
- 哪些特殊案例该如何处理

这些都应优先写入 `execution_description`。

## 10. 参数解释与校验规则

### 10.1 必填与可选

- `required=true` 的参数缺失时，最终结果应转为 `ask_user`
- `required=false` 的参数缺失时，不允许仅因为缺失而 `ask_user`
- 可选参数若配置了 `default_value`，由 `Gateway` 在执行结果通过机械校验后补全

### 10.2 数据类型

- `value_type=number`
  - 只允许输出数字
  - 字符串形式的数字可在归一化后接受，但最终应视为数字
- `value_type=text`
  - 只允许输出文本

### 10.3 枚举值

- `is_enum=true` 时，只能从 `enum_values` 中选择
- 无法可靠判断时，应留空，不允许编造
- `number` 类型的枚举值必须全为数字
- `text` 类型的枚举值必须保持文本枚举一致性

### 10.4 默认值

- 只有可选参数允许声明默认值
- 默认值必须满足本参数自身的类型与枚举约束
- 枚举参数的默认值必须来自自己的 `enum_values`

### 10.5 时间参数规范

对于网关级别的时间参数，协议定义以下统一规则：

- 当某个意图采用 `time` 与 `time_period` 组合时：
  - `time` 只保存格式化后的时钟字符串
  - `time_period` 单独保存时间制式或时段语义
- `time` 不应保留日期词、星期词、相对日期词或时段词，例如：
  - 不应保留 `明天`
  - 不应保留 `下周五`
  - 不应保留 `上午`
  - 不应保留 `晚上`
- 推荐格式：
  - `10:00`
  - `03:30`
  - `23:00`
- `time_period` 推荐枚举：
  - `24小时制`
  - `凌晨`
  - `上午`
  - `下午`
  - `晚上`

示例：

```json
{
  "time": "10:00",
  "time_period": "上午"
}
```

对于：

- `明天早上10点`
- `tomorrow at 10am`

如果意图没有单独的日期参数，网关机械规范化后，`time` 仍应只保留时钟部分。

24 小时制规则：

- 明确的 24 小时写法，如 `23:00`、`18:30`
  - `time` 保持原时钟值
  - `time_period=24小时制`
- 即使原话同时出现 `晚上23:00`，也仍然以 `24小时制` 为准

12 小时 / 时段规则：

- `上午10点` -> `time=10:00`, `time_period=上午`
- `下午3点半` -> `time=03:30`, `time_period=下午`
- `7pm` -> `time=07:00`, `time_period=晚上`

这条规则属于 `Gateway` 的共享参数规范，而不是某个单独意图的私有特例。

## 11. 网关机械校验

`Gateway` 在执行模型返回结果后，只做机械校验，不代替模型做复杂语义推理。

校验清单：

- 输出是否为合法 JSON
- `skill` 是否等于当前命中的意图名
- 是否出现未定义参数名
- 参数类型是否合法
- 枚举值是否合法
- 默认值是否可补
- 必填参数是否仍缺失

如果机械校验通过：

- 必填参数齐全 -> `result`
- 必填参数缺失 -> `ask_user`

如果机械校验失败：

- 把错误上下文反馈给执行模型重试一次
- 第二次仍失败 -> `deepmodel`

## 12. 重试与降级协议

### 12.1 允许触发重试的情况

- 输出不是合法 JSON
- `skill` 错了
- 参数名不存在
- 参数类型错误
- 枚举值不合法
- 必填参数列表与 `params` 明显矛盾

### 12.2 重试输入应包含什么

`Gateway` 重试时，应追加：

- 上一次返回原文
- 具体错误原因
- 当前意图参数结构
- 明确要求只修正结构，不要改动用户语言

### 12.3 降级条件

以下任一条件满足时，进入 `deepmodel`：

- 分类阶段未命中任何意图
- 执行阶段第一次失败、第二次仍未通过机械校验
- 当前请求本身明显超出意图协议覆盖范围

但对前端来说，这些内部降级最终仍只表现为：

- `ask_user`
- 或 `result`

## 13. SSE 输出协议

`/chat` 继续使用 SSE：

```json
{
  "type": "message | status",
  "content": "...",
  "done": false
}
```

推荐状态事件：

- `routing...`
- `executing intent...`
- `skill executed`
- `calling deep model...`

说明：

- SSE 的 `status` 事件属于调试和体验信息
- 它们不是正式业务协议字段
- 正式业务结果仍应落在 `ask_user` 或 `result`

## 14. 默认意图文件与网页注入

默认意图来源：

- [default_intents.json](/Users/zhangfeng/Desktop/Project/Apple/MacAgent/Gateway/default_intents.json)

规则：

1. `Gateway` 启动时加载默认意图文件
2. 网页当前不再自带默认意图
3. 网页自定义意图通过请求体注入
4. 同名意图时，请求体优先级高于默认文件

## 15. 兼容策略

为避免一次性重构过大，协议允许短期兼容旧字段，但新配置应逐步向标准格式收敛。

### 15.1 兼容旧字段

当前仓库里仍可能出现：

- `route_description` / `execution_description` 已存在
- `description` 为字符串
- `params[].description` 承担大量语义说明

这类写法在过渡期可以继续读取，但不建议作为长期标准。

### 15.2 推荐收敛方向

长期标准应为：

- 分类说明与执行说明分离
- 参数对象尽量简洁
- 参数语义上收至 `execution_description`
- 网关只做分类、分发、机械校验、重试、降级
- 具体意图理解交给对应执行模型

## 16. 当前建议

如果现在开始继续演进这套系统，建议按这个顺序推进：

1. 先把意图配置文件从“自由文本描述”收敛到本协议结构
2. 再把分类模型与执行模型彻底拆开
3. 再把默认意图逐步迁移到新的 `description` 结构
4. 最后再补多意图编排，而不是过早把复杂流程塞回 `Gateway`

这会让系统保持两个优点：

- 架构可扩展
- 网关职责始终清晰
