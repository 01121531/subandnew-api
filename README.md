# SubAndNew API Control Plane

<p align="center">
  <img src="./web/default/public/logo.svg" alt="SubAndNew API Logo" width="96" height="96" />
</p>

<p align="center">
  <strong>统一纳管多个 New API、HUICHUAN-AI 与 Sub2API 实例</strong>
</p>

本仓库由 HUICHUAN-AI 二次开发而来，现已收缩为独立的多实例控制平面。业务请求仍由各个数据平面实例处理；本项目负责实例纳管、健康巡检、能力识别、受控运维、权限和审计。

详细设计与执行状态见 [多实例统一管理二开方案](./docs/MULTI-INSTANCE-MANAGEMENT-DESIGN.zh-CN.md)。

## 当前能力

- 纳管多个 New API、HUICHUAN-AI、Sub2API 和通用 HTTP 健康端点。
- 保存实例名称、类型、环境、标签、管理模式和巡检策略。
- 使用 AES-256-GCM 加密远端管理凭据，接口和审计日志不返回明文。
- 通过 SSRF 防护 Connector 限制私网、回环、重定向和 DNS 重绑定风险。
- 定时巡检实例健康、版本、延迟、能力和资源摘要。
- 支持 `observe`、`operate`、`enforce` 管理模式。
- 支持操作预览后执行、幂等键、实例级任务锁和审计记录。
- 首批受控动作：刷新资源、测试资源、启用或停用资源。
- 支持版本化配置模板、实例绑定、漂移检测、差异预览、定向应用、应用后校验和失败补偿。
- New API/HUICHUAN-AI 配置采用逐项写入；Sub2API 采用白名单部分对象写入，远端完整配置不落库。
- Root/管理员按细粒度 RBAC 权限访问实例中心。
- React 管理端提供实例列表、筛选、新增、编辑、凭据轮换和实例详情。

## 已移除模块

控制平面不再承载本地 AI 网关和商业化门户。当前二开已删除或关闭：

- Classic 前端、Electron 客户端和 Capture Proxy。
- Dataset Capture、NERV、排行榜、公开定价页和 Playground。
- 本地渠道、转发、视频等运行时路由及其后台 worker。
- Token、钱包、充值、订阅、兑换码、倍率、计费和使用日志等前后端实现。
- `LOG_DB`、ClickHouse、本地用量数据库和请求体磁盘缓存。
- 普通用户注册、账号绑定、删除账号及 API Access Token 鉴权。

控制平面账号只用于身份与权限管理，API 管理接口只接受登录会话，不兼容旧 `User.AccessToken`。旧数据库中的商业化表、列和配置不会在升级时自动删除或重新解释。

升级旧数据库时不会自动删除历史业务表。历史数据必须由管理员在备份后显式归档或清理。

### 旧数据归档与清理

维护命令不会启动 Web 服务，也不会执行自动迁移：

```bash
# 查看旧表、旧 users 列及数据库指纹
subandnew-api legacy-data inventory --output legacy-inventory.json

# 流式归档并生成列结构、逐表内容哈希与 SHA-256 校验文件
subandnew-api legacy-data archive --output legacy-archive.json

# 默认只预演；输出中会给出 execute 所需的数据库指纹
subandnew-api legacy-data purge --archive legacy-archive.json

# 校验归档完整性后显式清理
subandnew-api legacy-data purge --archive legacy-archive.json \
  --execute --confirm <database-fingerprint>
```

`purge` 会重新计算逐表和旧用户列内容哈希，数据在归档后发生任何变化都会拒绝清理。工具仅识别代码中明确列出的 HUICHUAN 历史表；插件表或其他未知表会单独报告并阻断归档与清理，不会因为“不属于控制平面”而被推断为可删除。管理员、权限、审计、任务和实例管理数据不会进入清理范围。

破坏性 `purge --execute` 当前仅支持 SQLite。MySQL/PostgreSQL 可执行清单和归档，但必须由 DBA 根据归档列结构审核并执行迁移，工具不会在这两类数据库上尝试跨方言 DDL。

从旧 HUICHUAN-AI 二进制迁移时需先手工部署一次 SubAndNew API；在线升级器仅识别 `subandnew-api-*` 资产和 `.subandnew-update` 状态目录，不保留旧产品名的 helper/资产兼容分支。完成首次部署后，后续版本可使用内置在线升级。

## 快速开始

### 1. 克隆

