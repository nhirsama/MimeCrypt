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
- `internal/appruntime`：登录、登出、token 等凭据相关运行时装配
- `internal/flowruntime`：topology 解析、source/route 运行计划与 mailflow 装配
- `internal/modules/login`：登录与账号验证
- `internal/modules/logout`：本地认证状态清理
- `internal/modules/list`：邮件摘要列表
- `internal/modules/download`：原始 MIME 下载与保存
- `internal/modules/encrypt`：PGP/MIME 加密与已加密邮件识别
- `internal/modules/writeback`：回写与幂等对账抽象
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
- `internal/appconfig`：环境变量、本地配置、topology 与状态路径管理
- `internal/mimefile`：MIME 文件保存逻辑
- `internal/fileutil`：原子写入等通用文件工具
- `internal/mimeutil`：MIME 头部识别与 MimeCrypt 标记校验

该结构围绕“模块编排依赖接口，协议实现封装于 provider”这一原则组织，便于在保持主链路稳定的前提下扩展新的接入方式。

## Mailflow 架构

当前版本已经收口为以邮件为处理对象的三层模型：

1. `Producer`
   负责从命名 `source` 拉取邮件，并输出统一的 `MailEnvelope`。
2. `Processor`
   负责读取原始 MIME、识别已加密邮件、执行加密、备份、审计，并产出 `ExecutionPlan`。
3. `Consumer`
   负责把处理结果写入一个或多个命名 `sink`，并返回可持久化的 `DeliveryReceipt`。

`internal/mailflow.Coordinator` 负责邮件级事务推进：持久化计划和投递结果、在必需出口全部成功后确认来源，并在 route 明确启用 `delete_source` 时执行源端删除。删除动作不再是 writer 的隐式副作用，而是协调层的显式步骤。

运行时现在是 `topology-only`：

- `run` / `process` / `download` / `list` / `health` 只接受 topology 中的命名 `source` / `route`
- `login` / `token` / `logout` 只接受 topology 中的命名 `credential`
- source / sink driver、文件夹、回写目标、本地文件输出都通过 topology 声明，而不是命令行临时拼装
- `run` 使用持久化事务状态；`process` 缺省使用 `ephemeral` 事务状态，也可切到 `persistent`

## Topology 模型

运行时配置围绕四个一等对象组织：

- `credential`：认证与秘密材料，覆盖 `state_dir`、token store、协议 scopes、`imap_username` 等
- `source`：邮件来源，声明 `driver`、`credential_ref`、轮询方式、文件夹、状态路径和节奏
- `sink`：邮件出口，声明 `driver`、`credential_ref`、目标邮箱文件夹或本地输出目录
- `route`：把一个或多个 `source` 路由到若干 `sink`，并声明必需投递、删除源邮件策略等

这套模型已经是当前代码的真实运行边界，而不是兼容层上的包装。

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
- `run` / `process` / `download` / `list` / `health` 通过 topology 文件选择命名 `source`；其中 `run` / `process` / `health` 还支持命名 `route`
- `login` / `token` / `logout` 可通过 topology 文件选择命名 `credential`
- `run` / `process` 共用 `mailflow` 协调器，单封处理显式使用 ephemeral 事务状态，持续运行显式使用 persistent 事务状态

后续规划方向包括：

- `google` / `gmail` 来源驱动
- HTTP webhook 与 SMTP ingress 等 push 型来源
- 更多 driver 与凭据后端
- 更细粒度的邮件路由与密钥选择策略

## Driver 能力

当前内置 driver 如下：

- `source`: `imap`、`graph`
- `sink`: `file`、`discard`、`imap`、`graph`、`ews`

其中：

- `imap` sink 通过 IMAP `APPEND` 写入目标文件夹，并支持幂等对账与 hard delete 语义
- `graph` / `ews` sink 通过服务端 API 导入 MIME
- `graph` / `ews` 对源邮件删除属于 soft delete 语义，因此 `mailflow` 会拒绝把它们当成安全删除来源

## 常用命令

登录并写入本地 token 缓存：

```bash
go run ./cmd/mimecrypt login
go run ./cmd/mimecrypt login your-mailbox@example.com
go run ./cmd/mimecrypt login --topology-file ./topology.json --credential office-auth
```

清理本地认证状态：

```bash
go run ./cmd/mimecrypt logout
go run ./cmd/mimecrypt logout --topology-file ./topology.json --credential office-auth
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
go run ./cmd/mimecrypt token status --topology-file ./topology.json --credential office-auth
```

按邮件标识下载原始 MIME：

