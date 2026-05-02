# Remork

Remork 让你可以在本地编辑一个远端项目目录。它会在远端服务器上运行一个很小的
daemon，把本机的一个目录绑定到远端的一个项目目录；你先把文件同步下来，在本地
编辑，然后明确执行 `remork apply` 才会把修改安全写回远端。

Remork V1 面向可信 VPN 或内网环境。它支持可选的共享 bearer token，但这不是账号
系统，也不应该直接暴露在公网。

## 先理解几个概念

- **Remork host**：本机给 daemon endpoint 起的昵称，例如
  `z00879328_docker_2.7`。
- **SSH target**：Remork 安装或升级 daemon 时使用的 SSH 目标。它可以和 Remork
  host 同名，但含义不同。
- **Daemon URL**：安装完成后 CLI 访问 daemon 的 HTTP 地址，例如
  `http://175.100.2.7:17731`。这不是 SSH，也不是 SSH 端口。
- **Allowed root**：`remorkd` 在服务端声明的安全边界。daemon 只允许访问这些目录
  下面的 workspace。
- **Workspace root**：真正绑定到本地 working copy 的远端项目目录。
- **Local working copy**：你在本机实际编辑的目录。

下面的 quickstart 里，`ALLOWED_ROOT` 是 allowed root，`WORKSPACE_ROOT` 是
workspace root。

## 开始前先确认

你需要：

- 本机是 macOS，并且可以使用 `curl`；
- 本机能通过 SSH 登录远端服务器；
- 本机能访问远端服务器的 VPN/内网 IP 或 DNS 名字，端口是 `17731`；
- 远端是 Linux。ARM 服务器通常用 `linux-arm64`，x86_64 服务器通常用
  `linux-amd64`；
- 远端用户可以写入 `$HOME/.local/bin` 和 `$HOME/.remork`；
- 远端 workspace 目录已经存在，并且位于 allowed root 下面。

先填好这些值：

```bash
HOST_ALIAS=my-lab
SSH_TARGET=my-lab
DAEMON_URL=http://10.0.0.12:17731
ALLOWED_ROOT=/home/me
WORKSPACE_ROOT=/home/me/project
LOCAL_WORKING_COPY=~/remork/project
REMOTE_PLATFORM=linux-arm64
CLIENT_PLATFORM=darwin-arm64
```

这些值分别表示：

```text
HOST_ALIAS          本机 Remork 使用的昵称
SSH_TARGET          只用于安装或升级 daemon 的 SSH 目标
DAEMON_URL          安装完成后本机访问 daemon 的 HTTP 地址
ALLOWED_ROOT        remorkd 在远端允许访问的安全边界
WORKSPACE_ROOT      你真正想编辑的远端项目目录
LOCAL_WORKING_COPY  你在本机实际编辑的目录
REMOTE_PLATFORM     linux-arm64 或 linux-amd64
CLIENT_PLATFORM     Apple Silicon 用 darwin-arm64，Intel Mac 用 darwin-amd64
```

你也可以自动设置 `CLIENT_PLATFORM`：

```bash
case "$(uname -m)" in
  arm64) CLIENT_PLATFORM=darwin-arm64 ;;
  x86_64) CLIENT_PLATFORM=darwin-amd64 ;;
  *) echo "unsupported local macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac
```

安装前先检查远端：

```bash
ssh "$SSH_TARGET" 'uname -m; pwd'
ssh "$SSH_TARGET" "test -d '$WORKSPACE_ROOT'"
```

## 五分钟上手

先在本机安装 macOS client：

```bash
VERSION=v0.1.0
mkdir -p "$HOME/.local/bin"
curl -L -o "$HOME/.local/bin/remork" \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-${CLIENT_PLATFORM}"
chmod 0755 "$HOME/.local/bin/remork"
export PATH="$HOME/.local/bin:$PATH"
```

如果新打开的终端仍然找不到 `remork`，把下面这一行加入 `~/.zshrc`：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

通过本机 client 走 SSH 安装远端 daemon：

```bash
remork daemon install "$HOST_ALIAS" \
  --ssh "$SSH_TARGET" \
  --url "$DAEMON_URL" \
  --root "$ALLOWED_ROOT" \
  --platform "$REMOTE_PLATFORM" \
  --execute --yes \
  --verify \
  --no-proxy
```

