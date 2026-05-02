# Remork

## Remork 是什么

Remork 让你可以在本地编辑一个远端目录。

典型流程是：在远端机器上启动 `remorkd`，在本机把一个本地目录绑定到远端 workspace，然后把远端文件同步到本地。本地文件可以正常编辑，但不会自动写回远端；只有在你明确运行 `remork apply` 时，本地修改才会经过安全检查后应用到远端。

远端机器只需要一个预编译好的 `remorkd` 二进制文件。不需要在远端安装 Go、npm、apt、brew，也不要求远端能联网。

Remork V1 面向可信 VPN 或内网环境。它支持可选的共享 bearer token，但这不是账号系统，也不应该直接暴露在公网。

## 五分钟上手

先从 GitHub Release 下载本机要用的 macOS 客户端和远端要用的 Linux daemon：

```bash
VERSION=v0.1.0
curl -L -o remork \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-darwin-arm64"
chmod 0755 remork
mkdir -p ~/bin
mv remork ~/bin/remork

curl -L -o remorkd \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remorkd-linux-arm64"
chmod 0755 remorkd
```

把 daemon 拷贝到远端并启动：

```bash
scp remorkd lab-a:/tmp/remorkd
ssh lab-a 'chmod 0755 /tmp/remorkd'
ssh lab-a 'nohup /tmp/remorkd --root /data/project-a --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
```

`--addr 0.0.0.0:17731` 会让 Remork 对能访问该 VPN/内网地址的机器开放。如果这是共享 VPN 或多人网络，建议启动 `remorkd` 时使用 `--token-file`，并在本地通过 `remork host add --token-env` 配置 token。

在本机配置 CLI：

```bash
remork host add lab-a --url http://10.0.0.12:17731
mkdir project-a && cd project-a
remork init lab-a:/data/project-a
remork sync
remork status
```

之后你可以像普通本地目录一样编辑文件。编辑完成后，先看 diff，再明确应用到远端：

```bash
remork diff
remork apply
```

## 必须知道的六个命令

### `remork init HOST:/absolute/remote/root`

把当前本地目录绑定到某个已配置的 daemon host 和远端目录。

绑定后，日常命令会从当前目录自动识别 workspace，不需要每次都写远端路径。

### `remork sync`

把远端文件同步到本地 working copy，并记录一份本地 base state。

默认情况下，大文件不会直接下载，而是生成 `.meta` 占位文件。

### `remork status`

查看当前本地目录的状态：是否干净、是否有本地修改、是否有远端更新、是否存在冲突、是否有大文件占位。

### `remork apply`

把本地修改写回远端。

`apply` 会带上同步时记录的 base hash。如果远端文件在你上次同步后已经变了，daemon 会拒绝覆盖并返回冲突。

### `remork run -- COMMAND ...`

在远端 workspace 中运行一条非交互式命令。

### `remork shell`

通过 daemon 打开一个交互式远端 shell。

## 日常示例

添加一个 daemon endpoint：

```bash
remork host add lab-a --url http://10.0.0.12:17731
remork host list
```

如果你的本地 shell 里有代理，而代理访问不到 VPN 地址，可以对这个 host 绕过代理：

```bash
remork host add lab-a --url http://10.0.0.12:17731 --no-proxy
```

使用共享 token，但不把密钥直接写进配置：

```bash
export REMORK_LAB_TOKEN='replace-with-real-token'
remork host add lab-a --url http://10.0.0.12:17731 --token-env REMORK_LAB_TOKEN
```

绑定并同步一个 workspace：

```bash
mkdir ~/work/project-a
cd ~/work/project-a
remork init lab-a:/data/project-a
remork sync
remork workspace
```

运行远端检查：

```bash
remork run -- make test
```

应用一份本地修改：

```bash
remork status
remork diff
remork apply
```

## 大文件

超过 daemon 阈值的文件默认不会被下载。Remork Product V1 默认阈值是 `128MB`，除非启动 daemon 时另外指定。

如果远端 workspace 中有：

```text
/data/project-a/checkpoints/model.tar.gz
```

本地 working copy 会收到：

```text
checkpoints/model.tar.gz.meta
```

这个 `.meta` 文件会记录远端路径、文件大小、版本信息和 pull 命令。只有当你确实需要完整内容时，再执行：

```bash
remork pull checkpoints/model.tar.gz
```

`.meta` 里记录的 pull 命令有时会是完整引用，例如：

```text
lab-a:/data/project-a/checkpoints/model.tar.gz
```

