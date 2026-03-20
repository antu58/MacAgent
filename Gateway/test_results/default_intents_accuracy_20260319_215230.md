# Default Intents Accuracy Report

- Generated at: `2026-03-19T21:52:30`
- Route URL: `http://127.0.0.1:19090/route`
- Total cases: `20`
- Passed: `0`
- Failed: `20`
- Pass rate: `0.0%`
- Avg latency: `0.27 ms`

## By Skill

- `calendar.create`: 0/3 passed, avg `0.03 ms`
- `deepmodel`: 0/1 passed, avg `0.03 ms`
- `greeting.reply`: 0/1 passed, avg `0.03 ms`
- `light.adjust`: 0/3 passed, avg `1.6 ms`
- `memo.create`: 0/3 passed, avg `0.03 ms`
- `reminder.create`: 0/3 passed, avg `0.04 ms`
- `todo.create`: 0/3 passed, avg `0.03 ms`
- `weather.query`: 0/3 passed, avg `0.03 ms`

## Cases

### FAIL `light_zh_bright`

- message: `把客厅灯调到明亮`
- elapsed_ms: `4.7`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `light_zh_standard`

- message: `厨房灯调成标准亮度`
- elapsed_ms: `0.05`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `light_zh_natural`

- message: `客厅的灯亮点一点`
- elapsed_ms: `0.04`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `memo_zh_release`

- message: `记个备忘，明天发布软件`
- elapsed_ms: `0.04`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `memo_zh_rent`

- message: `帮我记一下，下周交房租`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `memo_en_call`

- message: `Create a memo: call Alice after lunch`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `reminder_zh_callmom`

- message: `明天上午九点提醒我给妈妈打电话`
- elapsed_ms: `0.04`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `reminder_zh_trash`

- message: `今天晚上8点提醒我倒垃圾`
- elapsed_ms: `0.04`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `reminder_en_plants`

- message: `Remind me at 7pm to water the plants`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `todo_zh_milk`

- message: `把买牛奶加入待办，今天下班前完成`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `todo_zh_reimburse`

- message: `创建一个待办，内容是提交报销单`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `todo_en_passport`

- message: `Add renew passport to my todo list by Friday`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `calendar_zh_review`

- message: `下周五下午三点安排一个项目复盘，在A会议室`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `calendar_zh_product`

- message: `帮我安排明天下午两点的产品评审`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `calendar_en_dentist`

- message: `Schedule a dentist appointment next Tuesday at 10am in Room 301`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `weather_zh_tokyo`

- message: `东京明天天气怎么样`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `weather_zh_beijing`

- message: `北京今天会下雨吗`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `weather_en_tokyo`

- message: `What is the weather in Tokyo tomorrow?`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `greeting_zh`

- message: `你好`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`

### FAIL `deepmodel_compare`

- message: `帮我比较一下 Rust 和 Go 的并发模型`
- elapsed_ms: `0.03`
- actual: `{}`
- failures: `<urlopen error [Errno 1] Operation not permitted>`
