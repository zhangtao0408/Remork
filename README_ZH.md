<p align="center">
  <img src="docs/assets/header.png" alt="Remork：面向私有服务器的远端 workspace 控制工具" width="100%">
</p>

# Remork

面向私有服务器的远端 workspace 控制工具。

[English README](README.md)

Remork 会把远端服务器上的目录同步成一份本地 working copy。你在本机编辑、查看 diff，然后显式 `apply` 写回远端；命令和交互式 shell 仍然在远端机器上运行。

它适合可信 VPN 或私有网络里的服务器，尤其是不方便在每台机器上安装完整 Agent 运行环境的场景。

## 安装

安装 macOS 客户端：

```bash
VERSION=v0.1.1.beta03
case "$(uname -m)" in
  arm64) CLIENT_PLATFORM=darwin-arm64 ;;
  x86_64) CLIENT_PLATFORM=darwin-amd64 ;;
  *) echo "unsupported macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac

mkdir -p "$HOME/.local/bin"
curl -L -o "$HOME/.local/bin/remork" \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-${CLIENT_PLATFORM}"
chmod 0755 "$HOME/.local/bin/remork"
export PATH="$HOME/.local/bin:$PATH"

remork version
```

如果新开的终端找不到 `remork`，把下面这行加入 shell 配置：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

使用 PowerShell 安装 Windows 客户端：

```powershell
$Version = "v0.1.1.beta03"
$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }
$InstallDir = Join-Path $HOME ".local\bin"
New-Item -ItemType Directory -Force $InstallDir | Out-Null
Invoke-WebRequest -Uri "https://github.com/zhangtao0408/Remork/releases/download/$Version/remork-windows-$Arch.exe" -OutFile (Join-Path $InstallDir "remork.exe")
$env:Path = "$InstallDir;$env:Path"

remork version
```

如果新开的 PowerShell 找不到 `remork`，把 `%USERPROFILE%\.local\bin` 加入用户 `Path`。

## 设置

日常使用从这里开始：

```bash
remork setup
```

`setup` 是产品化引导流程。它可以准备或更新服务器、修复已有配置，或者把当前本地目录绑定到远端 workspace。真正修改 daemon、host 或 workspace 前，它会先展示 review plan。

通常只需要理解这几个概念：

- **SSH target**：Remork 通过它复制或更新 `remorkd`，例如 `user@server`。
- **Allowed root**：daemon 允许服务的远端基础目录，例如 `/home/me`。
- **Workspace root**：具体项目目录，例如 `/home/me/project`。
- **Daemon URL**：setup 完成后，本机 client 访问 daemon 的 HTTP URL。

如果 daemon URL 是私有 IP，而你的 shell 设置了代理变量，setup 里 **Bypass proxy** 建议选 yes。

## 日常使用

进入本地 working copy：

```bash
remork sync

# 在本机编辑文件

remork status
remork diff
remork apply
```

在远端 workspace 执行命令：

```bash
remork run -- pwd
remork run "pytest -q"
remork run --timeout 30s "go test ./..."
```

`remork run` 会先加载远端用户的 bash 环境，因此 `~/.bashrc` 里的变量可以在命令里使用。命令输出会在远端执行结束后回放。

打开交互式远端 shell：

```bash
remork shell
```

脚本和 Agent 用 `run`。人需要交互终端时用 `shell`。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `remork setup` | 引导式设置 server、host 和 workspace。 |
| `remork sync` | 将远端文件同步到本地 working copy。 |
| `remork status` | 查看本地修改、远端更新、冲突和大文件占位符。 |
| `remork diff` | 查看本地修改与上次同步 base 的差异。 |
| `remork apply` | 将 review 过的本地修改写回远端。 |
| `remork pull PATH` | 下载指定文件或目录。 |
| `remork run -- COMMAND` | 在远端执行非交互式命令。 |
| `remork shell` | 打开交互式远端 shell。 |
| `remork doctor` | 检查本地和远端是否就绪。 |

在真实终端中直接运行 `remork`，可以打开命令菜单。

## 大文件

大文件默认不会完整下载，本地会收到 `.meta` 占位符。需要完整内容时再显式 pull：

```bash
remork pull checkpoints/model.tar.gz
```

脚本或 Agent 非交互运行时，需要显式确认大文件下载：

```bash
remork pull --force checkpoints/model.tar.gz
```

## 写回安全

- 远端 workspace 是事实来源。
- 本地修改不会自动写回。
- `remork apply` 会检查同步时记录的 base，避免静默覆盖远端较新的修改。
- untracked 新文件默认跳过，需要显式 opt in：

```bash
remork apply path/to/new-file --include-untracked
```

使用 `.remorkignore` 放置永远不该 apply 的内容，例如缓存、密钥、虚拟环境、生成产物和 Agent 临时文件。

## 脚本化设置

需要自动化而不是引导式 setup 时，使用高级命令：

```bash
remork daemon install my-lab \
  --ssh user@server \
  --url http://server:17731 \
  --root /home/me \
  --token-file .remork/remork.token \
  --token-env REMORK_TOKEN \
  --no-proxy \
  -y \
  --verify

remork init my-lab:/home/me/project --non-interactive
remork sync --non-interactive
```

共享 VPN 或多人网络建议启用 token。可信私有网络里可以选择不启用 token，但非 loopback 监听地址的安装需要显式传 `--allow-unauthenticated-network-bind`。

脚本需要机器可读输出时，按命令使用 `--json`、`--quiet`、`--yes`，并使用全局 `--non-interactive`。`--color=never` 只关闭 ANSI 颜色，不会把面向人的文本变成机器可解析格式。

## 排障

```bash
remork doctor
remork host list
remork daemon status HOST
remork workspace
```

常见处理：

- **Connection refused**：host URL 已保存，但 daemon 没有监听。运行 `remork setup`，选择 repair 或 update。
- **私有 IP 返回 HTTP 502**：本机代理可能拦截了 daemon URL。给该 host 启用 `--no-proxy`，或在 setup 中选择 **Bypass proxy**。
- **Remote root is not advertised**：daemon 的 allowed root 不包含 workspace。更新 daemon 的 root。
- **Token env is not set**：加载 host 配置的 token 环境变量，例如 `export REMORK_TOKEN="$(cat ~/.remork/remork.token)"`。
- **只同步到 `.meta` 文件**：远端文件较大；确实需要本地内容时运行 `remork pull --force PATH`。

每个命令都有更聚焦的帮助：

```bash
remork setup -h
remork run -h
remork daemon install -h
```

## 开发

```bash
go test ./...
go vet ./...
scripts/build-release.sh v0.1.1.beta03
```

## 更多文档

- [English README](README.md)
- [Daemon API](docs/remork-api.md)
- [Agent 操作指南](skills/remork/SKILL.md)
- [Product V1 验证记录](docs/remork-product-v1-validation.md)
