# 运行与升级

本文覆盖 daemon 模式、自动升级与发布流程。配置见 [配置参考](configuration.md)。

> 工程层面的部署约束以 [DEVELOPER.md](../DEVELOPER.md) 的 Deployment Notes / Build And Release Requirements 为准。

## Daemon 模式

Feidex 支持 Linux 用户态 systemd daemon。

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
  - 通过 `journalctl --user -u <service>.service` 查看当前 daemon 日志
  - `-n` 控制输出行数，`-f` 持续跟随
  - 依赖 Linux/systemd journald
- `upgrade-runner`
  - 内部升级 runner，不需要手动调用

对应实现见：

- [daemon.go](../cmd/feidex/daemon.go)
- [manager.go](../internal/daemon/manager.go)

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
