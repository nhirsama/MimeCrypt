# MimeCrypt

一个面向远程服务器 CLI 场景的 Go 原型，用来：

- 通过 Microsoft Entra `device code flow` 完成首次授权
- 通过 Microsoft Graph 拉取邮件原始 MIME
- 按邮件 ID 处理邮件，给后续 GPG/PGP-MIME 加密和回写预留清晰模块边界
- 持续发现新邮件并路由到统一处理链路

## 模块结构

业务模块只保留 7 个：

- `internal/modules/login`：登录并验证当前账号
- `internal/modules/logout`：清除本地 token 缓存
- `internal/modules/download`：按邮件 ID 下载原始 MIME
- `internal/modules/writeback`：回写邮件并校验
- `internal/modules/process`：按邮件 ID 和配置处理邮件
- `internal/modules/encrypt`：加密邮件
- `internal/modules/discover`：发现邮件并进行路由处理

底层支撑代码保持轻量：

- `internal/provider`：统一的认证、收件、回写接口契约
- `internal/providers`：按配置选择具体 provider 的工厂
- `internal/auth`：device code 登录、refresh token 刷新、token 本地缓存
- `internal/mail`：当前 Graph provider 使用的邮件读取实现
- `internal/mimefile`：原始 MIME 文件落盘
- `internal/appconfig`：读取环境变量与维护运行配置
- `internal/cli`：基于 `cobra` 的命令树，只负责接线
- `cmd/mimecrypt`：CLI 入口，只负责启动

## 当前能力

- 不再依赖 `client_secret`
- 适合在没有浏览器的远程服务器上运行
- 首次授权后，后续可以自动刷新 token
- 使用 `delta` 查询跟踪指定文件夹里的新邮件
- 首次运行默认只建立同步基线，不会把历史邮件全部处理下来
- 支持调试模式，直接处理当前文件夹最新的一封邮件
- `process` 模块当前链路为：下载 MIME -> 判定是否已加密 -> 保存到本地 -> 可选回写
- 底层收件和回写 API 已抽象为统一 provider 接口，后续可以增量接入 Google

当前只内置了一个 provider：

- `graph`：Microsoft Graph

当前 `encrypt` 模块还没有真正执行 GPG 加密，它只会：

- 识别 `multipart/encrypted` 的 `PGP/MIME`
- 识别 `inline PGP`
- 对已加密邮件直接透传
- 对未加密邮件先原样保存，等待后续接入真实加密逻辑

## 你需要准备什么

### 1. 注册一个 Microsoft Entra 公共客户端应用

在 Microsoft Entra 管理中心里创建应用注册，并记录它的 `Application (client) ID`。

这个应用需要：

- 启用 `Allow public client flows`
- 具备 Microsoft Graph 的委托权限 `Mail.Read` 和 `User.Read`

`offline_access`、`openid`、`profile` 这几个 scope 由程序在登录时一并请求，用于获取 refresh token 和基础身份信息。

### 2. 设置环境变量

最少只需要一个：

```bash
export MIMECRYPT_CLIENT_ID="你的应用 Client ID"
```

可选环境变量：

```bash
export MIMECRYPT_PROVIDER="graph"
export MIMECRYPT_TENANT="organizations"
export MIMECRYPT_STATE_DIR="$HOME/.config/mimecrypt"
export MIMECRYPT_OUTPUT_DIR="./output"
export MIMECRYPT_FOLDER="inbox"
export MIMECRYPT_GRAPH_SCOPES="https://graph.microsoft.com/Mail.Read https://graph.microsoft.com/User.Read offline_access openid profile"
```

`MIMECRYPT_PROVIDER` 当前只支持 `graph`，但模块层已经不再直接依赖 Graph API。

## CLI 命令

登录并缓存 token：

```bash
go run ./cmd/mimecrypt login
```

程序会在终端打印一段 device code 提示。你需要：

1. 在本地浏览器打开 Microsoft 提示的登录地址
2. 输入终端里显示的验证码
3. 登录并同意权限

完成后，程序会把 token 缓存到本地状态目录。

清除本地登录状态：

```bash
go run ./cmd/mimecrypt logout
```

按邮件 ID 下载原始 MIME：

```bash
go run ./cmd/mimecrypt download <message-id> --output-dir ./output
```

按邮件 ID 处理一封邮件：

```bash
go run ./cmd/mimecrypt process <message-id> --output-dir ./output
```

发现邮件并持续路由处理：

只执行一次同步：

```bash
go run ./cmd/mimecrypt run --once
```

持续轮询新邮件：

```bash
go run ./cmd/mimecrypt run --poll-interval 1m --output-dir ./output
```

首次启动如果你希望连历史邮件也一起下载：

```bash
go run ./cmd/mimecrypt run --once --include-existing
```

调试时如果你只想先验证整条处理链路，可以直接处理当前文件夹中最新的一封邮件：

```bash
go run ./cmd/mimecrypt run --debug-save-first --output-dir ./output
```

如果你传入 `--write-back` 或 `--verify-write-back`，命令会走到 `writeback` 模块；当前该模块还只是占位实现，会返回“尚未实现”错误。

## 文件说明

- `graph-token.json`：Graph access token 和 refresh token 缓存
- `sync-<folder>.json`：指定文件夹的增量同步状态
- `output/*.eml`：下载或处理后保存的 MIME 文件

## 现阶段限制

- `encrypt` 模块还没有真正生成 RFC 3156 `multipart/encrypted`
- `writeback` 模块还没有接上 IMAP `APPEND`
- `discover` 模块当前只有一条默认路由，尚未加入可配置规则
- 还没有把 `.eml` 转换成 Thunderbird 可直接解密的最终 `PGP/MIME`

下一步最自然的扩展就是：

1. 在 `encrypt` 模块中接入真实 GPG 加密
2. 生成 RFC 3156 `multipart/encrypted`
3. 在 `writeback` 模块中加入 IMAP OAuth2 `APPEND` 和校验
4. 在 `discover` 模块中加入按配置路由的规则
