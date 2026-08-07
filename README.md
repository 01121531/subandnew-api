# SubAndNew API Control Plane

<p align="center">
  <img src="./web/default/public/logo.svg" alt="SubAndNew API Logo" width="96" height="96" />
</p>

<p align="center">
  <strong>统一纳管多个 New API、HUICHUAN-AI 与 Sub2API 实例</strong>
</p>

本仓库由 HUICHUAN-AI 二次开发而来，正在收缩为独立的多实例控制平面。业务请求仍由各个数据平面实例处理；本项目负责实例纳管、健康巡检、能力识别、受控运维、权限和审计。

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
- Root/管理员按细粒度 RBAC 权限访问实例中心。
- React 管理端提供实例列表、筛选、新增、编辑、凭据轮换和实例详情。

## 已移除模块

控制平面不再承载本地 AI 网关和商业化门户。当前二开已删除或关闭：

- Classic 前端、Electron 客户端和 Capture Proxy。
- Dataset Capture、NERV、排行榜、公开定价页和 Playground。
- 本地渠道、转发、视频等运行时路由及其后台 worker。
- 钱包、充值、订阅、兑换码、使用日志等前端入口。

升级旧数据库时不会自动删除历史业务表。历史数据必须由管理员在备份后显式归档或清理。

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

## 验证

```bash
go test ./service/managedinstance ./service ./model ./controller ./router -run "ManagedInstance|Operation|SystemTask|Scoped" -count=1
cd web/default
bun run typecheck
bun run build
```

## 实施状态

- 已完成：实例模型、凭据加密、RBAC、适配器、SSRF 防护、巡检、实例中心、详情页、首批受控操作。
- 正在进行：进一步删除本地网关、计费、订阅、注册和历史配置耦合。
- 后续：配置基线、漂移检测、批量编排、显式历史数据归档工具和完整端到端测试。

## 上游与许可

项目保留原 HUICHUAN-AI 的 AGPL-3.0 许可和版权声明。衍生项目的修改、部署和分发应继续遵守 [LICENSE](./LICENSE) 与 [NOTICE](./NOTICE)。
