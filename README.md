# MimeCrypt

MimeCrypt 是一个面向服务器场景的自动化邮件加密系统。它以原始 MIME 邮件为处理对象，从命名来源读取邮件，执行 PGP/MIME 加密、审计和可选备份，再按照拓扑路由投递到一个或多个出口。

项目的运行模型以 topology 为中心，配置对象包括 `credential`、`source`、`sink` 和 `route`。CLI 负责触发命令；认证装配和邮件流装配分别落在独立运行时层；核心处理链路围绕单封邮件事务展开。

## 核心能力

- 从命名 `source` 读取原始 MIME 邮件
- 识别已加密邮件，避免重复处理
- 使用本地 `gpg` 生成 RFC 3156 `multipart/encrypted`
- 把处理结果投递到一个或多个命名 `sink`
- 记录 JSONL 审计日志并生成可选密文备份
- 在满足 route 策略时执行源端删除
- 支持持续轮询和单封处理两种运行方式
- 支持基于 topology 的多凭据、多来源、多出口装配

## 运行模型

MimeCrypt 以邮件为事务边界，主链路分为三层：

1. `Producer`
   从 `source` 拉取邮件，生成统一的 `MailEnvelope`。
2. `Processor`
   读取 MIME、判断是否已加密、执行加密、备份和审计，并生成执行计划。
3. `Consumer`
   按 route 把产物写入一个或多个 `sink`，返回可持久化的投递回执。

`internal/mailflow.Coordinator` 负责推进事务状态、落盘回执、执行对账，并在 route 的 `delete_source` 明确启用且条件满足时删除源邮件。

删除策略是协调层行为，不是 writer 的隐式副作用。

## 配置对象

### `credential`

认证与秘密材料。一个 credential 可以覆盖：

- `state_dir`
- `client_id`
- `tenant`
- `authority_base_url`
- `token_store`
- `keyring_service`
- `graph_scopes`
- `ews_scopes`
- `imap_scopes`
- `imap_username`

如果命名 credential 没有显式提供 `state_dir`，默认落在 `<state_dir>/credentials/<credential-name>`。

### `source`

邮件入口。主要字段包括：

- `driver`
- `credential_ref`
- `mode`
- `folder`
- `state_path`
- `include_existing`
- `poll_interval`
- `cycle_timeout`

当前 `run` 只支持 `mode = "poll"` 的 source。

### `sink`

邮件出口。主要字段包括：

- `driver`
- `credential_ref`
- `folder`
- `output_dir`
- `verify`

如果 sink 没有显式声明 `folder`，运行时会继承当前 source 的 folder 作为该 sink 的默认邮箱上下文。

### `route`

路由和投递策略。主要字段包括：

- `source_refs`
- `targets`
- `state_dir`
- `delete_source`

一个 route 可以绑定多个 source，也可以把一封邮件投递到多个 sink。

## 当前驱动

当前内置驱动如下：

- `source`: `imap`、`graph`
- `sink`: `file`、`discard`、`imap`、`graph`、`ews`

说明：

- `imap` sink 使用 IMAP `APPEND` 写入目标文件夹，并支持基于 `InternetMessageID` 的幂等对账。
- `graph` 和 `ews` sink 通过服务端 API 导入 MIME。
- `imap` source 删除语义是 hard delete。
- `graph` / `ews` 的删除语义属于 soft delete，因此不会被当作安全删除来源。

## 代码结构

- `cmd/mimecrypt`
  程序入口与信号感知退出。
- `internal/cli`
  CLI 命令定义、参数解析和终端输出。
- `internal/appruntime`
  认证相关运行时装配，包括 `login`、`logout`、`token`。
- `internal/flowruntime`
  topology 解析、运行计划和 mailflow 装配。
- `internal/mailflow`
  邮件级事务、状态存储、协调器和执行结果模型。
- `internal/mailflow/adapters`
  基于 provider 和模块服务的 producer / processor / consumer 适配层。