如果同一个 daemon 需要管理多个互相独立的基础目录，可以重复写 `--root`，例如
`--root /home/me --root /scratch/me`。每个 workspace 都必须位于这些 advertised
allowed roots 之一下面。

这条命令会把匹配的 `remorkd` 二进制拷贝到远端的持久路径，例如
`$HOME/.local/bin` 和 `$HOME/.remork` 下，启动 daemon，写入本机 Remork host 配置，
并验证 daemon 状态。如果这是共享 VPN 或多人网络，建议给 daemon 加 `--token-file`，
并在本地 host 配置里使用 `--token-env`，不要使用无认证 daemon。

Daemon URL 里的 IP 应该是本机能访问的 VPN/内网 IP 或 DNS 名字，端口是 `17731`。
它不是 SSH 端口。如果不确定，优先使用服务器的 VPN/内网地址，然后用下面命令确认：

```bash
remork daemon status "$HOST_ALIAS"
```

如果你给 daemon URL 换了端口，安装时也要传同一个端口，例如
`--url http://HOST:18131` 要搭配 `--addr 0.0.0.0:18131`。

创建本地 working copy，并绑定到远端 workspace root：

```bash
mkdir -p "$LOCAL_WORKING_COPY"
cd "$LOCAL_WORKING_COPY"
remork init "$HOST_ALIAS:$WORKSPACE_ROOT"
remork sync
remork status
remork doctor
```

之后你可以像普通本地目录一样编辑文件。编辑完成后，先看 diff，再明确应用到远端：

```bash
remork diff
remork apply
```

## 日常命令

### `remork sync`

把远端文件同步到本地 working copy，并记录一份用于冲突检查的 base state。默认情况
下，大文件不会直接下载，而是生成 `.meta` 占位文件。

### `remork status`

查看当前本地目录是否干净、是否有本地修改、是否有远端更新、是否存在冲突、是否有
大文件占位。

### `remork diff`

查看本地修改和同步 base 之间的差异。

### `remork apply`

把本地修改写回远端 workspace。`apply` 会带上同步时记录的 base hash。如果远端文件
在你上次同步后已经变了，daemon 会拒绝覆盖并返回冲突。

### `remork run -- COMMAND ...`

在远端 workspace 中运行一条非交互式命令。

### `remork shell`

通过 daemon 打开一个交互式远端 shell。

## 常见流程

添加或查看 daemon endpoint 昵称：

```bash
remork host list
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --no-proxy
remork host remove HOST
```

使用共享 token，但不把密钥直接写进配置：

```bash
export REMORK_TOKEN='replace-with-real-token'
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --token-env REMORK_TOKEN
```

绑定同一个 allowed root 下面的另一个 child workspace：

```bash
mkdir -p ~/remork/MY_PROJECT
cd ~/remork/MY_PROJECT
remork init HOST:/absolute/remote/workspace
remork sync
remork workspace
```

编辑后运行远端检查：

```bash
remork status
remork diff
remork apply
remork run -- make test
```

## 大文件

超过 daemon 阈值的文件默认不会被下载。Remork Product V1 默认阈值是 `128MB`，除非
启动 daemon 时另外指定。

如果远端 workspace 中有：

```text
checkpoints/model.tar.gz
```

本地 working copy 会收到：

```text
checkpoints/model.tar.gz.meta
```

这个 `.meta` 文件会记录远端路径、文件大小、版本信息和 pull 命令。只有当你确实需
要完整内容时，再执行：

```bash
remork pull checkpoints/model.tar.gz
```

## 安全地 apply

Remork 把远端 workspace 当作事实来源。你可以在本地编辑文件，但本地修改不会自动
推送到远端。

`remork apply` 会发送一个 changeset，里面带着 `sync` 或 `pull` 时记录的 base hash。
如果远端文件已经不再匹配这个 base hash，daemon 会返回冲突，并且不会继续覆盖该文
件。

这能避免几类常见问题：

- 另一个人同时修改了同一个远端文件；
- 远端命令生成或改写了文件；
- Agent 拿着过期的本地副本去覆盖远端。

建议在 apply 前先运行：

```bash
remork status
remork diff
```

### 冲突恢复

当本地修改和远端更新碰到同一个文件时，先看详细状态：

```bash
remork status --verbose
```

对每个冲突文件，可以查看引导式恢复步骤：

```bash
remork conflict -- path/to/file
```

如果要丢弃这个文件上的本地修改，可以恢复到本地 base cache。注意，这并不是直接接
受当前远端更新：

