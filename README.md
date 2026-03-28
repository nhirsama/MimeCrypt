# MimeCrypt

MimeCrypt 是一个面向服务器场景的自动化邮件加密工具。

它的目标是尽可能自动地完成以下链路：

- 接收新邮件事件
- 拉取原始 MIME 邮件
- 对邮件执行 GPG/PGP-MIME 加密，或识别其已经加密
- 将处理后的邮件回写到原邮箱
- 尽量减少明文邮件在系统中的暴露时间和泄露路径

这个项目最终会支持两类邮件接入方式：

- webhook 驱动的事件接入
- 主动轮询或增量同步的邮件发现

也会支持两类主要邮件提供方：

- Microsoft Graph API
- Google Gmail API

## 项目定位

MimeCrypt 的定位是一个自动化邮件加密中间层。

它运行在远程服务器或受控主机上，负责把“邮件到达”到“加密后回写”的流程串起来，而不是让用户手工导出、加密、再上传邮件。这样做的核心目的，是尽量缩短邮件以明文形式存在和流转的路径。

更具体地说，这个项目希望减少以下暴露面：

- 明文邮件长期保存在本地磁盘
- 明文邮件被多个中间程序重复读取
- 邮件先下载到客户端，再手工加密再上传
- 自动化链路中出现多份明文副本

## 目标链路

项目的目标处理链路如下：

1. 通过 webhook、增量同步或轮询发现新邮件
2. 通过 Microsoft Graph 或 Gmail API 拉取邮件原始 MIME
3. 判断邮件是否已经是加密邮件
4. 对未加密邮件执行 GPG 加密并生成标准 MIME 结构
5. 将加密后的邮件回写到原邮箱
6. 校验回写结果，确保自动归档链路可靠

对于 Thunderbird 等邮件客户端，目标格式是 RFC 3156 `multipart/encrypted`，以便邮件可以被标准 PGP/MIME 流程直接打开。

## 设计原则

- 自动化优先：登录完成后，后续流程尽量无人值守
- Provider 抽象：模块层不直接依赖 Graph 或 Gmail 的具体 API
- MIME 原样处理：尽量围绕原始 MIME 工作，减少中间转换损耗
- 最少暴露：尽量避免产生额外的明文副本
- 可扩展：登录、发现、下载、加密、回写、路由彼此分离

## 模块结构

业务模块保持为 8 个：

- `internal/modules/login`：登录并验证当前账号
- `internal/modules/logout`：清除本地登录状态
- `internal/modules/list`：列出最新邮件摘要
- `internal/modules/download`：按邮件 ID 下载原始 MIME
- `internal/modules/writeback`：回写邮件并校验
- `internal/modules/process`：按邮件 ID 和配置处理邮件
- `internal/modules/encrypt`：加密邮件
- `internal/modules/discover`：发现邮件并进行路由处理

底层支撑层负责协议与实现解耦：

- `internal/provider`：统一的认证、收件、回写接口契约
- `internal/providers`：按配置选择 provider 的工厂
- `internal/providers/graph`：当前 Microsoft Graph provider 的收件/回写实现
- `internal/auth`：Graph 登录、refresh token 刷新、token 缓存
- `internal/mimefile`：MIME 文件落盘
- `internal/appconfig`：配置读取
- `internal/cli`：CLI 命令树
- `cmd/mimecrypt`：程序入口

当前这套结构的目的，就是为后续增加 `internal/providers/google` 和 webhook 接入层时，不必推翻已有模块。

## 当前实现状态

当前已经完成的部分：

- 基于 `cobra` 的 CLI 结构
- Microsoft Graph 的 device code 登录
- token 本地缓存与自动刷新
- IMAP OAuth 收信与 MIME 拉取
- 统一 provider 抽象
- 按邮件 ID 下载邮件
- 调试模式处理第一封邮件
- 增量同步发现邮件的基础框架
- 已加密邮件识别与拒绝重复加密（PGP/S-MIME）
- 基于 `gpg` 的 PGP/MIME（RFC 3156）加密封装
- IMAP APPEND 回写、原文件夹保留回写与可选指定文件夹回写
- 回写后基础校验
- 关键流程 JSONL 审计日志
- 本地加密备份目录