- `internal/modules/*`
  加密、下载、列表、回写、健康检查、备份、审计等模块。
- `internal/provider`
  统一接口契约。
- `internal/providers/*`
  各协议驱动和 provider 装配。
- `internal/auth`
  OAuth 会话、token 存储与刷新。
- `internal/appconfig`
  环境变量、topology、本地配置和状态布局。

## 命令

### 登录与凭据

```bash
go run ./cmd/mimecrypt login
go run ./cmd/mimecrypt login mailbox@example.com
go run ./cmd/mimecrypt login --topology-file ./topology.json --credential office-auth

go run ./cmd/mimecrypt token status
go run ./cmd/mimecrypt token import ./token.json
cat ./token.json | go run ./cmd/mimecrypt token import -
go run ./cmd/mimecrypt token status --topology-file ./topology.json --credential office-auth

go run ./cmd/mimecrypt logout
go run ./cmd/mimecrypt logout --topology-file ./topology.json --credential office-auth
```

`login [imap-username]` 用于在当前 credential 上显式指定 IMAP 用户名。若同时设置了 `MIMECRYPT_IMAP_USERNAME`，环境变量优先。

### 读取与检查

```bash
go run ./cmd/mimecrypt list 10 --topology-file ./topology.json --source office-inbox
go run ./cmd/mimecrypt list 10 20 --topology-file ./topology.json --source office-inbox

go run ./cmd/mimecrypt download <message-id> --topology-file ./topology.json --source office-inbox --output-dir ./output

go run ./cmd/mimecrypt health --topology-file ./topology.json --route archive
go run ./cmd/mimecrypt health --topology-file ./topology.json --route archive --deep
go run ./cmd/mimecrypt health --topology-file ./topology.json --route archive --source office-inbox --timeout 20s
```

`list` 的范围是半开区间 `[start,end)`；`list 10` 等价于 `[0,10)`。

### 单封处理与持续运行

```bash
go run ./cmd/mimecrypt process <message-id> --topology-file ./topology.json --route archive --source office-inbox
go run ./cmd/mimecrypt process <message-id> --topology-file ./topology.json --route archive --source office-inbox --transaction-mode persistent

go run ./cmd/mimecrypt run --topology-file ./topology.json --route archive --once
go run ./cmd/mimecrypt run --topology-file ./topology.json --route archive
go run ./cmd/mimecrypt run --topology-file ./topology.json --route archive --source office-inbox
go run ./cmd/mimecrypt run --topology-file ./topology.json --source office-inbox --debug-save-first
```

说明：

- `run` 会解析 route 中的全部 source；如果传入 `--source`，则只运行指定 source。
- `process` 以单封邮件为单位执行同一套 mailflow 协调逻辑。
- `process` 默认使用 `ephemeral` 事务状态；设置 `--transaction-mode persistent` 后，会复用 route 的持久化事务目录。
- `--debug-save-first` 会处理当前 source 文件夹中最新的一封邮件并退出。

### 本地 MIME 加密

```bash
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --recipient alice@example.com --recipient bob@example.com
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --key 0xDEADBEEF
go run ./cmd/mimecrypt encrypt ./plain.eml ./encrypted.eml --protect-subject
```

## Topology 示例

```json
{
  "default_credential": "office-auth",
  "default_source": "office-inbox",
  "default_route": "archive",
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
    "archive-file": {
      "driver": "file",
      "output_dir": "./output"
    },
    "encrypted-mailbox": {
      "driver": "imap",
      "credential_ref": "office-auth",
      "folder": "Encrypted",
      "verify": true
    }
  },
  "routes": {
    "archive": {
      "source_refs": ["office-inbox"],
      "targets": [
        {
          "name": "primary-file",
          "sink_ref": "archive-file",
          "artifact": "primary",
          "required": true
        },
        {
          "name": "mailbox-copy",
          "sink_ref": "encrypted-mailbox",
          "artifact": "primary",
          "required": false
        }
      ],
      "delete_source": {
        "enabled": true,
        "require_same_store": true,
        "eligible_sinks": ["encrypted-mailbox"]
      }
    }
  }
}
```

