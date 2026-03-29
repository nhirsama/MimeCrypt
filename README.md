# MimeCrypt

MimeCrypt 是一个面向服务器场景的自动化邮件加密工具。项目围绕“发现邮件、读取原始 MIME、执行 PGP/MIME 加密、回写邮箱、保留审计记录”这一处理链路构建，适用于需要在受控主机上持续处理邮箱中明文邮件的场景。

## 项目概述

MimeCrypt 的核心职责如下：

- 发现新到达的邮件或指定范围内的邮件
- 读取原始 RFC 822 / MIME 数据
- 识别已加密邮件并终止重复处理
- 对未加密邮件执行 GPG 加密并生成 RFC 3156 `multipart/encrypted`
- 将处理结果回写至原邮箱或指定文件夹
- 在本地生成审计记录与可选备份

项目现阶段聚焦于 Microsoft 365 体系下的自动化邮件加密，代码结构已设置 provider 与接入层扩展边界。

## 设计目标

- 自动化处理：登录完成后，主链路可由 CLI 以守护方式持续运行
- MIME 保真：围绕原始 MIME 数据组织处理流程，降低中间转换带来的结构偏差
- 明文暴露收敛：缩短邮件以明文形式驻留于磁盘、内存和中间工件中的时间窗口
- 模块解耦：登录、发现、下载、加密、回写、审计等能力分别建模
- 协议抽象：provider 接口与具体协议实现分离，便于扩展新的邮件服务
- 邮件级事务：以“单封邮件”为最小处理单元，显式建模幂等、确认与源端删除
- 审计可追踪：关键步骤以 JSONL 形式记录，便于留痕与排障

## 代码结构

项目目录按入口层、模块层、协议层和基础设施层划分：

- `cmd/mimecrypt`：程序入口，负责启动 CLI 与信号感知退出
- `internal/cli`：Cobra 命令树与参数绑定
- `internal/modules/login`：登录与账号验证
- `internal/modules/logout`：本地认证状态清理
- `internal/modules/list`：邮件摘要列表
- `internal/modules/download`：原始 MIME 下载与保存
- `internal/modules/encrypt`：PGP/MIME 加密与已加密邮件识别
- `internal/modules/writeback`：回写与幂等对账抽象
- `internal/modules/process`：单封邮件处理链路编排
- `internal/modules/discover`：增量发现与循环处理
- `internal/mailflow`：邮件级事务模型、执行计划、协调器与状态存储
- `internal/mailflow/adapters`：基于现有 provider / writeback 的 mailflow 适配层
- `internal/modules/audit`：关键流程审计日志写入
- `internal/modules/backup`：本地备份落盘
- `internal/modules/health`：运行环境与连通性检查
- `internal/provider`：统一接口契约与跨模块数据结构
- `internal/providers`：provider 装配入口
- `internal/providers/imap`：IMAP OAuth 收信与回写实现
- `internal/providers/graph`：Microsoft Graph 读信与 Graph/EWS 回写实现
- `internal/auth`：OAuth 登录、token 缓存与刷新
- `internal/appconfig`：环境变量、本地配置与状态路径管理
- `internal/mimefile`：MIME 文件保存逻辑
- `internal/fileutil`：原子写入等通用文件工具
- `internal/mimeutil`：MIME 头部识别与 MimeCrypt 标记校验

该结构围绕“模块编排依赖接口，协议实现封装于 provider”这一原则组织，便于在保持主链路稳定的前提下扩展新的接入方式。

## Mailflow 架构演进

项目当前正在从“单一 provider + 单一处理链路”演进到以邮件为中心的三层模型：

1. 邮件生产 `Producer`
   - 负责从 Graph、IMAP、Gmail API、HTTP webhook、SMTP ingress 等来源产出统一邮件对象。
   - 输出统一的 `MailEnvelope`，其中包含原始 MIME 打开器、`MailTrace` 追踪上下文，以及可选的来源句柄。
2. 邮件处理 `Processor`
   - 负责以单封邮件为单位执行加密、备份、补头、审计，并根据规则生成该邮件的 `ExecutionPlan`。
   - 处理层不关心底层协议，只处理 MIME、邮件级上下文和执行计划。
3. 邮件消费 `Consumer`
   - 负责将处理结果写入一个或多个出口，并返回幂等写入的 `DeliveryReceipt`。
   - 消费层只负责“写成功、可校验、可重试”，不负责删除源邮件。