```bash
git clone https://github.com/01121531/subandnew-api.git
cd subandnew-api
```

### 2. 配置密钥

生成 32 字节随机值并编码为标准 Base64：

```bash
openssl rand -base64 32
```

至少配置：

```env
SESSION_SECRET=replace-with-a-random-session-secret
MANAGED_INSTANCE_SECRET_KEY=replace-with-the-base64-value
MANAGED_INSTANCE_SECRET_KEY_VERSION=v1
```

未配置 `MANAGED_INSTANCE_SECRET_KEY` 时，可以浏览不含密钥的实例信息，但系统会拒绝写入远端凭据。

### 3. 本地开发

```bash
docker compose -f docker-compose.dev.yml up -d --build
cd web/default
bun install
bun run dev
```

- 后端：http://localhost:3000
- 前端：http://localhost:3001

首次打开后按安装向导创建 Root 用户，然后进入 `/instances`。

生产镜像发布到 `ghcr.io/01121531/subandnew-api:latest`。

### 4. 源码构建

```bash
cd web/default
bun install
bun run build
cd ../..
go build ./...
```

## 关键环境变量

| 变量 | 说明 |
| --- | --- |
| `PORT` | 后端监听端口，默认 `3000` |
| `SQL_DSN` | PostgreSQL/MySQL 连接字符串；未设置时使用 SQLite |
| `SQLITE_PATH` | SQLite 文件路径 |
| `REDIS_CONN_STRING` | 可选 Redis 连接 |
| `SESSION_SECRET` | 登录会话签名密钥 |
| `MANAGED_INSTANCE_SECRET_KEY` | 32 字节标准 Base64 主密钥 |
| `MANAGED_INSTANCE_SECRET_KEY_VERSION` | 当前凭据密钥版本 |
| `MANAGED_INSTANCE_ALLOWED_CIDRS` | Connector 允许访问的私网 CIDR |
| `MANAGED_INSTANCE_PROBE_MAX_CONCURRENCY` | 巡检全局并发上限，默认 `8` |
| `MANAGED_INSTANCE_OPERATION_MAX_CONCURRENCY` | 受控操作全局并发上限，默认 `4` |
| `MANAGED_INSTANCE_OPERATION_MAX_PER_HOST` | 同一远端主机的操作并发上限，默认 `2` |
| `MANAGED_INSTANCE_BATCH_MAX_CONCURRENCY` | 同一批次的操作并发上限，默认 `2` |

默认拒绝访问私网和回环地址。如控制平面需要连接内网实例，必须通过 `MANAGED_INSTANCE_ALLOWED_CIDRS` 显式放行精确网段。

## 权限

| 权限 | 用途 |
| --- | --- |
| `managed_instance.view` | 查看实例和脱敏状态 |
| `managed_instance.create` | 新增实例和写入凭据 |
| `managed_instance.update` | 修改连接、策略和标签 |
| `managed_instance.delete` | 删除纳管关系 |
| `managed_instance.operate` | 执行单实例远端操作 |
| `managed_instance.batch_operate` | 执行批量操作 |
| `managed_instance.secret_rotate` | 轮换或撤销凭据 |
| `managed_instance.audit` | 查看详细审计信息 |
| `managed_template.view` | 查看配置模板、绑定、漂移和差异 |
| `managed_template.apply` | 管理模板、绑定并执行配置应用，仅 Root 默认拥有 |

## 验证

```bash
go test ./... -count=1
go vet ./...
cd web/default
bun run typecheck
bun run build
```

## 实施状态

- 已完成：实例模型、凭据加密、RBAC、适配器、SSRF 防护、巡检、实例中心、详情页、单实例及批量受控操作。
- 已完成：配置模板与版本化 Schema、漂移检测、差异预览、两阶段配置应用、应用后验证和失败补偿。
- 已完成：旧业务数据清单、流式归档、校验、预演和双确认显式清理工具。
- 已完成：本地 Relay、Channel、代理池、Token、计费、订阅、注册、旧通知/状态集成及商业化配置的代码删除；新安装只创建控制平面表。
- 可选后续：Phase 4 联邦路由。该能力不属于当前控制平面默认范围。

## 上游与许可

项目保留原 HUICHUAN-AI 的 AGPL-3.0 许可和版权声明。衍生项目的修改、部署和分发应继续遵守 [LICENSE](./LICENSE) 与 [NOTICE](./NOTICE)。
