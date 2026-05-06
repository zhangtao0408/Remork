<p align="center">
  <img src="docs/assets/header.png" alt="Remork：面向私有服务器的远端 workspace 控制工具" width="100%">
</p>

# Remork

面向私有服务器的远端 workspace 控制工具。

[English README](README.md)

Remork 会为远端服务器上的目录维护一份本地可编辑 working copy。你从远端同步文件，在本机编辑和查看 diff，再通过 `remork apply` 显式写回远端。同一个 daemon 也可以在远端机器上执行命令和打开交互式 shell。

它面向可信 VPN 或私有网络环境，适合不方便在每台服务器上安装完整 Agent 环境的场景。

## 为什么用 Remork

适合使用 Remork 的场景：

- 远端服务器不方便安装或持续更新完整 Agent 运行环境；
- 人和 Agent 都需要查看、编辑远端 workspace 文件；
- 大模型权重、数据包、构建产物等大文件应默认保留在远端；
- 写回远端前需要先 review diff，并且必须显式 apply；
- 命令需要在服务器上运行，但编辑体验希望留在本机。

Remork 不是公网多租户远程执行平台。Product V1 支持 allowed roots 和可选共享 token 认证，但不提供账号系统、RBAC 或公网加固。

## 你会得到什么

- `remork sync` 将远端文件同步到本地 working copy。
- `remork diff` 和 `remork apply` 用于 review 并写回本地修改。
- `remork run -- COMMAND` 在远端执行非交互式命令。
- `remork shell` 通过 daemon 打开交互式远端 shell。
- 大文件默认以 `.meta` 占位符表示，需要时再显式 pull。
- `remork daemon install` 可通过 SSH 复制预构建 daemon。远端不需要 Go、npm、apt、brew，也不需要能访问互联网。
- 在真实终端中直接运行 `remork` 会打开交互式命令菜单。
- 面向人的普通文本输出使用 inline TUI 风格：彩色分区、进度条、表格、警告和下一步命令提示。

## 当前状态

Remork 目前是 Product V1 beta，适合小团队和 Agent 辅助的私有服务器开发流程。

Release 提供这些二进制文件：

```text
remork-darwin-arm64     macOS 客户端，Apple Silicon
remork-darwin-amd64     macOS 客户端，Intel
remorkd-linux-arm64     Linux daemon，arm64
remorkd-linux-amd64     Linux daemon，amd64
```

## 快速开始

这一套流程会安装 macOS 客户端，然后通过引导式 setup flow 准备 Linux daemon、绑定本地目录，并同步远端 workspace。

### 1. 安装 macOS 客户端

```bash
VERSION=v0.1.1.beta02
case "$(uname -m)" in
  arm64) CLIENT_PLATFORM=darwin-arm64 ;;
  x86_64) CLIENT_PLATFORM=darwin-amd64 ;;
  *) echo "unsupported local macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac

mkdir -p "$HOME/.local/bin"
curl -L -o "$HOME/.local/bin/remork" \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-${CLIENT_PLATFORM}"
chmod 0755 "$HOME/.local/bin/remork"
export PATH="$HOME/.local/bin:$PATH"
remork version
```

如果新开的终端找不到 `remork`，把下面这行加入你的 shell 配置：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### 2. 运行引导式 setup

人在终端里使用时，优先走产品化 setup flow：

```bash
remork setup
```

setup 会先询问你要做什么：连接当前项目、准备服务器、更新已有服务器，或修复已有配置。它和脚本化命令使用同一套 operation spec，会先展示 review plan，然后才修改 host、daemon 或 workspace 状态。

### 3. 脚本化安装 daemon

需要非交互脚本时再使用高级 daemon 命令。这条路径和 setup 内部使用的是同一套操作代码。

推荐安装方式使用共享 token。token 用于避免 daemon 在私有网络内被未授权客户端访问。

```bash
HOST_ALIAS=my-lab
SSH_TARGET=user@my-server
DAEMON_URL=http://remork-daemon.example.internal:17731
ALLOWED_ROOT=/home/me

export REMORK_TOKEN="$(openssl rand -hex 32)"
mkdir -p "$HOME/.remork"
printf '%s\n' "$REMORK_TOKEN" > "$HOME/.remork/remork.token"
chmod 0600 "$HOME/.remork/remork.token"
REMOTE_TOKEN_FILE=".remork/remork.token"

printf '%s\n' "$REMORK_TOKEN" | ssh "$SSH_TARGET" \
  "mkdir -p \"\$HOME/.remork\" && umask 077 && cat > \"\$HOME/$REMOTE_TOKEN_FILE\""

remork daemon install "$HOST_ALIAS" \
  --ssh "$SSH_TARGET" \
  --url "$DAEMON_URL" \
  --root "$ALLOWED_ROOT" \
  --token-file "$REMOTE_TOKEN_FILE" \
  --token-env REMORK_TOKEN \
  -y \
  --verify \
  --no-proxy
```