当前还没有完成的部分：

- Google Gmail API provider
- webhook 接收入口
- 可配置的邮件路由规则

所以现阶段它仍然是一个“围绕自动加密目标设计的原型”，而不是完整可投产的最终版本。

## 当前可用 Provider

当前内置 provider：

- `imap`：IMAP OAuth 收信与回写
- `graph`：Microsoft Graph

计划中的 provider：

- `google`：Gmail API

## 当前可用命令

登录并缓存 token：

```bash
go run ./cmd/mimecrypt login
```

清除本地登录状态：

```bash
go run ./cmd/mimecrypt logout
```

按邮件 ID 下载原始 MIME：

```bash
go run ./cmd/mimecrypt download <message-id> --output-dir ./output
go run ./cmd/mimecrypt download <message-id> --folder INBOX --output-dir ./output
```

列出指定文件夹中最新一段邮件摘要：

```bash
go run ./cmd/mimecrypt list 10
go run ./cmd/mimecrypt list 10 20
go run ./cmd/mimecrypt list 0 5 --folder inbox
```

`list 10` 表示列出半开区间 `[0,10)`，`list 10 20` 表示列出半开区间 `[10,20)`，都按 `receivedDateTime desc` 排序。

加密本地 MIME 文件到 RFC 3156 PGP/MIME：

```bash
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --key 0xDEADBEEF
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --recipient alice@example.com --recipient bob@example.com
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --protect-subject
```

按邮件 ID 执行处理链路：

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

发现邮件并持续处理：

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

调试时直接处理当前文件夹中最新的一封邮件：

```bash
go run ./cmd/mimecrypt run --debug-save-first --save-output --output-dir ./output
```

需要注意的是，`process` 和 `run` 已经接入真实加密与邮箱回写；默认回写到原文件夹，也可以通过 `--write-back-folder` 指定目标文件夹。
当前默认 provider 是 `imap`，默认回写后端也是 `imap`。这样读写两侧都基于 RFC 822 / MIME 和 IMAP `APPEND`，不再依赖 Graph draft 语义或 EWS 生命周期。

## 配置

默认内置了一个 Microsoft Entra 应用 Client ID；如果你要改成自己的应用，可以用环境变量覆盖：

```bash
export MIMECRYPT_CLIENT_ID="你的应用 Client ID"
```