在三层之外，`TransactionCoordinator` 负责邮件级事务推进：持久化状态、驱动多个消费目标、在满足条件后确认来源、并在显式启用且满足同邮箱约束时删除源邮件。当前 `internal/mailflow` 已经实现了这套核心骨架，并提供：

- `MailEnvelope` / `MailTrace` / `ExecutionPlan` / `DeliveryReceipt`
- 基于文件系统的 `FileStateStore`
- 基于现有 `provider.Reader` 的 polling producer
- 基于现有 `provider.Reader` 的单封邮件 envelope builder
- 基于现有加密 / 备份 / 审计模块的 encrypting processor
- 基于本地目录的 file consumer
- 基于内存丢弃的 discard consumer
- 基于现有 `writeback.Service` 的 consumer 适配器

这套模型背后的关键约束如下：

- 幂等与状态推进以“单封邮件事务”为单位，而不是以同步周期或 provider 为单位。
- 删除源邮件不再作为 sink / writer 的隐式副作用，而是协调层的显式步骤。
- 只有当删除策略启用、必需出口已提交成功，并且目标 receipt 证明与来源属于同一逻辑邮箱存储时，才允许删除源邮件。
- 处理计划一旦针对某封邮件持久化，就不应在重试过程中被新的规则结果覆盖，避免路由漂移。
- 已加密邮件在 `mailflow` 中属于终态跳过：记录审计后确认来源，不再无限重试。

### Source / Sink Driver 方向

后续配置模型会继续向类似 `rclone remote` 的命名驱动方式收敛。设计方向如下：

- `credential`：命名凭据，封装 OAuth、basic auth、bearer token、证书等秘密材料。
- `source`：命名邮件来源，声明 `driver`、`credential_ref`、监听或轮询参数。
- `sink`：命名邮件出口，声明 `driver`、`credential_ref`、目标邮箱或目标文件夹。
- `route`：按邮件规则将一个 source 过来的邮件事务路由到一个或多个 sink。

也就是说，未来会以“命名 source / sink driver”而不是单一全局 provider 作为主要配置单位。这样同一个实例可以同时接入多个来源和多个出口。

当前仓库中的 CLI 和环境变量配置仍然主要围绕单一 provider 组织。当前 `run`、`process`、`flow-run` 已经共用 `mailflow` 编排，但命名 source / sink / route / credential 模型仍未落地，因此 `mailflow` 还没有完全演进到最终形态。

## 当前能力范围

当前版本已经具备以下能力：

- 基于 `cobra` 的完整 CLI 命令体系
- Microsoft Entra device code 登录
- 文件存储与系统 keyring 两类 token 存储后端
- token 刷新、导入、状态检查与登出清理
- IMAP OAuth 读信、列信、原始 MIME 获取与 IMAP `APPEND` 回写
- Graph 读信以及 Graph / EWS 兼容回写路径
- 单封邮件处理与持续同步处理
- 已加密邮件识别，避免重复加密
- 基于本地 `gpg` 的 PGP/MIME 生成
- 原文件夹回写与指定文件夹回写
- 回写校验与回写后对账能力
- 原始 MIME 的加密备份
- 关键步骤 JSONL 审计日志
- 单实例运行锁与只读 / 深度两级健康检查
- `mailflow` 事务骨架、文件状态存储以及 producer / processor / consumer 适配层
- `run` / `process` / `flow-run` 共用 `mailflow` 协调器

后续规划方向包括：

- 继续收敛剩余 legacy 模块与兼容层，减少对旧 `discover` / `process` 编排的依赖
- `google` / `gmail` 来源驱动
- HTTP webhook 与 SMTP ingress 等 push 型来源
- 命名 source / sink / route / credential 配置模型
- 更细粒度的邮件路由与密钥选择策略

## Provider 与回写后端

当前内置 provider 如下：

- `imap`：基于 IMAP OAuth 的邮件发现、读取与回写
- `graph`：基于 Microsoft Graph 的邮件发现与读取

当前可选回写后端如下：

- `imap`：通过 IMAP `APPEND` 直接写入目标邮箱文件夹
- `graph`：通过 Microsoft Graph API 导入 MIME
- `ews`：通过 EWS SOAP 接口导入 MIME

