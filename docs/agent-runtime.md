# Agent 执行内核

交互聊天和后台邮件分析共用 `internal/agentruntime.Run`。执行内核不包含邮件字段、报价步骤或合同流程；应用入口组合模型、工具、Skill、预算和结果校验器。

## 分层

| 层 | 位置 | 职责 |
| --- | --- | --- |
| 通用消息与执行循环 | `internal/agentruntime` | 结构化消息、工具注册列表、调用 ID、预算、取消、活动事件和输出校验重试 |
| 模型协议适配 | `mailstore/ai_agent_completion.go`、`ai_agent_stream.go` | OpenAI Responses、DeepSeek / Chat Completions、Gemini；转换请求和流式响应，保留供应商要求的续传内容 |
| 应用工具 | `handlers/web/ai_agent_tools.go`、`mailstore/agent_mail_tools.go` | 声明当前执行可用的工具参数和能力；原有 executor 检查身份、范围、版本及证据 |
| 工作方法 | 用户管理的 Skill | 按目录选择、按需加载，或显式选择后加载；定义邮件分析、产品核对、报价和合同工作方法 |
| 任务交付 | 聊天 / 邮件分析入口 | 聊天生成可展示结果及草稿；邮件分析提供输出 schema，最后持久化结果；展示摘要为最多 200 字的自由文本 |

新增工具时，应用入口同时注册 schema 和实现。Skill 可以说明如何使用工具，不能注册工具或扩大权限。已有 Skill 内容、启停和删除状态不因这次升级而覆盖；新的文档完整性指引随默认 Skill 首次初始化，已有用户可通过 Skill 管理自行调整流程。历史 Skill 的文本调用示例不改变服务器使用的原生调用协议。

## 一轮执行

1. 入口冻结当前配置、Skill 和数据范围，并提供当前任务上下文。
2. 模型适配器发送消息和原生工具定义，返回独立的可见文本、工具调用及私有续传数据。
3. 内核检查调用 ID 和工具注册列表，再调用受范围限制的 executor。
4. 每个调用都有同 ID 的工具结果；失败及因预算拒绝的调用也有明确结果。过大的结果替换为缺省提示，不把未提供的数据算作已读证据。
5. 将模型消息及对应工具结果传回模型。推理内容和签名不进入工具活动、用户聊天记录或日志。
6. 最终答案通过入口校验后才交付。邮件总结的 `submit_result` 是该入口注册的终止工具，不包含在通用内核的业务知识里。

邮件总结校验失败时，错误通过对应 `submit_result` 的工具结果返回；普通文本结果则通过校验反馈消息纠正。两者都受相同轮数、修正次数和上下文预算约束。预算耗尽后只允许综合已有证据；若仍请求其他工具或结构无效，则失败，不伪造成功。

工具参数按通用 Agent 约定容错：`arguments` 在严格解析失败后先做保守修复（剥离 markdown 围栏、去除尾逗号、转义字符串内的裸换行等控制字符、还原双重编码的字符串），修复不改变内容；仍失败时解析错误文本作为该调用的工具结果回灌给模型继续回合，而不是立即终止任务。畸形的 `submit_result` 同样按失败的工具调用回灌，不消耗输出校验修正次数；预算收尾的强制最终提交若参数仍无法解析，允许一次纠错重试，第二次失败才终止。

## 模型协议

- OpenAI 使用 Responses 的 `function_call` / `function_call_output` 和 `call_id`，回传完整 output items，包括推理续传项。
- DeepSeek 与兼容网关使用 Chat Completions 的 `tool_calls` / `role=tool` 和 `tool_call_id`，需要时保留 `reasoning_content`。兼容网关可使用嵌套 `data` 包装。
- Gemini 回传原始 parts 和 thought signatures。批量调用的响应组成同一 user turn；提供了函数 ID 时回传原 ID，没有 ID 时只使用本地关联标识，不向供应商虚构 ID。
- 流式响应在完成事件之后才交付工具调用；截断、错误或中断的响应不会执行部分调用。只有可见正文增量进入聊天流。

不支持原生工具调用的网关会明确失败。这次升级没有把伪装成工具调用的文本作为自动降级协议。JSON/DSML 解析仍用于历史消息清理及旧文本适配器；生产聊天和总结入口均使用原生 `Complete` 接口。

## 参考与验证

本实现参考了 [Codex 工具路由](https://github.com/openai/codex/blob/main/codex-rs/core/src/tools/router.rs) 中结构化调用与受控分发的分工，以及 [DeepSeek Harness 核心设计](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/core.md) 中会话、模型、工具注册和执行循环的边界，在现有 Go 服务中实现，没有引入这些项目的运行时依赖。OpenAI 协议以 [官方 function calling 文档](https://developers.openai.com/api/docs/guides/function-calling) 为依据。

测试通过模拟供应商的真实协议包验证完整执行循环，包括流式参数分片、调用 ID 配对、工具权限拒绝、推理与签名续传、截断拒绝、预算收尾、结构校验修正、邮件证据以及现有聊天和任务行为；模拟测试不代表所有第三方网关都支持原生工具协议。