```bash
go run ./cmd/mimecrypt download <message-id> --output-dir ./output
go run ./cmd/mimecrypt download <message-id> --topology-file ./topology.json --source office-inbox --output-dir ./output
```

列出 source 对应文件夹中最新的一段邮件摘要：

```bash
go run ./cmd/mimecrypt list 10
go run ./cmd/mimecrypt list 10 20
go run ./cmd/mimecrypt list 0 5 --topology-file ./topology.json --source office-inbox
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
go run ./cmd/mimecrypt process <message-id> --backup-dir ./backup --audit-log-path ./audit.jsonl
go run ./cmd/mimecrypt process <message-id> --backup-key-id 0xDEADBEEF
go run ./cmd/mimecrypt process <message-id> --protect-subject
go run ./cmd/mimecrypt process <message-id> --transaction-mode persistent
go run ./cmd/mimecrypt process <message-id> --topology-file ./topology.json --source office-inbox
```

持续发现并处理邮件：

```bash
go run ./cmd/mimecrypt run --once
go run ./cmd/mimecrypt run --once --backup-dir ./backup --audit-log-path ./audit.jsonl
go run ./cmd/mimecrypt run --once --backup-key-id 0xDEADBEEF
go run ./cmd/mimecrypt run --once --protect-subject
go run ./cmd/mimecrypt run --topology-file ./topology.json --route default --source office-inbox
```

调试模式用于处理当前文件夹中最新的一封邮件：

```bash
go run ./cmd/mimecrypt run --debug-save-first --topology-file ./topology.json --source office-inbox
```

使用命名 topology 文件：

```bash
go run ./cmd/mimecrypt run --topology-file ./topology.json --route default --source office-inbox
go run ./cmd/mimecrypt process <message-id> --topology-file ./topology.json --source office-inbox
go run ./cmd/mimecrypt download <message-id> --topology-file ./topology.json --source office-inbox --output-dir ./output
go run ./cmd/mimecrypt list 0 10 --topology-file ./topology.json --source office-inbox
go run ./cmd/mimecrypt health --topology-file ./topology.json --route default --source office-inbox --deep
```

## 配置

项目内置一个 Microsoft Entra 应用 Client ID；部署方可通过环境变量覆盖。当前入口配置分为两层：

- 基础运行时参数：state dir、OAuth client、协议 endpoint、GPG 与审计参数
- topology 文件：credential/source/sink/route

常用基础环境变量如下：

```bash
export MIMECRYPT_CLIENT_ID="你的应用 Client ID"
export MIMECRYPT_TOPOLOGY_PATH="$HOME/.config/mimecrypt/topology.json"
export MIMECRYPT_TENANT="organizations"
export MIMECRYPT_STATE_DIR="$HOME/.config/mimecrypt"
export MIMECRYPT_OUTPUT_DIR="./output"
export MIMECRYPT_PROTECT_SUBJECT="false"
export MIMECRYPT_BACKUP_DIR="./backup"
export MIMECRYPT_BACKUP_KEY_ID=""
export MIMECRYPT_AUDIT_LOG_PATH="$HOME/.config/mimecrypt/audit.jsonl"
export MIMECRYPT_AUDIT_STDOUT="false"
export MIMECRYPT_GRAPH_SCOPES="https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/User.Read offline_access openid profile"
export MIMECRYPT_IMAP_SCOPES="https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
export MIMECRYPT_IMAP_ADDR="outlook.office365.com:993"
export MIMECRYPT_IMAP_USERNAME="your-mailbox@example.com"
export MIMECRYPT_EWS_SCOPES="https://outlook.office365.com/EWS.AccessAsUser.All"
export MIMECRYPT_EWS_BASE_URL="https://outlook.office365.com/EWS/Exchange.asmx"
export MIMECRYPT_TOKEN_STORE="file"
export MIMECRYPT_KEYRING_SERVICE="mimecrypt"
```

`MIMECRYPT_TOPOLOGY_PATH` 现在是主链路必需配置；命令行 `--topology-file` 仅用于覆盖默认路径。

一个最小 topology 文件示例如下：

```json
{
  "default_credential": "office-auth",
  "default_source": "office-inbox",
  "default_route": "default",
  "credentials": {
    "office-auth": {
      "kind": "oauth",
      "token_store": "keyring",
      "imap_username": "user@example.com"
    }
  },
  "sources": {
    "office-inbox": {
      "driver": "imap",
      "credential_ref": "office-auth",
      "mode": "poll",
      "folder": "INBOX",
      "poll_interval": 60000000000,
      "cycle_timeout": 120000000000
    }
  },
  "sinks": {
    "archive": {
      "driver": "file",
      "output_dir": "./output"
    }
  },
  "routes": {
    "default": {
      "source_refs": ["office-inbox"],
      "targets": [
        {
          "sink_ref": "archive",
          "artifact": "primary",
          "required": true
        }
      ]
    }
  }
}
```

