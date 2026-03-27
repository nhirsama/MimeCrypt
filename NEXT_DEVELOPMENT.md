# MimeCrypt 下一阶段开发文档

## 1. 文档目标
本阶段目标是把当前“可跑通原型”推进到“可验证闭环”的版本：邮件发现 -> MIME 拉取 -> 加密 -> 回写 -> 校验，全链路可测试、可观测、可回滚。

## 2. 当前状态分析
- 架构边界已清晰，`discover -> process -> download/encrypt/writeback` 流程已打通。
- Microsoft Graph 登录、token 缓存与自动刷新可用。
- 增量发现与首次基线跳过逻辑可用。
- 关键缺口：
  - `encrypt` 仅做“是否已加密”识别，未执行真实加密。
  - `writeback` 和 Graph writer 仍为未实现占位。
  - 明文 MIME 默认落盘，和“最少暴露”目标存在偏差。
  - 测试覆盖集中在 `auth/discover/encrypt`，核心流程与 provider 层覆盖不足。

## 3. 里程碑规划

### M1. 实现真实加密（P0）
交付内容：
- 在 `internal/modules/encrypt` 接入 GPG 执行器（可替换接口 + 默认实现）。
- 生成 RFC 3156 `multipart/encrypted` 输出。
- 为已加密邮件保持透传。

验收标准：
- 输入明文 MIME，输出结构可被 Thunderbird/OpenPGP 客户端识别。
- 单测覆盖：纯文本、含附件、已加密透传、GPG 失败回退。

### M2. 实现回写与校验（P0）
交付内容：
- 在 Graph provider 中实现 `WriteMessage`。
- 打通 `process --write-back --verify-write-back` 行为。
- 回写后至少完成“可读回校验”（message id 或 hash 校验）。

验收标准：
- 集成测试可验证 `WroteBack=true` 且 `Verified=true`。
- 回写失败可返回可诊断错误，不污染同步状态。

### M3. 减少明文暴露（P1）
交付内容：
- 增加 `--save-plain`（默认 false），仅调试模式落盘明文。
- 处理链路优先内存传递；必要落盘时强制权限 `0600` 并可选自动清理。

验收标准：
- 默认配置下不产生明文 `.eml` 文件。
- 文档明确审计与调试模式下的数据留存策略。

### M4. 可运维能力（P1）
交付内容：
- 结构化日志字段：`message_id`、`folder`、`format`、`wrote_back`、`verified`。
- 增加基础指标：处理总数、失败数、重试数、耗时分布。

验收标准：
- 单轮与持续运行可快速定位失败邮件与失败阶段。

## 4. 测试与质量门禁
- `go test ./...` 必须通过。
- 新增模块要求最少单测 + 一条端到端集成用例（mock provider 可接受）。
- 合并前执行：
  - `go test ./...`
  - `go test ./... -cover`
  - `go vet ./...`

## 5. 建议执行顺序
1. 先完成 M1（不依赖回写实现，风险最低）。
2. 再完成 M2，形成“真正闭环”。
3. 完成 M3 降低安全暴露面。
4. 最后补 M4 提升稳定运营能力。