这样即使把 `.meta` 文件拷贝到绑定目录外，也仍然能知道它原本对应哪个远端文件。

## 安全地 apply

Remork 把远端 workspace 当作事实来源。你可以在本地编辑文件，但本地修改不会自动推送到远端。

`remork apply` 会发送一个 changeset，里面带着 `sync` 或 `pull` 时记录的 base hash。如果远端文件已经不再匹配这个 base hash，daemon 会返回冲突，并且不会继续覆盖该文件。

这能避免几类常见问题：

- 另一个人同时修改了同一个远端文件；
- 远端命令生成或改写了文件；
- Agent 拿着过期的本地副本去覆盖远端。

建议在 apply 前先运行：

```bash
remork status
remork diff
```

## 冲突恢复

当本地修改和远端更新碰到同一个文件时，先看详细状态：

```bash
remork status --verbose
```

对每个冲突文件，可以查看引导式恢复步骤：

```bash
remork conflict -- path/to/file
```

查看你的本地修改和同步 base 之间的差异：

```bash
remork diff -- path/to/file
```

如果要丢弃这个文件上的本地修改，可以恢复到本地 base cache。注意，这并不是直接接受当前远端更新：

```bash
remork restore -- path/to/file
```

恢复后再检查状态：

```bash
remork status
```

如果远端仍然有更新，再同步下来：

```bash
remork sync
```

之后继续 review 或 apply：

```bash
remork apply
```

新的本地文件不会被宽泛的 `remork apply` 自动创建到远端。你需要明确选择它：

```bash
remork apply path/to/new-file
```

如果你确认要应用所有未跟踪文件，可以使用：

```bash
remork apply --include-untracked
```

Remork 会先读取 `.remorkignore`，再读取 `.gitignore`。建议用 `.remorkignore` 排除本地缓存、密钥、生成产物、虚拟环境、Agent 临时文件等不应该 apply 到远端的内容。

## 运行命令和交互式 shell

非交互式命令使用 `run`：

```bash
remork run -- pwd
remork run -- make test
remork run -- python scripts/check.py
```

需要交互时使用 `shell`：

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

如果客户端断开，shell session 会在 daemon 侧保留一段时间。可以用 `remork shell --list` 查找保留的 session，用 `--attach` 重新连接，用 `--kill` 停止它。

空闲 session 会在 daemon 配置的保留窗口之后被回收。

## 操作日志

每个远端 workspace 都有自己的操作日志：

```text
<workspace>/.remork/log/operations.jsonl
```

daemon 扫描 manifest 时会跳过 `.remork`，所以这个日志不会同步到本地 working copy。

日志可以用来查看 daemon 侧发生过的操作，例如 manifest、download、apply、exec、shell、operations 请求。

查看最近日志：

```bash
remork log
```

## 后续学习的命令

### `remork pull PATH`

拉取指定文件或目录。大文件需要完整内容时主要用这个命令。

### `remork diff`

查看本地修改相对于同步 base 的差异。

### `remork restore -- PATH`

丢弃本地修改，恢复到同步 base cache，而不是当前远端最新内容。运行后建议再执行 `remork status`。如果远端仍有更新，再运行 `remork sync`。

### `remork conflict -- PATH`

显示某个冲突文件的本地 diff、restore、status、apply 等恢复指引。

### `remork log`

查看远端 workspace 的近期 Remork 操作日志。

### `remork watch`

监听 daemon 文件事件，让本地 working copy 持续跟随远端更新。

### `remork host`

管理 daemon endpoint alias。

### `remork workspace`

查看或移除当前本地目录的 workspace 绑定。

常用维护命令：

```bash
remork host list
remork host remove lab-a
remork workspace
remork workspace remove
```

## 调试和维护命令

### `remork doctor`

检查本地配置、当前 workspace 绑定、token 设置、daemon 可达性、远端 root 白名单、manifest 访问、操作日志访问等。

### `remork debug manifest`

拉取 daemon manifest，用于排查扫描、ignore、hash、大文件识别等问题。

### `remork debug events`

连接 daemon 文件事件流，并打印规范化后的事件信息。

### `remork debug api`

直接探测 daemon API，并输出简洁的传输层诊断。

### `remork daemon status HOST`

读取本地配置中的 host，调用远端 `/status`，打印 daemon 版本、平台、允许访问的 roots、大文件阈值、watch 支持情况和本地认证状态。

### `remork daemon install HOST --root /remote/root`

