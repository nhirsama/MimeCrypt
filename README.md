# MimeCrypt

MimeCrypt 是一个面向服务器场景的自动化邮件加密工具。

它的目标不是单纯“抓取邮件”，而是尽可能自动地完成以下链路：

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

业务模块保持为 7 个：

- `internal/modules/login`：登录并验证当前账号
- `internal/modules/logout`：清除本地登录状态
- `internal/modules/download`：按邮件 ID 下载原始 MIME
- `internal/modules/writeback`：回写邮件并校验
- `internal/modules/process`：按邮件 ID 和配置处理邮件
- `internal/modules/encrypt`：加密邮件
- `internal/modules/discover`：发现邮件并进行路由处理

底层支撑层负责协议与实现解耦：

- `internal/provider`：统一的认证、收件、回写接口契约
- `internal/providers`：按配置选择 provider 的工厂
- `internal/providers/graph`：当前 Microsoft Graph provider 实现
- `internal/auth`：Graph 登录、refresh token 刷新、token 缓存
- `internal/mail`：当前 Graph 的收件读取实现
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
- Microsoft Graph 邮件 MIME 拉取
- 统一 provider 抽象
- 按邮件 ID 下载邮件
- 调试模式处理第一封邮件
- 增量同步发现邮件的基础框架
- 已加密邮件识别与拒绝重复加密（PGP/S-MIME）
- 基于 `gpg` 的 PGP/MIME（RFC 3156）加密封装

当前还没有完成的部分：

- 邮件回写与校验
- Google Gmail API provider
- webhook 接收入口
- 可配置的邮件路由规则

所以现阶段它仍然是一个“围绕自动加密目标设计的原型”，而不是完整可投产的最终版本。

## 当前可用 Provider

当前内置 provider：

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
```

按邮件 ID 执行处理链路：

```bash
go run ./cmd/mimecrypt process <message-id> --output-dir ./output
```

发现邮件并持续处理：

```bash
go run ./cmd/mimecrypt run --once
go run ./cmd/mimecrypt run --poll-interval 1m --output-dir ./output
```

调试时直接处理当前文件夹中最新的一封邮件：

```bash
go run ./cmd/mimecrypt run --debug-save-first --output-dir ./output
```

需要注意的是，`process` 和 `run` 已经接入真实加密；但回写与回写校验逻辑仍在后续开发中。

## 配置

当前最少需要：

```bash
export MIMECRYPT_CLIENT_ID="你的应用 Client ID"
```

常用配置：

```bash
export MIMECRYPT_PROVIDER="graph"
export MIMECRYPT_TENANT="organizations"
export MIMECRYPT_STATE_DIR="$HOME/.config/mimecrypt"
export MIMECRYPT_OUTPUT_DIR="./output"
export MIMECRYPT_FOLDER="inbox"
export MIMECRYPT_GRAPH_SCOPES="https://graph.microsoft.com/Mail.Read https://graph.microsoft.com/User.Read offline_access openid profile"
```

加密相关配置：

```bash
export MIMECRYPT_PGP_RECIPIENTS="alice@example.com,bob@example.com"
export MIMECRYPT_GPG_BINARY="gpg"
```

说明：

- `MIMECRYPT_PROVIDER` 当前只支持 `graph`
- `MIMECRYPT_STATE_DIR` 用来保存 token 和同步状态
- `MIMECRYPT_OUTPUT_DIR` 当前主要用于调试和开发阶段保存 `.eml`
- `MIMECRYPT_PGP_RECIPIENTS` 用于补充/覆盖收件人；如果邮件头缺少 `To/Cc/Bcc`，该变量是必需的
- 未加密邮件会调用本地 `gpg` 生成 `PGP/MIME (RFC 3156)`；请确保对应收件人的公钥已导入 keyring

## 文件说明

- `graph-token.json`：当前 provider 的 token 缓存
- `sync-<folder>.json`：文件夹增量同步状态
- `output/*.eml`：当前调试或处理中落盘的 MIME 文件

从长期目标看，磁盘落盘应尽量只在调试、审计或故障恢复场景下使用，而不是默认主路径。

## 路线图

接下来更符合项目定位的开发顺序是：

1. 在 `writeback` 模块中接入邮件回写和校验
2. 增加 `google` provider
3. 增加 webhook 接收与事件路由
4. 减少默认明文落盘，让“拉取后尽快加密并回写”成为主路径
5. 增加密钥管理与收件人路由策略（按域名、文件夹或规则选择 key）

## 当前结论

MimeCrypt 的核心目标已经明确：

- 自动完成邮件加密
- 兼容 Microsoft 与 Google 邮件生态
- 支持事件驱动和主动拉取两种发现方式
- 将邮件在明文状态下的暴露路径压缩到尽可能短

现阶段代码已经完成了模块边界和 provider 抽象，后续重点应当放在真实加密、回写校验和 webhook 接入上。