其中，`imap` 为缺省读写路径。该路径围绕 RFC 822 / MIME 组织读写流程，并在回写阶段保留源邮件时间元数据，便于维持客户端中的时间线一致性。

## 常用命令

登录并写入本地 token 缓存：

```bash
go run ./cmd/mimecrypt login
go run ./cmd/mimecrypt login your-mailbox@example.com
```

清理本地认证状态：

```bash
go run ./cmd/mimecrypt logout
```

检查运行环境、缓存 token 与连通性状态：

```bash
go run ./cmd/mimecrypt health
go run ./cmd/mimecrypt health --timeout 20s
go run ./cmd/mimecrypt health --deep
```

查看或导入 token：

```bash
go run ./cmd/mimecrypt token status
go run ./cmd/mimecrypt token import ./token.json
cat ./token.json | go run ./cmd/mimecrypt token import -
```

按邮件标识下载原始 MIME：

```bash
go run ./cmd/mimecrypt download <message-id> --output-dir ./output
go run ./cmd/mimecrypt download <message-id> --folder INBOX --output-dir ./output
```

列出指定文件夹中最新的一段邮件摘要：

```bash
go run ./cmd/mimecrypt list 10
go run ./cmd/mimecrypt list 10 20
go run ./cmd/mimecrypt list 0 5 --folder INBOX
```

其中，`list 10` 表示范围 `[0,10)`，`list 10 20` 表示范围 `[10,20)`，结果按接收时间倒序排列。

将本地 MIME 文件加密为 RFC 3156 PGP/MIME：

```bash
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --key 0xDEADBEEF
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --recipient alice@example.com --recipient bob@example.com
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --protect-subject
```

按邮件标识执行单封邮件处理：

```bash
go run ./cmd/mimecrypt process <message-id> --save-output --output-dir ./output
go run ./cmd/mimecrypt process <message-id> --folder INBOX --save-output --output-dir ./output
go run ./cmd/mimecrypt process <message-id> --backup-dir ./backup --audit-log-path ./audit.jsonl
go run ./cmd/mimecrypt process <message-id> --backup-key-id 0xDEADBEEF
go run ./cmd/mimecrypt process <message-id> --write-back
go run ./cmd/mimecrypt process <message-id> --write-back --write-back-provider imap
go run ./cmd/mimecrypt process <message-id> --write-back --write-back-folder archive
go run ./cmd/mimecrypt process <message-id> --protect-subject
```

持续发现并处理邮件：

```bash
go run ./cmd/mimecrypt run --once
go run ./cmd/mimecrypt run --poll-interval 1m --save-output --output-dir ./output
go run ./cmd/mimecrypt run --once --backup-dir ./backup --audit-log-path ./audit.jsonl
go run ./cmd/mimecrypt run --once --backup-key-id 0xDEADBEEF
go run ./cmd/mimecrypt run --once --write-back
go run ./cmd/mimecrypt run --once --write-back --write-back-provider imap
go run ./cmd/mimecrypt run --once --write-back --write-back-folder archive
go run ./cmd/mimecrypt run --once --protect-subject
```

调试模式用于处理当前文件夹中最新的一封邮件：

```bash
go run ./cmd/mimecrypt run --debug-save-first --save-output --output-dir ./output
```

基于 `mailflow` 执行邮件级事务处理：

```bash
go run ./cmd/mimecrypt flow-run --once --save-output --output-dir ./output
go run ./cmd/mimecrypt flow-run --once --write-back --verify-write-back
go run ./cmd/mimecrypt flow-run --once --write-back --delete-source
go run ./cmd/mimecrypt flow-run --poll-interval 1m --save-output --write-back
```

## 配置

项目内置一个 Microsoft Entra 应用 Client ID；部署方可通过环境变量覆盖：

```bash
export MIMECRYPT_CLIENT_ID="你的应用 Client ID"
```

常用配置示例如下：

