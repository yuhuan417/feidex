# 运行与升级

本文覆盖一键安装、daemon 模式、自动升级与发布流程。配置见 [配置参考](configuration.md)。

> 工程层面的部署约束以 [DEVELOPER.md](../DEVELOPER.md) 的 Deployment Notes / Build And Release Requirements 为准。

## 一键安装脚本

从 GitHub Release 下载对应平台的二进制、校验 `sha256sums.txt`、装到 PATH，无需 Go 工具链。支持 Linux / macOS / Windows（amd64 / arm64）。

Linux / macOS（`install.sh`）：

```bash
curl -fsSL https://raw.githubusercontent.com/yuhuan417/feidex/main/install.sh | bash
```

Windows（`install.ps1`，PowerShell）：

```powershell
irm https://raw.githubusercontent.com/yuhuan417/feidex/main/install.ps1 | iex
```

选项（`install.sh` 写在 `| bash -s --` 之后；`install.ps1` 用 PowerShell 参数 `-Version` / `-Dev` / `-BinDir` / `-Repo` / `-NoModifyPath`）：

- `--version <vX.Y.Z>` / `-Version`
  - 安装指定版本（默认最新 release）
- `--dev` / `-Dev`
  - 安装最新开发版（`dev-latest` prerelease）
- `--bin-dir <dir>` / `-BinDir`
  - 安装目录（`install.sh` 默认 `~/.local/bin`；`install.ps1` 默认 `%LOCALAPPDATA%\Programs\feidex`）
- `--repo <owner/name>` / `-Repo`
  - 指定来源仓库（默认 `yuhuan417/feidex`）
- `--no-modify-path` / `-NoModifyPath`
  - 不自动把安装目录写进 PATH（shell profile / 用户 PATH）
- `-h` / `--help`
  - 显示帮助（仅 `install.sh`）

环境变量等价：`FEIDEX_VERSION`、`FEIDEX_REPO`、`FEIDEX_BIN_DIR`。

行为：

- 识别 OS/架构，自动选对应资产：`feidex-linux-amd64` / `feidex-linux-aarch64` / `feidex-darwin-amd64` / `feidex-darwin-arm64` / `feidex-windows-amd64.exe` / `feidex-windows-arm64.exe`
- 强制校验 SHA-256（与 release 的 `sha256sums.txt` 比对，不一致即中止）
- 原子替换（可重复执行以升级；二进制正被占用时也安全）
- 安装目录不在 PATH 时，提示或写入 PATH（`--no-modify-path` / `-NoModifyPath` 可关闭）

