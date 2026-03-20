# Default Intents Accuracy Report

- Generated at: `2026-03-19T21:54:10`
- Route URL: `http://127.0.0.1:19090/route`
- Total cases: `20`
- Passed: `16`
- Failed: `4`
- Pass rate: `80.0%`
- Avg latency: `2605.73 ms`

## By Skill

- `calendar.create`: 3/3 passed, avg `2714.68 ms`
- `deepmodel`: 1/1 passed, avg `2389.35 ms`
- `greeting.reply`: 1/1 passed, avg `2403.78 ms`
- `light.adjust`: 2/3 passed, avg `2672.06 ms`
- `memo.create`: 2/3 passed, avg `2501.56 ms`
- `reminder.create`: 2/3 passed, avg `2834.46 ms`
- `todo.create`: 2/3 passed, avg `2539.67 ms`
- `weather.query`: 3/3 passed, avg `2511.42 ms`

## Cases

### PASS `light_zh_bright`

- message: `把客厅灯调到明亮`
- elapsed_ms: `2668.04`
- actual: `{"route": "direct", "skill": "light.adjust", "params": {"brightness": "明亮", "room_name": "客厅"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `light_zh_standard`

- message: `厨房灯调成标准亮度`
- elapsed_ms: `2521.85`
- actual: `{"route": "direct", "skill": "light.adjust", "params": {"brightness": "标准", "room_name": "厨房"}, "missing_params": [], "confidence": 0.95, "question": null}`

### FAIL `light_zh_natural`

- message: `客厅的灯亮点一点`
- elapsed_ms: `2826.3`
- actual: `{"route": "direct", "skill": "light.adjust", "params": {"room_name": "客厅"}, "missing_params": null, "confidence": 0.85, "question": null}`
- failures: `param brightness missing`

### PASS `memo_zh_release`

- message: `记个备忘，明天发布软件`
- elapsed_ms: `2463.13`
- actual: `{"route": "direct", "skill": "memo.create", "params": {"content": "明天发布软件"}, "missing_params": [], "confidence": 0.95, "question": null}`

### FAIL `memo_zh_rent`

- message: `帮我记一下，下周交房租`
- elapsed_ms: `2572.12`
- actual: `{"route": "direct", "skill": "todo.create", "params": {"content": "下周交房租"}, "missing_params": null, "confidence": 0.85, "question": null}`
- failures: `skill expected 'memo.create', got 'todo.create'`

### PASS `memo_en_call`

- message: `Create a memo: call Alice after lunch`
- elapsed_ms: `2469.42`
- actual: `{"route": "direct", "skill": "memo.create", "params": {"content": "call Alice after lunch"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `reminder_zh_callmom`

- message: `明天上午九点提醒我给妈妈打电话`
- elapsed_ms: `2755.05`
- actual: `{"route": "direct", "skill": "reminder.create", "params": {"content": "给妈妈打电话", "time": "明天上午九点"}, "missing_params": null, "confidence": 0.95, "question": null}`

### PASS `reminder_zh_trash`

- message: `今天晚上8点提醒我倒垃圾`
- elapsed_ms: `2928.08`
- actual: `{"route": "direct", "skill": "reminder.create", "params": {"content": "倒垃圾", "time": "今天晚上8点"}, "missing_params": null, "confidence": 0.95, "question": null}`

### FAIL `reminder_en_plants`

- message: `Remind me at 7pm to water the plants`
- elapsed_ms: `2820.24`
- actual: `{"route": "direct", "skill": "reminder.create", "params": {"content": "water the plants", "time": "7pm"}, "missing_params": null, "confidence": 0.95, "question": null}`
- failures: `param time expected 'at 7pm', got '7pm'`

### PASS `todo_zh_milk`

- message: `把买牛奶加入待办，今天下班前完成`
- elapsed_ms: `2614.19`
- actual: `{"route": "direct", "skill": "todo.create", "params": {"content": "买牛奶", "due_time": "今天下班前"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `todo_zh_reimburse`

- message: `创建一个待办，内容是提交报销单`
- elapsed_ms: `2443.07`
- actual: `{"route": "direct", "skill": "todo.create", "params": {"content": "提交报销单"}, "missing_params": [], "confidence": 0.95, "question": null}`

### FAIL `todo_en_passport`

- message: `Add renew passport to my todo list by Friday`
- elapsed_ms: `2561.75`
- actual: `{"route": "direct", "skill": "todo.create", "params": {"content": "renew passport", "due_time": "Friday"}, "missing_params": [], "confidence": 0.95, "question": null}`
- failures: `param due_time expected 'by Friday', got 'Friday'`

### PASS `calendar_zh_review`

- message: `下周五下午三点安排一个项目复盘，在A会议室`
- elapsed_ms: `2708.73`
- actual: `{"route": "direct", "skill": "calendar.create", "params": {"location": "A会议室", "time": "下周五下午三点", "title": "项目复盘"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `calendar_zh_product`

- message: `帮我安排明天下午两点的产品评审`
- elapsed_ms: `2570.3`
- actual: `{"route": "direct", "skill": "calendar.create", "params": {"time": "明天下午两点", "title": "产品评审"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `calendar_en_dentist`

- message: `Schedule a dentist appointment next Tuesday at 10am in Room 301`
- elapsed_ms: `2865.01`
- actual: `{"route": "direct", "skill": "calendar.create", "params": {"location": "Room 301", "time": "next Tuesday at 10am", "title": "dentist appointment"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `weather_zh_tokyo`

- message: `东京明天天气怎么样`
- elapsed_ms: `2488.89`
- actual: `{"route": "direct", "skill": "weather.query", "params": {"date": "明天", "location": "东京"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `weather_zh_beijing`

- message: `北京今天会下雨吗`
- elapsed_ms: `2495.55`
- actual: `{"route": "direct", "skill": "weather.query", "params": {"date": "今天", "location": "北京"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `weather_en_tokyo`

- message: `What is the weather in Tokyo tomorrow?`
- elapsed_ms: `2549.81`
- actual: `{"route": "direct", "skill": "weather.query", "params": {"date": "tomorrow", "location": "Tokyo"}, "missing_params": [], "confidence": 0.95, "question": null}`

### PASS `greeting_zh`

- message: `你好`
- elapsed_ms: `2403.78`
- actual: `{"route": "direct", "skill": "greeting.reply", "params": {}, "missing_params": null, "confidence": 0.95, "question": null}`

### PASS `deepmodel_compare`

- message: `帮我比较一下 Rust 和 Go 的并发模型`
- elapsed_ms: `2389.35`
- actual: `{"route": "deepmodel", "skill": null, "params": null, "missing_params": null, "confidence": 0.45, "question": null}`