```bash
export MIMECRYPT_PROVIDER="imap"
export MIMECRYPT_TENANT="organizations"
export MIMECRYPT_STATE_DIR="$HOME/.config/mimecrypt"
export MIMECRYPT_OUTPUT_DIR="./output"
export MIMECRYPT_SAVE_OUTPUT="false"
export MIMECRYPT_PROTECT_SUBJECT="false"
export MIMECRYPT_BACKUP_DIR="./backup"
export MIMECRYPT_BACKUP_KEY_ID=""
export MIMECRYPT_AUDIT_LOG_PATH="$HOME/.config/mimecrypt/audit.jsonl"
export MIMECRYPT_AUDIT_STDOUT="false"
export MIMECRYPT_FOLDER="INBOX"
export MIMECRYPT_WRITEBACK_PROVIDER="imap"
export MIMECRYPT_WRITEBACK_FOLDER=""
export MIMECRYPT_GRAPH_SCOPES="https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/User.Read offline_access openid profile"
export MIMECRYPT_IMAP_SCOPES="https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
export MIMECRYPT_IMAP_ADDR="outlook.office365.com:993"
export MIMECRYPT_IMAP_USERNAME="your-mailbox@example.com"
export MIMECRYPT_EWS_SCOPES="https://outlook.office365.com/EWS.AccessAsUser.All"
export MIMECRYPT_EWS_BASE_URL="https://outlook.office365.com/EWS/Exchange.asmx"
export MIMECRYPT_TOKEN_STORE="file"
export MIMECRYPT_KEYRING_SERVICE="mimecrypt"
```

加密相关配置示例如下：

```bash
export MIMECRYPT_PGP_RECIPIENTS="alice@example.com,bob@example.com"
export MIMECRYPT_GPG_BINARY="gpg"
export MIMECRYPT_GPG_TRUST_MODEL="auto"
export MIMECRYPT_WORK_DIR=""
```

各项配置含义如下：

- `MIMECRYPT_PROVIDER`：收信 provider。可选 `imap`、`graph`；缺省值 `imap`。
- `MIMECRYPT_CLIENT_ID`：Microsoft Entra 应用 Client ID。缺省值为项目内置应用 ID。
- `MIMECRYPT_STATE_DIR`：状态目录，保存 token、本地配置与同步状态。
- `MIMECRYPT_OUTPUT_DIR`：加密后 `.eml` 的本地输出目录，仅在启用 `save-output` 时生效。
- `MIMECRYPT_SAVE_OUTPUT`：控制是否额外保存本地 `.eml` 输出；缺省值 `false`。
- `MIMECRYPT_WORK_DIR`：处理链路使用的临时工作目录；为空时使用系统临时目录。
- `MIMECRYPT_PROTECT_SUBJECT`：控制外层 `Subject` 是否写为 `...`。
- `MIMECRYPT_BACKUP_DIR`：原始 MIME 密文备份目录。
- `MIMECRYPT_BACKUP_KEY_ID`：备份加密使用的 catch-all GPG key id；设置后统一使用该密钥进行备份加密。
- `MIMECRYPT_AUDIT_LOG_PATH`：追加式 JSONL 审计日志路径。
- `MIMECRYPT_AUDIT_STDOUT`：控制审计日志是否同步输出到 stdout。
- `MIMECRYPT_WRITEBACK_PROVIDER`：回写后端。可选 `imap`、`graph`、`ews`；缺省值 `imap`。
- `MIMECRYPT_WRITEBACK_FOLDER`：目标回写文件夹；为空时沿用源文件夹。
- `MIMECRYPT_IMAP_SCOPES`：IMAP OAuth scopes。
- `MIMECRYPT_IMAP_ADDR`：IMAP 服务地址，典型值为 `outlook.office365.com:993`。
- `MIMECRYPT_IMAP_USERNAME`：IMAP 登录用户名，通常为邮箱地址。使用 `imap` provider 时应提供该值。
- `login [imap-username]`：支持将 IMAP 用户名写入本地配置。优先级顺序为 `MIMECRYPT_IMAP_USERNAME`、`--imap-username`、`login` 参数、本地已保存值。
- `MIMECRYPT_EWS_SCOPES`：EWS 回写使用的 OAuth scopes。
- `MIMECRYPT_EWS_BASE_URL`：EWS SOAP 端点地址。
- `MIMECRYPT_PGP_RECIPIENTS`：补充或覆盖收件人邮箱列表；当邮件头缺少 `To/Cc/Bcc` 时应显式提供。
- `MIMECRYPT_GPG_BINARY`：本地 `gpg` 可执行文件路径。
- `MIMECRYPT_GPG_TRUST_MODEL`：`gpg --trust-model` 取值。缺省值 `auto`，可选 `always`、`auto`、`classic`、`direct`、`tofu`、`tofu+pgp`、`pgp`。
- `MIMECRYPT_TOKEN_STORE`：token 存储后端。可选 `file`、`keyring`；缺省值 `file`。
- `MIMECRYPT_KEYRING_SERVICE`：`keyring` 模式下的服务名；缺省值 `mimecrypt`。