只要没有显式传 `--dry-run`，`remork daemon install` 和 `remork daemon upgrade` 就会展示 plan，并在交互式终端中询问是否执行。脚本或非 TTY 场景使用 `-y/--yes` 直接执行。只有想查看 SSH/SCP plan、不修改服务器时，才显式加 `--dry-run`。交互式 daemon 表单会把部署参数放在同一屏里，包括 roots、SSH target、daemon URL、认证、verify、dry-run 以及 `--allow-unauthenticated-network-bind`。如果远端执行前的校验失败，会带着上一次输入重新打开表单，用户只需要改有问题的字段。

后续终端需要先加载同一个 token：

```bash
export REMORK_TOKEN="$(cat "$HOME/.remork/remork.token")"
```

Remork 会通过 SSH 自动识别远端 Linux 平台。只有自动识别不可用时才需要手动传 `--platform`。如果一个 daemon 需要服务多个基础目录，可以重复传 `--root`。

### 4. 绑定并同步 workspace

```bash
LOCAL_WORKING_COPY=~/remork/project
WORKSPACE_ROOT=/home/me/project

mkdir -p "$LOCAL_WORKING_COPY"
cd "$LOCAL_WORKING_COPY"

remork init "$HOST_ALIAS:$WORKSPACE_ROOT"
remork sync
remork status
```

`remork init` 不会安装 daemon。它只会把当前本地目录绑定到一个已经由 `remorkd` 服务的远端 workspace。

## 日常工作流

```bash
remork sync

# 在本机编辑文件

remork status
remork diff
remork apply
```

在远端 workspace 里执行命令：

```bash
remork run -- pwd
remork run "make test"
```

打开交互式远端 shell：

```bash
remork shell
```

脚本和 Agent 优先使用 `run`。人需要交互终端时使用 `shell`。

想交互式发现命令，可以直接运行：

```bash
remork
```

根菜单会按日常操作、设置、诊断等场景分组，用来帮助用户发现和启动常用命令。它不是完整的参数向导：需要 path、host 或 shell command 的命令，仍然需要显式输入参数，或等待对应命令自己的交互式提示。

## 核心概念

| 名称 | 含义 |
| --- | --- |
| Daemon | 运行在远端服务器上的小型 HTTP 服务，即 `remorkd`。 |
| Remork host | 本机保存的 daemon endpoint 昵称，例如 `my-lab`。 |
| SSH target | 只用于安装或升级 daemon 的 SSH 目标。 |
| Daemon URL | client 运行时访问的 HTTP URL，不是 SSH 端口。 |
| Allowed root | `remorkd` 被允许服务的远端基础目录。 |
| Workspace root | 绑定到本地 working copy 的具体项目目录。 |
| Local working copy | 你在本机实际编辑的目录。 |
| Sync snapshot | 本地元数据，用来检测本地修改和远端冲突。 |

`remorkd --root /home/me` 表示 daemon 可以服务 `/home/me` 下的 workspace。随后本地目录可以绑定到 `/home/me/project`、`/home/me/another-project`，或者任何位于该 allowed root 下的子目录。

## 命令

### 日常命令

| 命令 | 用途 |
| --- | --- |
| `remork sync` | 将远端状态同步到本地 working copy。 |
| `remork status` | 查看本地修改、远端更新、冲突和大文件占位符。 |
| `remork diff` | 查看本地修改和上次同步 base 之间的差异。 |
| `remork apply` | 将 review 过的本地修改写回远端 workspace。 |
| `remork run -- COMMAND` | 在远端执行非交互式命令。 |
| `remork shell` | 打开或恢复交互式远端 shell session。 |

### 设置和检查

```bash
remork host list
remork daemon status my-lab
remork workspace
remork workspace list --json
remork doctor
```

### 自动化友好的输出

```bash
remork init HOST:/remote/project --non-interactive
remork sync --json
remork status --json
remork apply --yes --non-interactive
remork doctor --json
remork daemon status HOST --json
remork sync --quiet --non-interactive
```

交互式和普通文本输出主要面向人。Agent 和脚本应全局使用 `--non-interactive`，并只在命令支持时使用 `--json`、`--quiet`、`--yes` 等命令级参数。`--yes` 主要用于已经 review 过的写入或部署流程，例如 `apply` 和 daemon 执行安装。`--color=never` 只会关闭 ANSI 颜色，不会把面向人的文本输出变成机器可解析格式。

每个命令都有更详细的 CLI help：

```bash
remork init -h
remork daemon install -h
remork shell -h
```

## 大文件

超过 daemon 阈值的文件默认不会下载。Product V1 默认阈值是 `128MB`，除非 daemon 启动时指定了其他值。