> Windows 上 `feidex serve` 可直接运行；但 [Daemon 模式](#daemon-模式)与 `/upgrade` [自动升级](#自动升级)为 Linux 专属。离线环境请用 [手动安装 release 包](#release-资产) 或[从源码构建](../README.md#从源码构建)。

## Daemon 模式

Feidex 支持把自己作为后台守护进程常驻：Linux 用 systemd 用户服务，macOS 用 launchd 用户级 LaunchAgent。两者命令一致，平台差异由 `feidex daemon` 自动处理。

命令：

```bash
feidex daemon install
feidex daemon enable-linger
feidex daemon start
feidex daemon stop
feidex daemon restart
feidex daemon status
feidex daemon logs [-n 50] [-f]
feidex daemon uninstall
```

说明：

- `install`
  - 安装并启动用户服务；默认读取当前目录的 `config.toml`
  - 安装前会默认尝试为当前用户打开 linger；如果你不希望修改这个用户级 systemd 设置，可改用 `feidex daemon install --disable-linger`
- `enable-linger`
  - 手动为当前用户打开 linger；通常不需要单独执行，`install` 默认就会尝试开启
- `start` / `stop` / `restart` / `status` / `uninstall`
  - 也默认读取当前目录的 `config.toml`，按其中的 `[daemon].service_name` 选择实例
- `logs`
  - Linux：通过 `journalctl --user -u <service>.service` 查看
  - macOS：tail launchd 写入的日志文件（见下文 macOS 小节）
  - `-n` 控制输出行数，`-f` 持续跟随
- `upgrade-runner`
  - 内部升级 runner，不需要手动调用

### Linux（systemd 用户服务）

- unit 文件写在 `~/.config/systemd/user/<service>.service`
- `install` 默认尝试为当前用户开启 linger（开机/登出后仍运行）；不想改这个设置用 `--disable-linger`
- 日志由 journald 管理，`feidex daemon logs` 走 `journalctl`

### macOS（launchd LaunchAgent）

- plist 写在 `~/Library/LaunchAgents/<service>.plist`，通过 `launchctl bootstrap` / `bootout` / `kickstart` 管理
- `RunAtLoad` + `KeepAlive`：登录后自动启动、崩溃自动拉起
- 日志：launchd 把 stdout/stderr 写到 `~/Library/Logs/feidex/<service>.log`，`feidex daemon logs` 直接 tail 该文件
- `enable-linger` 在 macOS 上是 no-op（LaunchAgent 登录即起）。**开机即起（免登录）需要 root 级 LaunchDaemon，当前不支持**
- 若二进制是从浏览器下载被 Gatekeeper 隔离，先 `xattr -d com.apple.quarantine <binary>`（用 `install.sh` 经 curl 安装则无此问题）

> macOS daemon 仅负责常驻；`/upgrade` 自升级仍为 Linux 专属（见[自动升级](#自动升级)），macOS 升级请用 [install.sh](#一键安装脚本) 重新安装。

对应实现见：

- [daemon.go](../cmd/feidex/daemon.go)
- [manager.go](../internal/daemon/manager.go)
- [systemd_linux.go](../internal/daemon/systemd_linux.go) / [launchd_darwin.go](../internal/daemon/launchd_darwin.go)

## 自动升级

自动升级要求：

- 当前运行在 Linux daemon 模式
- 服务进程正在运行
- 如果走 GitHub release 流程，需要可访问 GitHub Release
- release 也会发布 macOS 产物，但自动升级仍只支持 Linux daemon

升级逻辑会：

- `/upgrade`
  - 查询最新 release
- `/upgrade dev`
  - 查询 `dev-latest` prerelease，获取当前 main 分支最近一次 push 产物
- `/upgrade vX.Y.Z`
  - 直接查询指定 tag，跳过最新版本探测
- `/upgrade local`
  - 打开当前 workspace 的文件选择器，选择本地 Binary
- `/upgrade path ./dist/feidex-linux-amd64`
  - 直接使用当前 workspace 下的本地 Binary，跳过 GitHub 查询
- 根据本机架构选择正确二进制
  - `amd64 -> feidex-linux-amd64`
  - `arm64 -> feidex-linux-aarch64`
- macOS release 对应为
  - `amd64 -> feidex-darwin-amd64`
  - `arm64 -> feidex-darwin-arm64`
- 对 release 流程校验 `sha256sums.txt`
- 对本地 Binary 流程先把制品复制到 `data_dir/upgrades/<request-id>/` 并计算 `sha256`
- 替换、重启
- 启动失败自动回退

设计约束：

- `/upgrade` 是救援路径；即使线程、工作区、Codex RPC、普通交互流程等其它功能异常，升级链路也必须尽量保持可用
- 升级实现不能依赖其它业务功能的成功执行；允许依赖的前置条件应尽量收敛到 daemon 状态、release 查询、确认卡片和本地 pending store
- 任何改动升级链路的提交，都必须补或更新独立的 upgrade 隔离测试，证明 `/upgrade` 和确认动作在缺失 `codex`/session 运行态时仍能工作

## 发布

### 手动指定版本

```bash
./scripts/create_github_release.sh v0.7.0
```

### 自动推导下一个 minor 版本

```bash
./scripts/create_github_release.sh
```

### 开发版发布

- 每次 `push main`，GitHub Actions 都会刷新 `dev-latest` tag 与同名 prerelease
- 开发版二进制会写入形如 `dev-20260416T233045-<shortsha>` 的版本号，带日期时间，便于 `/upgrade dev` 前后确认具体构建批次

脚本会以 `origin` 的 tag 为准，自动做：

- `v0.4.0 -> v0.5.0`
- `v0.4.3 -> v0.5.0`
- `v0.6.0 -> v0.7.0`

GitHub Actions 会在 tag push 后自动发布 release。

### Release 资产

当前 release workflow 会同时生成：

- `feidex-linux-amd64`
- `feidex-linux-amd64.tar.gz`
- `feidex-linux-aarch64`
- `feidex-linux-aarch64.tar.gz`
- `feidex-darwin-amd64`
- `feidex-darwin-amd64.tar.gz`
- `feidex-darwin-arm64`
- `feidex-darwin-arm64.tar.gz`
- `sha256sums.txt`