补充说明如下：

- 未加密邮件通过本地 `gpg` 生成 `PGP/MIME (RFC 3156)`；收件人公钥应已导入 keyring。
- `encrypt` 命令默认根据输入 MIME 的 `To/Cc/Bcc` 推断收件人并匹配同邮箱公钥；`--recipient` 接受邮箱地址，`--key` 接受显式 GPG key 标识。
- 显式传入的 `--recipient` 与 `--key` 值会执行输入校验，避免污染 GPG 参数语义。
- 加密阶段进入 `gpg` 的明文载荷始终为原始 MIME 字节；解密结果应恢复原始邮件内容与头部信息。
- `process` 与 `run` 缺省仅产出 `backup/*.pgp` 密文备份，不额外写入本地 `.eml`。
- 启用 `--save-output` 或 `MIMECRYPT_SAVE_OUTPUT=true` 后，才会额外生成 `output/*.eml`。
- `backup/*.pgp` 始终针对原始 MIME 源字节加密；若设置了 `MIMECRYPT_BACKUP_KEY_ID`，则统一使用该密钥生成备份。
- 加密输出的外层包装会增加 `X-MimeCrypt-Processed: yes`，用于标记该邮件经过 MimeCrypt 处理；解密后的原始 MIME 不包含该头部。
- IMAP 回写阶段优先保留源邮件的 `INTERNALDATE`；当源元数据缺失时，使用 MIME `Date` 头作为回退值。
- 早期版本生成的 token 可能不包含 IMAP scopes；升级后可执行 `logout` 与 `login` 以重新获取令牌。
- `run` 按 `provider + folder` 维度申请单实例运行锁；同一状态目录下的重复任务会被拒绝启动。
- `health` 缺省执行只读检查；`health --deep` 追加 provider 与 writeback 连通性探测，并可能触发 token 刷新。
- `token status` 用于读取当前 token 状态；`token import` 用于从 JSON 文件或标准输入导入 token。
- `logout` 会同时清理文件 token 与 keyring token。

## 回写方式

### IMAP 回写

`imap` 回写后端通过 IMAP `APPEND` 将完整 MIME 写入目标文件夹，具有以下特征：

- 处理对象为原生 RFC 822 / MIME 数据
- 回写结果在邮箱中呈现为新增邮件对象
- 文件夹标识使用 mailbox 名称
- 命令行中的 `message-id` 在 IMAP 路径下对应邮件 `UID`
- `MIMECRYPT_IMAP_USERNAME` 参与 IMAP 登录与命令执行

### API 回写

`graph` 与 `ews` 回写后端通过服务端 API 导入 MIME，具有以下特征：

- 回写动作由服务端 API 完成，而非标准邮箱协议追加
- 文件夹标识与消息标识采用服务端接口定义
- Graph 路径在 Outlook Web 中可能呈现为 draft
- EWS 路径能够保留更接近收件邮件的语义，但其生命周期已进入退役阶段

启用 `--protect-subject` 或 `MIMECRYPT_PROTECT_SUBJECT=true` 后，外层 `Subject` 统一写为 `...`；解密后的原始主题保持不变。

## 默认处理链路

当前默认处理链路如下：

1. 通过 IMAP OAuth 发现并读取原始 MIME
2. 在本地构造加密后的 `PGP/MIME`
3. 通过 IMAP `APPEND` 写回目标文件夹
4. 通过回写校验与对账逻辑确认目标邮件已存在
5. 删除原始明文邮件

该设计具有以下技术特征：

- 读写链路围绕 RFC 822 / MIME 展开
- 回写阶段可保留源邮件 `INTERNALDATE`
- IMAP 回写路径可避免 Graph draft 语义带来的展示偏差
- provider 抽象保留了 Graph 与 EWS 的兼容接入能力

## 状态文件与输出目录

运行过程中可能生成以下文件：