如果远端存在：

```text
checkpoints/model.tar.gz
```

本地 working copy 会收到：

```text
checkpoints/model.tar.gz.meta
```

需要完整内容时再下载：

```bash
remork pull checkpoints/model.tar.gz
```

脚本或 Agent 非交互运行时，需要显式确认大文件下载：

```bash
remork pull --force checkpoints/model.tar.gz
```

## 安全应用修改

远端 workspace 是事实来源。本地修改不会自动推送。

`remork apply` 会带上 `sync` 或 `pull` 时记录的 base hash。如果远端文件在此之后已经变化，daemon 会拒绝写入，避免覆盖较新的远端内容。

普通的 `remork apply` 默认跳过未跟踪的新文件。如果要创建一个新的远端文件：

```bash
remork apply path/to/new-file --include-untracked
```

如果要包含所有未被 ignore 的 untracked 文件：

```bash
remork apply --include-untracked
```

`remork apply` 适合已经 review 过的源码级修改。超过 `128MB` 的文件会在上传前被拒绝。大文件应尽量保留在远端，只有需要本地副本时再使用 `remork pull --force`。

使用 `.remorkignore` 放置永远不应 apply 的文件，例如缓存、密钥、虚拟环境、生成产物和 Agent 临时文件。Remork 会先读取 `.remorkignore`，再读取 `.gitignore`。

## 远端命令和 Shell

`remork run` 会在绑定的远端 workspace 中执行命令：

```bash
remork run -- pwd
remork run "pytest -q"
remork run --timeout 30s "go test ./..."
```

执行前，Remork 会检查本地和远端 workspace 状态。如果本地修改或冲突让命令执行不安全，它会停止并提示下一步。只有在你明确希望忽略本地待处理修改时，才使用 `--remote-only`。

当前版本的 `run` 会在远端命令结束后回放 stdout/stderr。需要长时间交互时使用 `remork shell`；脚本里使用 `run` 时可以配合 `--timeout` 设置硬超时。

`remork shell` 会通过 daemon 打开交互式远端 shell。它不是普通 SSH，但行为接近远端交互式 shell：从 workspace root 开始，使用远端用户的交互式 shell，并支持 attach / kill 保留的 session。

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

`remork shell` 需要真实终端。脚本和 Agent 应使用 `remork run -- COMMAND`。

## 排障

### `connect: connection refused`

client 访问到了 daemon URL 的 host/port，但那里没有进程监听。先检查保存的 host URL 和 daemon 状态：

```bash
remork host list
remork daemon status HOST
```

然后在脚本中用 `remork daemon install ... -y --verify` 安装或重启 daemon；在交互式终端中直接运行 `remork daemon install ...` 并确认提示。只想预览时加 `--dry-run`。

### `remote root is not advertised`

daemon 已经启动，但 workspace 路径不在它的 allowed roots 内。重新安装或重启 `remorkd`，并传入包含该 workspace 的 `--root`。

### `token env "REMORK_TOKEN" is not set`

host 配置使用了 `--token-env REMORK_TOKEN`。使用 Remork 前先加载 token：

```bash
export REMORK_TOKEN="$(cat "$HOME/.remork/remork.token")"
```

### 新文件被 `apply` 跳过

untracked 文件默认会跳过。可以 apply 指定新文件，或显式包含 untracked 文件：

```bash
remork apply path/to/new-file --include-untracked
```

### 只同步到了 `.meta` 文件

远端文件超过了大文件阈值。需要完整文件时显式 pull：

```bash
remork pull --force path/to/file
```

## 安全模型

Remork Product V1 假设：

- 使用可信 VPN 或私有网络；
- daemon 显式配置 allowed roots；
- 可通过 token file 和环境变量使用可选共享 token；
- 本地修改不会自动写回远端；
- 远端服务器不需要安装依赖。

当前限制：

- 没有账号、RBAC 或多租户隔离；
- 没有公网加固；
- daemon 配置主要通过启动参数完成；
- 本地配置存放在 `~/.remork`。

在可信 VPN 或私有网络中，可以跳过 token 准备，并在 install 时传 `--allow-unauthenticated-network-bind`。没有 token 时，Remork 会拒绝执行非 loopback 监听地址的安装，除非显式传入这个确认参数。

## 开发

```bash
go test ./...
go vet ./...
scripts/build-release.sh v0.1.1.beta02
```

CI 会在 push 和 pull request 上运行测试、vet 和 release build 检查。

## 文档

- [English README](README.md)
- [Daemon API](docs/remork-api.md)
- [Agent 操作指南](skills/remork/SKILL.md)
- [Product V1 验证记录](docs/remork-product-v1-validation.md)
- [可靠性验证记录](docs/remork-v1-10x-reliability.md)