常用配置：

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
export MIMECRYPT_FOLDER="INBOX"
export MIMECRYPT_WRITEBACK_PROVIDER="imap"
export MIMECRYPT_WRITEBACK_FOLDER=""
export MIMECRYPT_GRAPH_SCOPES="https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/User.Read offline_access openid profile"
export MIMECRYPT_IMAP_SCOPES="https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
export MIMECRYPT_IMAP_ADDR="outlook.office365.com:993"
export MIMECRYPT_IMAP_USERNAME="your-mailbox@example.com"
export MIMECRYPT_EWS_SCOPES="https://outlook.office365.com/EWS.AccessAsUser.All"
export MIMECRYPT_EWS_BASE_URL="https://outlook.office365.com/EWS/Exchange.asmx"
```

加密相关配置：

```bash
export MIMECRYPT_PGP_RECIPIENTS="alice@example.com,bob@example.com"
export MIMECRYPT_GPG_BINARY="gpg"
```

说明：

- `MIMECRYPT_PROVIDER` 当前支持 `imap` 和 `graph`；默认 `imap`
- `MIMECRYPT_CLIENT_ID` 默认使用项目内置的应用 ID，也可以显式覆盖成你自己的应用注册
- `MIMECRYPT_STATE_DIR` 用来保存 token 和同步状态
- `MIMECRYPT_OUTPUT_DIR` 仅在开启 `MIMECRYPT_SAVE_OUTPUT=true` 或 `--save-output` 时用于保存本地 `PGP/MIME .eml`
- `MIMECRYPT_SAVE_OUTPUT` 控制是否将加密后的 `PGP/MIME .eml` 额外落盘，默认 `false`
- `MIMECRYPT_PROTECT_SUBJECT` 控制是否将外层 `Subject` 改写为 `...`；开启后行为更接近 Thunderbird，解密后的原始主题仍保持不变
- `MIMECRYPT_BACKUP_DIR` 保存对原始 MIME 源字节直接执行 `gpg --armor --encrypt` 后得到的密文备份
- `MIMECRYPT_BACKUP_KEY_ID` 为备份指定 catch-all GPG key id；设置后所有备份都使用这把 key，而不是邮件收件人 key
- `MIMECRYPT_AUDIT_LOG_PATH` 保存关键流程的追加式 JSONL 审计日志
- `MIMECRYPT_WRITEBACK_PROVIDER` 控制回写后端；当前支持 `imap`、`graph` 和 `ews`，默认 `imap`
- `MIMECRYPT_WRITEBACK_FOLDER` 为空时默认回写到原文件夹；Graph 使用 folder id，IMAP 使用 mailbox 名称
- `MIMECRYPT_IMAP_SCOPES` 为 IMAP OAuth 申请 scope；默认 `https://outlook.office.com/IMAP.AccessAsUser.All offline_access`
- `MIMECRYPT_IMAP_ADDR` 为 IMAP 服务地址；默认 `outlook.office365.com:993`
- `MIMECRYPT_IMAP_USERNAME` 为 IMAP 登录用户名，一般就是邮箱地址；使用 `imap` provider 时必需
- `MIMECRYPT_EWS_SCOPES` 为 EWS 回写申请 OAuth scope；默认使用 `https://outlook.office365.com/EWS.AccessAsUser.All`
- `MIMECRYPT_EWS_BASE_URL` 为 EWS SOAP 端点；默认 `https://outlook.office365.com/EWS/Exchange.asmx`
- `MIMECRYPT_PGP_RECIPIENTS` 用于补充/覆盖收件人邮箱列表；如果邮件头缺少 `To/Cc/Bcc`，该变量是必需的
- 未加密邮件会调用本地 `gpg` 生成 `PGP/MIME (RFC 3156)`；请确保对应收件人的公钥已导入 keyring
- `encrypt` 命令默认会从输入 MIME 的 `To/Cc/Bcc` 推断收件人并匹配同邮箱公钥；`--recipient` 只接受邮箱地址，`--key` 用于显式指定 GPG key（指纹、key id 或 user id）
- 显式传入的 `--recipient` / `--key` 值会拒绝以 `-` 开头或包含控制字符的输入，避免污染 GPG 参数语义
- 加密时进入 `gpg` 的明文载荷始终是原始 MIME 字节；解密后应恢复出与输入一致的原始邮件内容（包括头与正文）
- `process` / `run` 默认只产出 `backup/*.pgp` 密文备份，不额外本地落盘 `.eml`
- 开启 `--save-output` 或 `MIMECRYPT_SAVE_OUTPUT=true` 后，才会额外产出 `output/*.eml`，供 Thunderbird 等客户端直接打开
- `backup/*.pgp` 始终针对原始 MIME 源字节加密；如果设置了 `--backup-key-id` 或 `MIMECRYPT_BACKUP_KEY_ID`，则统一使用该 catch-all key 生成备份
- 加密输出的外层包装会增加 `X-MimeCrypt-Processed: yes`，用于标记该邮件经过 MimeCrypt 处理；该头不会进入解密后的原始 MIME
- 使用 `imap` provider 或 `imap` 回写后端时，需要 `https://outlook.office.com/IMAP.AccessAsUser.All`
- 使用 `graph` provider 读信时，仍然需要 `Mail.ReadWrite`
- 如果你之前是在旧版本上登录过，需要重新执行 `logout` 和 `login`，以获取包含 IMAP scope 的新 token
- `graph` 写回后端仍保留作为可选项，但 Outlook Web 会把导入结果显示为 draft；如果要求“真实收件邮件”语义，应优先使用默认的 `imap`

## 回写行为