打印离线安装 daemon 所需的 `scp` 和 `ssh` 命令。默认只打印，不执行。

当默认值不合适时，可以配合这些参数：

```bash
--ssh
--platform
--local-bin
--remote-bin
--addr
--token-file
```

如果要让 Remork 直接执行生成的安装命令：

```bash
remork daemon install lab-a --root /data/project-a --ssh lab-a --platform linux-arm64 --execute --yes
```

### `remork daemon upgrade HOST`

打印替换远端 daemon 二进制的命令。如果提供 `--root`，也会包含启动参数。加上 `--execute --yes` 后会直接执行。

## Release 下载和离线部署 daemon

GitHub Release 只发布裸二进制文件：

```text
remork-darwin-arm64     # macOS 客户端，Apple Silicon
remork-darwin-amd64     # macOS 客户端，Intel
remorkd-linux-arm64     # Linux 服务端 daemon，arm64
remorkd-linux-amd64     # Linux 服务端 daemon，amd64
```

本机下载匹配的 macOS client 二进制，远端下载或拷贝匹配的 Linux daemon 二进制即可。远端服务器不需要安装 Go，也不需要联网。具体安装命令和 checksum 会写在 GitHub Release 正文里。

如果你需要在本地自行构建 release：

```bash
scripts/build-release.sh v0.1.0
```

本地构建还会在 `dist/` 下留下原始交叉编译二进制，方便测试和手动部署：

```text
dist/remork-darwin-arm64
dist/remork-darwin-amd64
dist/remork-linux-arm64
dist/remork-linux-amd64
dist/remorkd-darwin-arm64
dist/remorkd-darwin-amd64
dist/remorkd-linux-arm64
dist/remorkd-linux-amd64
dist/remorkd.example.toml
dist/checksums.txt
dist/RELEASE_BODY.md
```

例如远端是 Linux arm64：

```bash
scripts/build-release.sh v0.1.0
remork daemon install lab-a --root /data/project-a --ssh lab-a --platform linux-arm64
```

daemon install 默认只打印命令。如果要让 Remork 从本机直接执行生成的 `scp` 和 `ssh` 命令：

```bash
remork daemon install lab-a --root /data/project-a --ssh lab-a --platform linux-arm64 --execute --yes
```

这里 SSH 只用于安装辅助，也就是复制和启动 `remorkd`。Remork 正常运行时的传输仍然是 HTTP 到已配置的 `remorkd` 地址。

生成的命令大致等价于：

```bash
scp dist/remorkd-linux-arm64 lab-a:/tmp/remorkd
ssh lab-a 'chmod 0755 /tmp/remorkd'
ssh lab-a 'nohup /tmp/remorkd --root /data/project-a --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
curl --noproxy '*' http://10.0.0.12:17731/status
```

再次提醒：`--addr 0.0.0.0:17731` 会让 Remork 对能访问该 VPN/内网地址的机器开放。共享 VPN 或多人网络中，请使用 `--token-file`，并在本地通过 `remork host add --token-env` 配置 token。

也可以运行 smoke helper：

```bash
scripts/remote-smoke.sh \
  --host lab-a \
  --probe-host 10.0.0.12 \
  --root /tmp/remork-e2e \
  --port 17731 \
  --binary dist/remorkd-linux-arm64
```

清理临时 daemon：

```bash
ssh lab-a 'if [ -f /tmp/remorkd.pid ]; then kill "$(cat /tmp/remorkd.pid)" 2>/dev/null || true; fi; rm -f /tmp/remorkd.pid /tmp/remorkd.log /tmp/remorkd'
```

## 安全模型和当前限制

Remork Product V1 的前提是：

- 使用可信 VPN 或内网；
- 可选共享 token，通过环境变量或 token 文件提供；
- daemon 启动时明确指定可访问的远端 roots；
- 本地不会自动写入远端；
- daemon 部署过程中不要求远端安装依赖或访问互联网。

当前限制：

- 没有账号、RBAC 或多租户隔离；
- 没有面向公网暴露的安全加固；
- Product V1 的 daemon 配置主要通过命令行 flags；
- 本地配置是 `~/.remork` 下的 JSON，即使部署示例里可能会出现 TOML 模板。

## 开发者 API 说明

大多数用户只需要使用 CLI。daemon API 的细节和请求结构在：

```text
docs/remork-api.md
```

Agent 使用指引在：

```text
skills/remork/SKILL.md
```

运行核心验证：

```bash
go test ./...
scripts/build-release.sh dev
```
