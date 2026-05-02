# Remork

面向私有服务器的远端 workspace 控制工具。

Remork 在远端机器上运行一个轻量 daemon，在本机维护一份可编辑 working copy。你先
从远端同步文件，在本地编辑和查看 diff，然后通过 `remork apply` 显式写回远端。同
一个 daemon 也可以在远端 workspace 中运行命令或打开交互式 shell。

Remork 面向可信 VPN 或内网环境。Product V1 支持可选的共享 token 认证，但它不是
账号系统，也不应该直接暴露在公网。

## 当前状态

Remork 目前处于 Product V1 阶段，适合小团队和 Agent 辅助的远端开发场景，尤其是
不方便在每台服务器上安装完整 Agent 环境的情况。

Release 提供以下裸二进制文件：

```text
remork-darwin-arm64     macOS 客户端，Apple Silicon
remork-darwin-amd64     macOS 客户端，Intel
remorkd-linux-arm64     Linux daemon，arm64
remorkd-linux-amd64     Linux daemon，amd64
```

## 安装

安装本机 macOS client：

```bash
VERSION=v0.1.1.beta01
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

如果新打开的终端找不到 `remork`，把下面这行加入 shell 配置：

```bash
export PATH="$HOME/.local/bin:$PATH"
```

通过 SSH 安装或启动远端 daemon：

```bash
remork daemon install my-lab \
  --ssh my-lab \
  --url http://remork-daemon.example.internal:17731 \
  --root /home/me \
  --platform linux-arm64 \
  --execute --yes \
  --verify \
  --no-proxy
```

daemon 二进制会被复制到远端用户 home 下的持久路径。远端不需要安装 Go，也不需要
能访问互联网。

执行安装时，Remork 会检查远端是否已经存在 `remorkd`，在可用时报告已有版本，复制
新二进制后验证远端二进制版本；如果使用了 `--verify`，还会继续校验 daemon
`/status` 的版本和 allowed roots。

x86_64 服务器把 `linux-arm64` 换成 `linux-amd64`。如果一个 daemon 需要管理多个
基础目录，可以重复传 `--root`。

## 快速开始

把本地目录绑定到远端 workspace：

```bash
mkdir -p ~/remork/project
cd ~/remork/project

remork init my-lab:/home/me/project
remork sync
remork status
```

在本地编辑文件，然后查看并应用修改：

```bash
remork diff
remork apply
```

在远端 workspace 中运行命令：

```bash
remork run -- pwd
remork run -- make test
remork shell
```

## 核心概念

| 名称 | 含义 |
| --- | --- |
| Remork host | 本机给 daemon endpoint 起的名字，例如 `my-lab`。 |
| SSH target | 只用于安装或升级 daemon 的 SSH 目标。 |
| Daemon URL | client 运行时访问的 HTTP 地址，不是 SSH 端口。 |
| Allowed root | `remorkd` 允许访问的远端基础目录。 |
| Workspace root | 绑定到本地 working copy 的具体项目目录。 |
| Local working copy | 你在本机实际编辑的目录。 |

`remorkd --root /home/me` 表示 daemon 可以服务 `/home/me` 下面的 workspace。本地
目录可以绑定到 `/home/me/project`、`/home/me/another-project`，或者其他位于该
allowed root 下面的子目录。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `remork sync` | 把远端状态同步到本地 working copy。 |
| `remork status` | 查看本地修改、远端更新、冲突和大文件占位。 |
| `remork diff` | 查看本地修改和上次同步 base 之间的差异。 |
| `remork apply` | 把确认过的本地修改写回远端 workspace。 |
| `remork pull PATH` | 下载被大文件占位符替代的完整文件。 |
| `remork run -- COMMAND` | 在远端运行非交互式命令。 |
| `remork shell` | 打开或恢复交互式远端 shell。 |
| `remork doctor` | 检查本地配置、daemon 可达性、root 覆盖和日志访问。 |

主机和 workspace 辅助命令：

```bash
remork host list
remork daemon status my-lab
remork workspace
```

耗时较长的 `sync` 会显示阶段和操作进度；`--quiet` 或 `--json` 会关闭这些文本提示。

## 大文件

超过 daemon 阈值的文件默认不会下载。Product V1 默认阈值是 `128MB`，除非 daemon
启动时另行指定。

远端如果存在：

```text
checkpoints/model.tar.gz
```

本地 working copy 会收到：

```text
checkpoints/model.tar.gz.meta
```

只有需要完整内容时再拉取：

```bash
remork pull checkpoints/model.tar.gz
```

## 应用修改

远端 workspace 是事实来源。本地修改不会自动推送。

`remork apply` 会带上 `sync` 或 `pull` 时记录的 base hash。如果远端文件在此之后
已经变化，daemon 会拒绝写入，避免覆盖较新的远端内容。

新的本地文件不会被普通 `remork apply` 自动创建到远端，除非你明确选择：

```bash
remork apply path/to/new-file
remork apply --include-untracked
```

本地缓存、密钥、虚拟环境、生成产物、Agent 临时文件等不应该写回远端的内容，建议
放进 `.remorkignore`。Remork 会先读 `.remorkignore`，再读 `.gitignore`。

## 远端 Shell

`remork shell` 会通过 daemon 打开交互式 shell。client 断开后，session 仍会保留在
daemon 中。

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

## 安全模型

Remork Product V1 假设：

- 使用可信 VPN 或内网；
- daemon 启动时显式配置 allowed roots；
- 可通过 token file 和环境变量使用可选共享 token；
- 本地修改不会自动写回远端；
- 部署 daemon 时不要求远端安装依赖。

当前限制：

- 没有账号、RBAC 或多租户隔离；
- 没有公网加固；
- daemon 配置主要通过启动参数完成；
- 本地配置存放在 `~/.remork`。

## 文档

- [English README](README.md)
- [Daemon API](docs/remork-api.md)
- [Product V1 验证记录](docs/remork-product-v1-validation.md)
- [可靠性验证记录](docs/remork-v1-10x-reliability.md)
- [Agent 操作指南](skills/remork/SKILL.md)