- `token.json`：文件模式下的 token 缓存文件
- `sync-<folder>.json`：增量同步状态文件
- `audit.jsonl`：关键流程审计日志
- `output/*.eml`：显式启用 `save-output` 后生成的 PGP/MIME 文件
- `backup/*.pgp`：原始 MIME 的本地密文备份

当 `MIMECRYPT_TOKEN_STORE=keyring` 时，主存储切换为系统 keyring，旧的文件 token 会在迁移完成后清理。

## Docker 部署

Docker 部署以单实例、有状态运行模型为前提。部署约束如下：

- 当前运行模型按单实例设计
- `/state` 与 `/gnupg` 应挂载持久卷
- 生产环境宜采用 `imap` 读信与 `imap` 回写
- 容器环境宜使用 `MIMECRYPT_TOKEN_STORE=file`
- `./state`、`./gnupg` 目录权限宜设置为 `0700`
- 导入使用的 `token.json` 或 `./secrets/token.json` 文件权限宜设置为 `0600`

构建镜像：

```bash
docker build -t mimecrypt:local .
```

首轮初始化可采用以下方式之一：

1. 在宿主机执行 `login`，将生成的 `token.json` 放入挂载的 `./state`
2. 使用 `token import` 导入既有 token JSON

使用 `compose.example.yml` 时，可先执行一次性导入：

```bash
docker compose -f compose.example.yml build
docker compose -f compose.example.yml run --rm mimecrypt-token-import
```

随后启动主服务：

```bash
docker compose -f compose.example.yml up -d mimecrypt
```

参考卷布局如下：

- `./state -> /state`：token、同步状态、本地配置与可选审计文件
- `./backup -> /backup`：原始 MIME 密文备份
- `./gnupg -> /gnupg`：GPG keyring
- `/tmp`：处理临时目录，可使用 `tmpfs`

参考容器环境变量如下：

```bash
MIMECRYPT_PROVIDER=imap
MIMECRYPT_WRITEBACK_PROVIDER=imap
MIMECRYPT_STATE_DIR=/state
MIMECRYPT_BACKUP_DIR=/backup
MIMECRYPT_WORK_DIR=/tmp/mimecrypt
MIMECRYPT_AUDIT_LOG_PATH=
MIMECRYPT_AUDIT_STDOUT=false
MIMECRYPT_TOKEN_STORE=file
GNUPGHOME=/gnupg
```

`MIMECRYPT_WORK_DIR` 的容量可按最大单封邮件体积估算：

- 未启用 catch-all 备份密钥时，可按 `最大邮件体积 x3` 规划
- 启用 `MIMECRYPT_BACKUP_KEY_ID` 时，可按 `最大邮件体积 x4` 规划
- 当 `tmpfs` 容量规划受限时，可将 `WORK_DIR` 挂载到独立卷

容器中的只读健康检查示例如下：

```bash
docker run --rm \
  -e MIMECRYPT_STATE_DIR=/state \
  -e MIMECRYPT_TOKEN_STORE=file \
  -e MIMECRYPT_IMAP_USERNAME=your-mailbox@example.com \
  -v "$(pwd)/state:/state" \
  -v "$(pwd)/gnupg:/gnupg" \
  mimecrypt:local health
```

人工排障时，可执行深度检查：

```bash
docker run --rm \
  -e MIMECRYPT_STATE_DIR=/state \
  -e MIMECRYPT_TOKEN_STORE=file \
  -e MIMECRYPT_IMAP_USERNAME=your-mailbox@example.com \
  -v "$(pwd)/state:/state" \
  -v "$(pwd)/gnupg:/gnupg" \
  mimecrypt:local health --deep
```

部署补充说明如下：

- 当前 Docker 形态为单实例、有状态、持久卷模式
- Graph 与 EWS 回写路径在大附件场景下具有更高的内存占用
- `health` 缺省可用作 `HEALTHCHECK`；`health --deep` 主要用于人工诊断
- 启用 `MIMECRYPT_AUDIT_STDOUT=true` 后，审计事件中的邮件元数据会进入容器日志系统
- `compose.example.yml` 用于单机持久卷部署示例

## 后续工作

后续开发重点如下：

1. 增加 `google` provider
2. 引入 webhook 接收与事件路由能力
3. 继续收敛默认处理链路中的明文落盘范围
4. 完善密钥管理与收件人路由策略

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](./LICENSE) file for details.
Copyright © 2026 [nhir](https://github.com/nhirsama).
