# ADR: 多平台在线升级

## 状态

已接受。在线升级支持 Windows、Linux、macOS 的单节点独立二进制部署。

## 背景

系统设置中的版本检测需要从 GitHub Release 获取最新版本，并在服务端完成资产选择、SHA-256 校验、二进制替换、进程重启、健康检查和失败回滚。容器、Kubernetes 和多实例部署应继续由外部发布系统滚动升级，避免应用在运行时修改镜像或同时替换多个节点。

## 决策

1. 在线升级 API 仅允许 Root 调用。
2. 支持平台：
   - `windows/amd64`
   - `linux/amd64`
   - `linux/arm64`
   - `darwin/amd64`
   - `darwin/arm64`
3. 不支持 Docker、Kubernetes、多实例、`go run`、环境变量覆盖版本号、无法定位或无法替换当前可执行文件的场景。
4. 只安装固定仓库 `01121531/subandnew-api` 的最新稳定 SemVer Release；客户端只提交 `release_id`，服务端会重新拉取最新 Release 并校验 ID。
5. Release 资产命名约定：
   - Windows: `huichuan-ai-<version>-windows-amd64.exe` + `checksums-windows.txt`
   - Linux: `huichuan-ai-<version>-linux-amd64` / `huichuan-ai-<version>-linux-arm64` + `checksums-linux.txt`
   - macOS: `huichuan-ai-<version>-macos-amd64` / `huichuan-ai-<version>-macos-arm64` + `checksums-macos.txt`
6. 下载资产必须匹配当前 `GOOS/GOARCH`，大小受限，并通过 SHA-256 校验后才进入替换阶段。
7. 当前进程会复制自身为临时 helper。helper 等待原进程退出后备份旧版本、替换新版本、按原工作目录和参数启动服务，并轮询 `/api/status` 校验目标版本。
8. 新版本健康检查失败时，helper 会终止新进程、恢复旧二进制并尝试启动旧版本。
9. 状态保存在当前可执行文件相邻的 `.huichuan-update/state.json`，API 不返回本地路径、下载 URL、凭据或请求头。

## 状态机

```text
idle -> downloading -> verifying -> staged -> restarting -> validating -> succeeded
```

健康检查失败时：

```text
validating -> rolling_back -> rolled_back
```

替换前失败直接进入：

```text
failed
```

## 不变量

| 不变量 | 执行边界 |
| --- | --- |
| 非 Root 不能查询或启动在线升级 | `middleware.RootAuth()` |
| 客户端不能控制下载地址或本地路径 | controller + `pkg/systemupdate` |
| 校验失败绝不替换当前二进制 | 下载/校验阶段 |
| 替换前必须保留可恢复备份 | update helper |
| 容器和多实例不能显示可安装按钮 | capability API + frontend |
| 同一进程同一时间只能存在一个升级任务 | manager mutex + 本地状态 |

## API

- `GET /api/system-update/capability`
- `GET /api/system-update/latest`
- `GET /api/system-update/status`
- `POST /api/system-update`，请求体：`{"release_id": 123}`

## 验收标准

1. Root 可以检查最新版本、确认安装并观察从下载到重连的完整状态。
2. 非 Root 请求四个 API 均被拒绝。
3. 非 SemVer、缺少平台资产、超限或 SHA-256 不匹配均不会替换文件。
4. Windows、Linux、macOS helper 均可完成替换、启动健康版本和失败回滚。
5. 发布二进制的 `/api/status` 版本与 Release 标签一致。
6. 前端刷新或服务重启后仍可恢复并显示最后一次升级状态。
