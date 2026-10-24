# 邮件分析回放评测

`cmd/mail-eval` 将合成或人工脱敏的邮件案例放入逐案例独立的临时 SQLite 邮箱库，调用与生产相同的 `generateMailSummary → runMailAnalysis → agentruntime` 路径。它不启动 IMAP、通知或回复建议 worker，不创建后台任务，也不复用摘要缓存。每次回放冻结模型、业务提示词和 Skill 依赖内容，报告包含配置指纹、Skill 快照与流水线版本。

内置 8 个案例覆盖：转发引用、数量变更、同客户不同订单、修订报价、历史正文缺失、附件元数据、邮件指令干扰及未来/其他邮箱隔离、长正文尾部。转发案例复现曾观察到的 `gmail_quote` 结构问题；其余身份、域名、订单及商业数值均为合成测试数据，不是生产邮件导出。它们不能代表真实业务分布。

## 离线检查

```sh
make mail-eval-check
# 或仅验证数据：不需要密钥，不发模型请求，不产生质量分。
go run ./cmd/mail-eval -mode validate
```

普通 `go test ./...` 只运行本地规则与模拟传输测试，不调用真实模型。模拟测试结果不作为真实质量基线。

## 真实模型回放

复用本地已配置的模型和当前所有者的邮件总结 Skill：

```sh
go run ./cmd/mail-eval -mode run -config config.toml \
  -out output/mail-eval/baseline
```

读取配置时通过 `mode=ro`、`query_only` 及只读事务访问 SQLite，只读取模型、业务助手和 Skill 设置。不会调用会迁移或初始化数据库的 `mailstore.Open`，也不查询生产邮件。密钥只在内存中解密；临时邮箱库不存储模型凭据。

有多个所有者时必须用 `-owner` 或唯一的 `-model-id` 消除歧义。默认使用该所有者的默认模型与总结业务助手；`-model-id`、`-agent-id` 可选择已有记录。这里不自动采用某个真实邮箱的任务绑定，因为回放邮箱是合成的。Skill 注册表尚未初始化时使用代码内默认 Skill，并在报告中标记来源；已有注册表则读取其当前邮件总结流程及依赖，不写回设置。

也可使用环境变量提供模型凭据，避免在命令参数中放密钥：

```sh
export INBRIX_EVAL_API_KEY='...'
go run ./cmd/mail-eval -mode run \
  -provider openai-compatible -base-url https://your-model-endpoint.example/v1 \
  -model your-model -out output/mail-eval/environment-run
```

可选 `-cases forwarded-rfq,quantity-change`，`-repeat 3` 检查波动。案例按套件顺序执行；重复次数 1–10。整轮默认最多 64 个模型 HTTP 请求、15 分钟；可用 `-max-requests` 和 `-timeout` 收紧预算。每个案例仍受生产分析的 90 秒、工具与上下文预算限制。不自动重试网络故障，以保留真实失败；需要时定向重跑并保留两份报告。

报告为同一前缀的 `.json` 和 `.md`，文件权限为 0600，输出已存在时拒绝覆盖。默认输出位于被 Git 忽略的 `output/mail-eval/`。报告包括模型结果与 Skill 内容，分享前应检查其中的自定义业务设置。自定义案例也必须先完成脱敏；工具不会自动把任意真实邮件匿名化。

## 比较 Skill 候选

`-skill-file` 仅替换本次回放的根 Skill 指令，保留冻结的依赖；候选修订号为 0，内容指纹独立计算，不修改注册表或线上执行。

```sh
go run ./cmd/mail-eval -mode run -config config.toml \
  -skill-file internal/maileval/testdata/candidate-skill.md \
  -baseline output/mail-eval/baseline.json \
  -out output/mail-eval/candidate
```

比较报告列出共同案例的检查分、延迟变化、新增失败和已解决检查。只有套件和规则版本一致才允许比较；子集回放只比较共同的案例与重复轮次。单次分差可能来自模型随机性或网络，不能直接归因于候选 Skill。

保存的结果可离线重新计算或比较，不再调用模型：

```sh
go run ./cmd/mail-eval -mode rescore \
  -report output/mail-eval/candidate.json -out output/mail-eval/rescored
go run ./cmd/mail-eval -mode compare \
  -report output/mail-eval/candidate.json \
  -baseline output/mail-eval/baseline.json -out output/mail-eval/comparison
```

默认离线重算要求套件指纹一致。若仅修正规则（例如补齐“单价/总价”的同义词），可用 `-previous-suite` 提供采集时保存的原始套件：工具首先校验旧套件指纹确实对应报告，再校验触发邮件、历史、其他邮箱材料与隐藏边界控制完全一致，才允许离线重算。报告同时保留原采集指纹与新评分指纹。修改邮件输入后必须重新调用模型，不能以“规则更新”重用旧结果。基线和候选应一起按新规则重算后再比较。

```sh
go run ./cmd/mail-eval -mode rescore \
  -previous-suite output/mail-eval/suite-before-rubric-change.json \
  -report output/mail-eval/candidate.json -out output/mail-eval/reviewed
```

仓库中的候选文件针对首次真实回放发现的正文缺失引用、附件元数据和“尚未接受”状态解释问题。它是待评审材料，不会自动成为默认 Skill，也不会修改当前用户设置。

## 自动检查的能力边界

规则包括：指定输出字段的正则保留/排除检查、服务端记录的来源读取范围、引用、正文读取尝试，以及隐藏未来/其他邮箱内容的 canary 检查。预期规则和人工复核清单不会提供给模型。隐藏材料检查在实际请求发出前拦截违规内容。模型结果还必须通过生产的六键结构与引用校验。

检查分是明确规则的加权命中比例，并非语义准确率。关键词出现不能证明事实正确，负面模式命中也可能是合理的否定说明。每个案例均保留未完成的人工复核项，重点看：

- 是否把同客户其他订单的价格或承诺关联到本单；
- 是否把“尚未接受”写成“拒绝条款”；
- 是否把旧报价沿用于变更后的数量；
- 是否把附件文件名当作已确认规格；
- 是否把建议动作写成已经执行、承诺或成交。

退出码：0 表示全部自动检查通过，仍需语义复核；1 表示存在检查失败或案例执行失败；2 表示配置、数据、预算/取消或报告操作错误。基础设施超时和输出校验分别记录；不要把网络故障统计成检索能力错误。

## 耗时、token 和费用

耗时来自实际回放及各 HTTP 请求，报告 P50/P95。计量支持 Responses 与 Chat Completions 的已报告 token 字段，以及最多四层 `data` 网关封装；未识别或未返回的用量标记为未知，不能按零 token 处理。部分供应商的其他用量结构目前不作估算。

默认不猜价格。只有用量完整且同时传入 `-input-usd-per-million` 与 `-output-usd-per-million` 时，才按这两个统一单价计算 `estimatedCostUsd`。它不处理缓存折扣、阶梯计价或网关费用，不代表供应商账单。