Topology JSON 按严格模式解析。未知字段、拼写错误和多余 JSON 内容都会直接报错。

## 环境变量

环境变量提供安装级默认值；source、sink、route 和 credential 的运行时选择来自 topology。

### 路径与状态

```bash
export MIMECRYPT_TOPOLOGY_PATH="$HOME/.config/mimecrypt/topology.json"
export MIMECRYPT_STATE_DIR="$HOME/.config/mimecrypt"
export MIMECRYPT_OUTPUT_DIR="./output"
export MIMECRYPT_WORK_DIR=""
export MIMECRYPT_BACKUP_DIR="./backup"
export MIMECRYPT_AUDIT_LOG_PATH="$HOME/.config/mimecrypt/audit.jsonl"
export MIMECRYPT_AUDIT_STDOUT="false"
```

### OAuth 与协议端点

```bash
export MIMECRYPT_CLIENT_ID="your-client-id"
export MIMECRYPT_TENANT="organizations"
export MIMECRYPT_AUTHORITY_BASE_URL="https://login.microsoftonline.com"
export MIMECRYPT_GRAPH_BASE_URL="https://graph.microsoft.com/v1.0"
export MIMECRYPT_EWS_BASE_URL="https://outlook.office365.com/EWS/Exchange.asmx"
export MIMECRYPT_IMAP_ADDR="outlook.office365.com:993"
export MIMECRYPT_IMAP_USERNAME="mailbox@example.com"
```

### Scopes 与 token 存储

```bash
export MIMECRYPT_GRAPH_SCOPES="https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/User.Read offline_access openid profile"
export MIMECRYPT_EWS_SCOPES="https://outlook.office365.com/EWS.AccessAsUser.All"
export MIMECRYPT_IMAP_SCOPES="https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
export MIMECRYPT_TOKEN_STORE="file"
export MIMECRYPT_KEYRING_SERVICE="mimecrypt"
```

### 加密

```bash
export MIMECRYPT_PGP_RECIPIENTS="alice@example.com,bob@example.com"
export MIMECRYPT_GPG_BINARY="gpg"
export MIMECRYPT_GPG_HOME="$HOME/.config/mimecrypt/gnupg"
export MIMECRYPT_GPG_TRUST_MODEL="auto"
export MIMECRYPT_PROTECT_SUBJECT="false"
export MIMECRYPT_BACKUP_KEY_ID=""
```

## 状态与安全

- token 默认存储在 `state_dir/token.json`；当 `MIMECRYPT_TOKEN_STORE=keyring` 时，主存储切换到系统 keyring。
- token 文件写入走原子落盘；同一 credential 的 token 刷新在进程内和同机多进程间都会串行化。
- source 状态、事务状态和运行锁按 `route + source + driver + folder` 作用域隔离。
- 临时工作目录用于生成加密产物、备份和审计相关中间件；处理结束后会清理。
- 审计日志默认记录事务和投递信息，不记录邮件正文。
- `graph` / `ews` 的删除语义是 soft delete；只有声明为 hard delete 的来源才会参与安全删除。

状态目录中常见的文件包括：

- `token.json`
- `config.json`
- `flow-sync-<scope>.json`
- `flow-state/<scope>/`
- `audit.jsonl`
- `output/*.eml`
- `backup/*.pgp`

## 开发

```bash
go build -o mimecrypt ./cmd/mimecrypt
go test ./...
go test ./... -cover
```

常见本地流程：

```bash
export MIMECRYPT_TOPOLOGY_PATH="$HOME/.config/mimecrypt/topology.json"
go run ./cmd/mimecrypt login --credential office-auth
go run ./cmd/mimecrypt health --route archive --deep
go run ./cmd/mimecrypt run --route archive --once
```