```bash
remork restore -- path/to/file
remork status
remork sync
```

之后继续检查差异，确认后再 apply。

普通 `remork apply` 不会自动把新的本地文件创建到远端。要创建单个新文件，用
`remork apply path/to/new-file`；要应用所有未被 ignore 的 untracked 文件，用
`remork apply --include-untracked`。

Remork 会先读 `.remorkignore`，再读 `.gitignore`。本地缓存、密钥、生成产物、虚拟
环境、agent 临时文件这类不应该写回远端的内容，建议放进 `.remorkignore`。

## 远端命令和 shell

非交互式命令用 `run`：

```bash
remork run -- pwd
remork run -- make test
remork run -- python scripts/check.py
```

需要交互时用 `shell`：

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

Shell session 在本地 client 断开后仍会由 daemon 保留。可以用 `--list` 找到保留的
session，用 `--attach` 重新连接，用 `--kill` 停掉。

## 调试和维护

### `remork doctor`

检查本地配置、当前 workspace 绑定、token 设置、daemon 可达性、allowed root 覆盖、
manifest 访问、操作日志访问等。

### `remork daemon status HOST`

访问 `HOST` 配置的 daemon URL，打印 daemon 版本、平台、allowed roots、大文件阈
值、watch 支持情况和本地认证状态。

### `remork daemon install HOST --root /allowed/root [--root /another/root]`

通过 SSH 安装或启动 `remorkd`。常用参数包括：`--ssh` 选择 SSH target，`--url` 写
入本机 daemon URL，`--platform` 选择 daemon 二进制，`--execute --yes` 直接执行安
装命令，`--verify` 安装后验证 daemon。如果一个 daemon 需要暴露多个 allowed base
roots，可以重复传 `--root`。

### `remork daemon upgrade HOST`

替换远端 daemon 二进制。加上 `--execute --yes` 后会直接执行生成的命令。

### `remork debug manifest`、`remork debug events`、`remork debug api`

当 sync 或连接行为不清楚时，用这些命令检查 daemon 数据和传输层行为。

## Release 下载和高级兜底部署

GitHub Release 发布裸二进制文件：

```text
remork-darwin-arm64     # macOS 客户端，Apple Silicon
remork-darwin-amd64     # macOS 客户端，Intel
remorkd-linux-arm64     # Linux 服务端 daemon，arm64
remorkd-linux-amd64     # Linux 服务端 daemon，amd64
```

大多数用户只需要把本机 client 安装到 `~/.local/bin/remork`，然后用
`remork daemon install` 把 daemon 部署到远端持久路径。

如果你需要在本地自行构建 release：

```bash
scripts/build-release.sh v0.1.0
```

高级兜底：如果 client-driven install 不可用，可以手动拷贝 release daemon 并启动。
下面例子只使用明确的占位符：

```bash
scp dist/remorkd-linux-arm64 SSH_TARGET:~/.local/bin/remorkd
ssh SSH_TARGET 'chmod 0755 ~/.local/bin/remorkd'
ssh SSH_TARGET 'mkdir -p ~/.remork/run ~/.remork/log'
ssh SSH_TARGET 'nohup ~/.local/bin/remorkd --root /allowed/root --addr 0.0.0.0:17731 </dev/null >~/.remork/log/remorkd.log 2>&1 & echo $! >~/.remork/run/remorkd.pid'
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --no-proxy
remork daemon status HOST
```

如果要手动暴露多个 allowed base roots，也是在 `remorkd` 命令里重复写 `--root`。

这里 SSH target 只用于安装辅助，也就是复制和启动 `remorkd`。Remork 正常运行时的
传输是到 daemon URL 的 HTTP 或 WebSocket。

## 安全模型和限制

Remork Product V1 假设：

- 使用可信 VPN 或内网；
- 可以通过环境变量或 token file 使用可选的共享 token；
- `remorkd` 启动时明确指定 allowed roots；
- 本地修改不会自动写回远端；
- daemon 部署过程中不要求远端安装依赖或访问互联网。

当前限制：

- 没有账号、RBAC 或多租户隔离；
- 没有公网加固；
- Product V1 中 daemon 配置主要通过启动参数完成；
- 本地配置是 `~/.remork` 下的 JSON。

## 开发者说明

普通用户应该优先使用 CLI。Daemon API 细节和请求格式在 `docs/remork-api.md`。

Agent 使用 Remork 的操作指南在 `skills/remork/SKILL.md`。
