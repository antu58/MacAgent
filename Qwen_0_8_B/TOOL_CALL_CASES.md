# Tool Call 日常任务案例

用于测试 `http://127.0.0.1:18081/v1` 的工具调用行为。

## 案例与工具列表

1. 提醒任务  
用户输入：帮我创建一个明天早上8点提醒喝水的任务  
工具列表：`create_reminder(title, datetime, note)`  
期望：调用 `create_reminder`

2. 日程创建  
用户输入：请帮我创建日程：2026-03-20 15:00 和 张三 开会，时长30分钟，地点A会议室  
工具列表：`create_calendar_event(...)`, `create_todo(title)`  
期望：调用 `create_calendar_event`

3. 发邮件  
用户输入：给王总发邮件：我今天会晚到10分钟，主题是会议迟到说明  
工具列表：`send_email(to, subject, body)`, `create_todo(title)`  
期望：调用 `send_email`

4. 记账  
用户输入：记一笔午餐支出38元，分类餐饮，备注牛肉面  
工具列表：`add_expense(amount, currency, category, note)`, `create_note(text)`  
期望：调用 `add_expense`

5. 天气联动提醒（多步）  
用户输入：查一下明天上海天气，如果下雨就提醒我带伞  
工具列表：`get_weather(city, date)`, `create_reminder(title, datetime)`  
期望：第一步先调用 `get_weather`，再根据结果决定是否 `create_reminder`

## 一键测试脚本

```bash
cd /Users/zhangfeng/Desktop/Project/Apple/MacAgent/Qwen_0_8_B
chmod +x scripts/test_tool_call_accuracy.sh
./scripts/test_tool_call_accuracy.sh
```

可指定 endpoint：

```bash
./scripts/test_tool_call_accuracy.sh http://127.0.0.1:18081/v1/chat/completions
```

## 性能指标说明

脚本会额外输出：

- `ttft`：首 token 返回时间（Time To First Token）
- `total`：整次请求完成时间
- `overall_tps`：`completion_tokens / total`，稳定吞吐指标
- `gen_tps`：`completion_tokens / (total - ttft)`，仅在可可靠计算时输出

原始明细会保存到 `run/tool_call_benchmark_*.jsonl`。