加密相关配置示例如下：

```bash
export MIMECRYPT_PGP_RECIPIENTS="alice@example.com,bob@example.com"
export MIMECRYPT_GPG_BINARY="gpg"
export MIMECRYPT_GPG_TRUST_MODEL="auto"
export MIMECRYPT_WORK_DIR=""
```

各项配置含义如下：

- `MIMECRYPT_TOPOLOGY_PATH`：命名 topology 配置文件路径；设置后 `run`、`process`、`download`、`list`、`health`、`login`、`token`、`logout` 会优先使用 topology 文件。
- `MIMECRYPT_CLIENT_ID`：Microsoft Entra 应用 Client ID。缺省值为项目内置应用 ID。
- `MIMECRYPT_STATE_DIR`：状态目录，保存 token、本地配置与同步状态。
- `MIMECRYPT_OUTPUT_DIR`：`download` 命令的默认输出目录；mailflow 本地输出应通过 `file` sink 在 topology 中声明。
- `MIMECRYPT_WORK_DIR`：处理链路使用的临时工作目录；为空时使用系统临时目录。
- `MIMECRYPT_PROTECT_SUBJECT`：控制外层 `Subject` 是否写为 `...`。
- `MIMECRYPT_BACKUP_DIR`：原始 MIME 密文备份目录。
- `MIMECRYPT_BACKUP_KEY_ID`：备份加密使用的 catch-all GPG key id；设置后统一使用该密钥进行备份加密。
- `MIMECRYPT_AUDIT_LOG_PATH`：追加式 JSONL 审计日志路径。
- `MIMECRYPT_AUDIT_STDOUT`：控制审计日志是否同步输出到 stdout。
- `MIMECRYPT_IMAP_SCOPES`：IMAP OAuth scopes。
- `MIMECRYPT_IMAP_ADDR`：IMAP 服务地址，典型值为 `outlook.office365.com:993`。
- `MIMECRYPT_IMAP_USERNAME`：IMAP 登录用户名，通常为邮箱地址。使用 `imap` provider 时应提供该值。
- `login [imap-username]`：支持将 IMAP 用户名写入本地配置。优先级顺序为 `MIMECRYPT_IMAP_USERNAME`、`--imap-username`、`login` 参数、本地已保存值。
- `MIMECRYPT_EWS_SCOPES`：EWS 回写使用的 OAuth scopes。
- `MIMECRYPT_EWS_BASE_URL`：EWS SOAP 端点地址。
- `MIMECRYPT_PGP_RECIPIENTS`：补充或覆盖收件人邮箱列表；当邮件头缺少 `To/Cc/Bcc` 时应显式提供。
- `MIMECRYPT_GPG_BINARY`：本地 `gpg` 可执行文件路径。
- `MIMECRYPT_GPG_HOME`：仅供 MimeCrypt 调用 `gpg` 时使用的 `GNUPGHOME`，用于隔离默认 `~/.gnupg`。
- `MIMECRYPT_GPG_TRUST_MODEL`：`gpg --trust-model` 取值。缺省值 `auto`，可选 `always`、`auto`、`classic`、`direct`、`tofu`、`tofu+pgp`、`pgp`。
- `MIMECRYPT_TOKEN_STORE`：token 存储后端。可选 `file`、`keyring`；缺省值 `file`。
- `MIMECRYPT_KEYRING_SERVICE`：`keyring` 模式下的服务名；缺省值 `mimecrypt`。

补充说明如下：