MimeCrypt 当前保留两类回写/上传行为：

1. 标准邮箱协议回写
   - 当前实现：`imap`
   - 底层方式：`IMAP APPEND`
   - 结果语义：更接近“邮箱里新增一封真实邮件”，不会走 Graph draft 语义
   - 主要副作用：
     - 命令里的 `message-id` 在 IMAP 下是 `UID`
     - 依赖 `MIMECRYPT_IMAP_USERNAME`
     - 文件夹标识在 IMAP 下使用 mailbox 名称，不是 Graph folder id
     - 某些客户端对 PGP/MIME 的 UI 展示仍可能不一致，但不会因为 Graph draft 机制被显示成草稿

2. API 回写
   - 当前实现：`graph` 和 `ews`
   - 结果语义：通过服务端 API 导入邮件，而不是标准邮箱协议追加
   - 主要副作用：
     - `graph` 写回在 Outlook Web 上可能显示为 draft，这是 Graph 自身语义限制
     - `ews` 可以更接近真实收件语义，但 Exchange Online 已进入退役周期，不适合作为长期主路径
     - API 路径通常依赖 provider 自己的消息/文件夹 ID 体系，兼容性和客户端展示行为更依赖服务端实现

如果你启用了 `--protect-subject` 或 `MIMECRYPT_PROTECT_SUBJECT=true`，不论走哪种回写方式，外层 `Subject` 都会写成 `...`；解密后的原始主题保持不变。Thunderbird 这类支持 OpenPGP/MIME 的客户端解密后可以显示真实标题，Outlook 这类主要看外层头的客户端通常会显示 `...`。

## 回写实现说明

当前默认收发链路是：

1. 用 IMAP OAuth 发现和读取原始 MIME
2. 在本地生成加密后的 `PGP/MIME`
3. 用 IMAP `APPEND` 把 MIME 写回目标文件夹
4. 通过 `UID SEARCH/FETCH` 做校验和幂等对账
5. 最后再删除原邮件

这样做的原因很直接：

- IMAP 原生处理 RFC 822 / MIME，不需要把邮件转换成 Graph 的 draft 对象
- IMAP `APPEND` 没有 `isDraft` 语义包袱，写进去就是普通邮件
- 在 Microsoft 365 上，IMAP OAuth 属于当前官方持续支持的长期协议路径

需要明确的边界：

- IMAP provider 当前把消息 UID 当作命令行里的 `message-id`
- 对 `process/download` 这类单封邮件命令，若只给 UID，则默认从 `--folder` 或 `MIMECRYPT_FOLDER` 指定的 mailbox 中读取
- `graph` provider 仍然保留，适合继续验证 Graph delta、Graph metadata 和其它 provider 兼容路径
- `ews` 写回仍然保留为兼容选项，但已不再是默认主路径

## 文件说明

- `graph-token.json`：当前 provider 的 token 缓存
- `sync-<folder>.json`：文件夹增量同步状态
- `audit.jsonl`：关键流程审计日志，按 JSONL 逐行追加
- `output/*.eml`：仅在开启 `save-output` 时生成的 PGP/MIME 邮件文件
- `backup/*.pgp`：对原始 MIME 源字节直接加密后的本地备份

从长期目标看，磁盘落盘应尽量只在调试、审计或故障恢复场景下使用，而不是默认主路径。

## 路线图

接下来更符合项目定位的开发顺序是：

1. 增加 `google` provider
2. 增加 webhook 接收与事件路由
3. 减少默认明文落盘，让“拉取后尽快加密并回写”成为主路径
4. 增加密钥管理与收件人路由策略（按域名、文件夹或规则选择 key）

## 当前结论

MimeCrypt 的核心目标已经明确：

- 自动完成邮件加密
- 兼容 Microsoft 与 Google 邮件生态
- 支持事件驱动和主动拉取两种发现方式
- 将邮件在明文状态下的暴露路径压缩到尽可能短

现阶段代码已经完成了模块边界和 provider 抽象，后续重点应当放在真实加密、回写校验和 webhook 接入上。
