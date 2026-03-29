# MimeCrypt 下一阶段开发文档

## 1. 文档目的

这份文档用于记录主干当前已经具备的能力、仍然存在的缺口，以及下一阶段建议优先级。内容以当前代码实现为准，不再沿用早期原型时期的假设。

## 2. 当前实现状态

### 已完成

- 运行时已经转为 topology-first，`credential`、`source`、`sink`、`route` 都是命名对象，CLI 通过 `internal/flowruntime` 解析运行计划。
- `run` 已支持一个 route 下的多个 source；每个 source 都有独立锁、独立轮询周期和独立状态目录。
- 主链路已经统一为 `Producer -> Processor -> Consumer`，删除源邮件由 `mailflow.Coordinator` 统一判定和执行。
- 当前内置驱动已覆盖 `imap`、`graph` source，以及 `file`、`discard`、`imap`、`graph`、`ews` sink。
- 回写链路已经具备目标文件夹选择、幂等对账、可选校验，以及 `process` 输出里的 `wrote_back`、`verified` 结果摘要。
- topology JSON 入口已经拒绝未知字段，命名 `credential_ref` 也已经进入 source / sink 的运行时装配。
- 默认处理链路不会把明文 MIME 持久化到业务输出目录；临时文件权限为 `0600`，处理完成后会自动清理。
- `MIMECRYPT_GPG_HOME` 已支持显式隔离 GnuPG home。
- 审计日志已支持 JSONL 文件输出和 stdout 输出。
- 当前 `go test ./...` 通过。

### 部分完成

- 写回失败处理已经具备可诊断性，但 Graph 路径不是“强回滚”，而是尽量保留已创建对象并返回错误。
- 明文暴露面已经显著收敛，但仍然缺少“启动时清理遗留临时目录”这类带外治理。
- 审计事件模型已经有 `message_id`、`format`、`wrote_back`、`verified` 等字段，但运行时终端输出仍然是非结构化文本。
- 单封 `process` 已支持 `ephemeral` / `persistent` 两种事务模式，但还没有进一步抽成统一对外 API。

### 未完成

- 新入口驱动：webhook 原始 MIME、Gmail、SMTP / IMAP / POP3 等更通用的 ingress。
- 更稳定的 provider capability 模型和插件化边界。
- 指标系统：处理量、失败量、重试量、耗时分布。
- Graph 大附件的流式 / 分段上传，避免超大 MIME 带来的内存压力。
- 面向安全场景的启动清理、孤儿临时文件回收和更细化的数据留存策略。
- CI 级质量门禁自动化。

## 3. 原里程碑校准

### M1. 回写与校验

状态：基本完成

- Graph / IMAP 回写、对账、校验路径都已经存在。
- 默认源文件夹回写和显式目标文件夹回写都已具备。
- 原文中“回滚策略”的表述需要收敛。当前实现更接近“失败时保留可诊断现场”，而不是事务性回滚。

### M2. 明文暴露控制

状态：部分完成

- 默认主处理链路已经不是“明文默认落盘”。
- `download` 命令仍会显式导出原始 MIME，这是命令职责，不应视为主链路回退。
- `--save-plain` 当前并不存在；如果将来需要，应当定位为调试开关，而不是默认能力。

### M3. 运维可观测性

状态：部分完成

- 审计日志已经存在。
- 结构化运行日志、指标和统一失败阶段视图仍未完成。

## 4. 下一阶段建议

### P0. 可观测性补齐

- 统一 coordinator、consumer、delete-source 的结果事件，同时落到审计日志和终端输出。
- 引入最小指标面：总量、失败量、跳过量、删除量、耗时。

### P1. 数据安全补齐

- 增加启动清理或后台清理，处理异常退出后的临时明文遗留。
- 明确各 provider 的删除语义，并继续把 soft delete 与安全删除区分开。

### P1. 扩展能力建设

- 抽象通用 ingress driver，让 poll、webhook、listener 都统一归一到“原始 MIME + trace context”。
- 新增 webhook MIME source 和 Gmail source，验证多凭据、多来源模型的稳定性。
- 评估 Graph 大附件 upload session。

## 5. 测试与质量门禁

### 当前已具备

- 模块级测试已经覆盖 runtime 计划解析、provider 装配、source / sink 状态布局，以及单封 / 持续运行的关键路径。
- `go test ./...` 是当前最小可接受基线。

### 仍需补齐

- mixed-provider deep health 的 CLI 回归测试。
- 审计事件和终端输出一致性的直接回归测试。
- `go test ./... -cover`、`go vet ./...` 的自动化执行。
- CI workflow。

## 6. 结论

- 当前代码已经不再是“只有单一 provider 的原型”，文档重点应从“补齐基础功能”切换到“可观测性、数据安全带外治理和新入口驱动”。
- 如果只排一个最近优先级，建议先做临时文件遗留治理和结构化运行结果输出，再做 webhook、Gmail 等新入口。