- 未加密邮件通过本地 `gpg` 生成 `PGP/MIME (RFC 3156)`；收件人公钥应已导入 keyring。
- `encrypt` 命令默认根据输入 MIME 的 `To/Cc/Bcc` 推断收件人并匹配同邮箱公钥；`--recipient` 接受邮箱地址，`--key` 接受显式 GPG key 标识。
- 显式传入的 `--recipient` 与 `--key` 值会执行输入校验，避免污染 GPG 参数语义。
- 加密阶段进入 `gpg` 的明文载荷始终为原始 MIME 字节；解密结果应恢复原始邮件内容与头部信息。
- `process` 与 `run` 是否产生本地 `.eml`，由 topology 中是否配置 `file` sink 决定，不再由命令行临时启用。
- `mailflow` 工作目录中的临时文件默认是加密产物（`armored-*.asc`、`encrypted-*.eml`、可选 `backup-*.pgp`），不是原始明文 MIME；异常中断后需要关注的是磁盘卫生与临时件清理，而不是把这些文件误判成明文落盘。
- `backup/*.pgp` 始终针对原始 MIME 源字节加密；若设置了 `MIMECRYPT_BACKUP_KEY_ID`，则统一使用该密钥生成备份。
- 加密输出的外层包装会增加 `X-MimeCrypt-Processed: yes`，用于标记该邮件经过 MimeCrypt 处理；解密后的原始 MIME 不包含该头部。
- IMAP 回写阶段优先保留源邮件的 `INTERNALDATE`；当源元数据缺失时，使用 MIME `Date` 头作为回退值。
- token 默认存储在状态目录下的 `token.json`；只有显式设置 `MIMECRYPT_TOKEN_STORE=keyring` 时，系统 keyring 才会成为主存储。
- 文件 token 写入现在使用 `internal/fileutil.WriteFileAtomic`；同一 credential 的 token 刷新会在进程内和同机多进程间串行化。
- topology credential 可以覆盖 `state_dir`、`client_id`、`tenant`、`token_store`、`keyring_service`、各协议 scopes 和 `imap_username`。当命名 credential 未显式声明 `state_dir` 时，默认会落到 `<state_dir>/credentials/<credential-name>`。
- topology credential 切换后，IMAP 用户名会优先读取该 credential 自己 state dir 下的本地配置，不再继承基础 state dir 中缓存的用户名。
- topology JSON 现在按严格模式解析；未知字段或额外 JSON 文档都会直接报错，避免因拼写错误静默退回到更宽松的行为。
- `run` 的运行锁、producer 状态和事务状态会按 `route + source + driver + folder` 作用域隔离。
- topology 模式下，持续运行会按所选 `source.poll_interval` 驱动轮询，而不是回退到全局默认间隔。
- `health` 缺省执行只读检查；`health --deep` 追加来源连通性探测，并在 topology 模式下按选定 route 的远端 sink 逐个执行 writeback 健康探测。
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
- Graph / EWS 对“删除源邮件”的实现语义属于 soft delete；因此 `mailflow` 在启用 delete-source 时只接受声明为 hard delete 的 source，避免把“移到已删除邮件”误当成安全删除。

启用 `--protect-subject` 或 `MIMECRYPT_PROTECT_SUBJECT=true` 后，外层 `Subject` 统一写为 `...`；解密后的原始主题保持不变。

## 状态文件与输出目录

运行过程中可能生成以下文件：

- `token.json`：文件模式下的 token 缓存文件，也是缺省 token store
- `flow-sync-<scope>.json`：polling source 的增量同步状态
- `flow-state/<scope>/`：mailflow 事务状态目录
- `audit.jsonl`：关键流程审计日志
- `output/*.eml`：`download` 命令输出，或 topology `file` sink 写入的本地 MIME
- `backup/*.pgp`：原始 MIME 的本地密文备份

当 `MIMECRYPT_TOKEN_STORE=keyring` 时，主存储才会切换为系统 keyring；默认模式仍是状态目录内的 `token.json`。

## Docker 部署

Docker 部署以单实例、有状态运行模型为前提。部署约束如下：

- 当前运行模型按单实例设计
- `/state` 与 `/gnupg` 应挂载持久卷
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
MIMECRYPT_TOPOLOGY_PATH=/state/topology.json
MIMECRYPT_STATE_DIR=/state
MIMECRYPT_BACKUP_DIR=/backup
MIMECRYPT_WORK_DIR=/tmp/mimecrypt
MIMECRYPT_AUDIT_LOG_PATH=
MIMECRYPT_AUDIT_STDOUT=false
MIMECRYPT_TOKEN_STORE=file
MIMECRYPT_GPG_HOME=/gnupg
```

`MIMECRYPT_WORK_DIR` 的容量可按最大单封邮件体积估算：

- 未启用 catch-all 备份密钥时，可按 `最大邮件体积 x3` 规划
- 启用 `MIMECRYPT_BACKUP_KEY_ID` 时，可按 `最大邮件体积 x4` 规划
- 当 `tmpfs` 容量规划受限时，可将 `WORK_DIR` 挂载到独立卷

容器中的只读健康检查示例如下：

```bash
docker run --rm \
  -e MIMECRYPT_TOPOLOGY_PATH=/state/topology.json \
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
  -e MIMECRYPT_TOPOLOGY_PATH=/state/topology.json \
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
