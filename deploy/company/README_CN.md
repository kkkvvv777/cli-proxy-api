# 公司内部网关

仅适用于单实例、约 30 人的 HTTP/SSE 网关。员工无注册入口，无模型或额度限制。
公司模式使用独立员工 Key 文件，原 `api-keys` 不再授予模型访问权限。
管理员仍使用原管理密钥；共享管理密钥不能区分管理员个人操作。

## Linux 部署

在本目录执行，先安装 Docker Engine 和 Compose：

```sh
umask 077
mkdir -p runtime/settings runtime/auths runtime/data runtime/logs runtime/plugins runtime/tls
cp config.example.yaml runtime/settings/config.yaml
printf 'MANAGEMENT_PASSWORD=%s\nGATEWAY_API_BIND_IP=127.0.0.1\nGATEWAY_API_PORT=8317\n' "$(openssl rand -hex 32)" > .env
```

本部署默认复用服务器已有的 Nginx：网关仅发布到云服务器本机的 `127.0.0.1:8317`，
现有 Nginx 负责 `80/443`、证书和域名转发；不要把 `8317` 加入云安全组。
因此不需要把证书复制到本项目的 `runtime/tls/`。只有启用可选的 `bundled-edge` profile
时，才需要把证书放到 `runtime/tls/server.crt` 和 `server.key`。
将下面的 Nginx 站点配置保存到 `/etc/nginx/sites-available/ai-longsun-lite.com`，
替换当前用于证书申请的临时配置：

```nginx
server {
    listen 80;
    listen [::]:80;
    server_name ai.longsun-lite.com;

    location /.well-known/acme-challenge/ {
        root /var/www/letsencrypt;
    }
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name ai.longsun-lite.com;

    ssl_certificate /etc/letsencrypt/live/ai.longsun-lite.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ai.longsun-lite.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    location /v1/ {
        client_max_body_size 0;
        proxy_pass http://127.0.0.1:8317;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-Proto https;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_cache off;
    }

    location / {
        proxy_pass http://127.0.0.1:8317;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-Proto https;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_cache off;
    }
}
```

然后执行 `sudo nginx -t && sudo systemctl reload nginx`。
检查配置通过后，在本目录执行 `docker compose config --quiet` 和
`docker compose up -d --build`。由于 `edge` 使用了 `bundled-edge` profile，默认不会启动第二个 Nginx。

浏览器访问 `https://公司域名/management.html`，输入 `.env` 的管理密钥。
统一复用原管理后台：在“配置管理”的 API Key 区域管理员工，在侧栏“调用审计”
查询历史。一次登录即可切换所有功能，没有第二套页面或密码输入。
员工创建、改名、启停、轮换及删除立即保存，无需再提交 YAML 配置草稿。
本模板按同域公开后台处理：页面可被发现，但管理 API 仍必须提供管理员密钥。
Token 用量插件的部分普通资源接口默认不要求管理密钥；如果不希望员工看到插件统计元数据，
不要公开后台，改用 VPN/SSH 隧道或在边缘层为 `/v0/resource/plugins/` 增加访问控制。
旧 `/management.html?company=1` 地址自动跳转到原配置管理的 Key 区域。
公司镜像从 `management/` 源码构建单文件面板并固定在镜像内，不挂载或在线替换前端。
前端代码沿用原项目，只扩展员工 Key 和审计；公司模式禁止原版面板自动更新覆盖。
发布时一同保存公司 Docker 镜像版本及源码版本，恢复需使用匹配的镜像。
上游 API Key 和 OAuth 使用原管理流程；需本机回调的供应商通过 SSH 隧道完成，
不要向公网暴露 OAuth 端口。启用任何订阅 OAuth 前核实供应商允许的使用范围；
不要将个人订阅默认视为可共享的企业 API 授权。

员工 Base URL：`https://公司域名/v1`，Key 在创建/轮换时仅展示一次。
支持 `/models`、`/chat/completions`、`/completions`、`/responses`、
`/responses/compact`，HTTP/SSE。其他协议、WebSocket、音视频未纳入本版公司入口。
轮换保持原启停状态；禁用/删除/轮换立即影响新请求，不强行中断已进行的模型响应。
模型请求不再由公司网关设置额外的字节上限，Nginx 的 `/v1/` 入口也不设置
额外的请求体上限，由 ChatGPT/Codex 上游按实际模型能力返回结果或超限错误。
员工管理 API 仍限制为 32 KiB，这是管理面保护，不作用于模型请求。
请求体放开不会提高上游推理速度；长会话仍应由客户端按上游策略压缩或新建会话。
这不是对官方限制的自动同步，也不承诺 OAuth 上游与官方 API 限制一致。
现有协议转换仍在内存中处理完整请求；取消固定上限不等于任意大小或并发都安全。
生产需监控内存并设置容器资源保护，大请求的实际容量须单独压测。

## 数据与审计

- `runtime/data/company-users.json`：员工姓名、编号、Key 前缀和 SHA-256 摘要，不保存完整 Key。
- `runtime/data/audit/*.jsonl`：按 UTC 日及 10 MiB 滚动，完整历史不自动清理。
- `request` 行表示客户端请求最终结果（Token 为 0）；`attempt` 行记录上游尝试和 Token。
  按 request 行统计请求数，按 attempt 行统计 Token，用 Request ID 关联，不能将两者相加计数。
- 流中失败可出现 HTTP 200 但 `failed=true`；供应商未返回用量时数字可能为 0，不代表免费。
- 列表读取最近 100/500/1000 行，可按员工过滤并导出；完整归档直接从服务器备份检索。
- 公司模式禁用详细请求/响应文件日志，包括失败正文；审计不落请求正文、回答、Headers、上游密钥。
- 写入失败记录错误且后台显示异常，不中断正在传输的模型响应。管理员需监控磁盘剩余空间、
  错误日志和审计健康。突然断电仍可能丢失在途请求记录，不是财务级账本。
- 使用单网关进程；禁止多个实例共用此文件目录。员工目录损坏会拒绝启动，不降级为匿名访问。
- `company-gateway.enabled/data-dir` 修改需要重启；普通上游配置继续使用原热加载。

## 备份与恢复

备份包含 `.env`、OAuth 凭据、配置和审计，是敏感数据。加密密码必须与备份分开保存。
`backup.sh` 短暂停止 gateway 获取一致副本，结束后恢复启动；默认不删除任何历史备份。

```sh
BACKUP_PASSWORD_FILE=/root/company-backup-password sh ./backup.sh
```

Linux 定时任务示例（用绝对路径替换，安装到管理员 crontab；此仓库不会自动修改系统任务）：

```cron
0 3 * * * BACKUP_PASSWORD_FILE=/root/company-backup-password /bin/sh /opt/company-gateway/deploy/company/backup.sh >> /var/log/company-gateway-backup.log 2>&1
```

把加密备份及校验文件复制到异机。恢复时先验证 `shasum -a 256 -c ...sha256`，
再在权限为 0700 的全新目录解密；不要覆盖运行目录：

```sh
umask 077
openssl enc -d -aes-256-cbc -pbkdf2 -iter 200000 -pass file:/root/company-backup-password \
  -in company-TIMESTAMP.tar.gz.enc -out restored.tar.gz
tar -tzf restored.tar.gz
tar -xzf restored.tar.gz
```

确认内容后停止网关，将原 runtime/.env 移到保留目录，再替换为恢复内容并启动。
验证员工 Key、禁用状态、历史审计和上游登录状态。备份后已轮换/撤销的 Key 可能被恢复：
恢复后必须核对撤销清单并重新禁用。完整 Key 无法从哈希找回，必要时重新轮换。

## 管理 API

全部在原管理鉴权组 `/v0/management/company`，使用 `Authorization: Bearer 管理密钥`：

| 方法 | 路径 | 功能 |
|---|---|---|
| GET / POST | `/users` | 列表 / 创建 `{id,name}`，创建返回一次性 `api_key` |
| PATCH | `/users/:id` | 修改 `{name?,enabled?}` |
| POST | `/users/:id/rotate` | 轮换，返回一次性 `api_key` |
| DELETE | `/users/:id` | 删除并撤销 |
| GET | `/audit?limit=100&user_id=` | 查询元数据，返回 `entries` 和 `healthy` |

## 本版验收记录

2026-09-22 在 macOS 本地完成：

- `go test ./...` 全仓库通过，`go build -o cli-proxy-api ./cmd/server` 编译通过。
- 新增公司模式测试通过 race 检查，涵盖员工生命周期、权限隔离、文件损坏、
  写入失败回滚、热加载不能改变认证边界、审计轮转及隐私字段。
- 真实 HTTP 执行链连接本地模拟上游，30 位员工并发，流式和非流式分别通过。
- API Key 上游返回 401 后切换到模拟 OAuth 凭据，最终成功及各次尝试均保留审计。
- 统一后台前端 717 项测试、lint、类型检查和单文件生产构建通过。
- Chrome 自动化验证一次登录、配置页员工生命周期、审计筛选/导出、
  供应商和 OAuth 跨页导航、退出与未登录访问拦截、旧地址跳转。
  桌面及手机截图已检查，390px 手机视口无页面级横向溢出。
- 实际重启本地网关进程，轮换 Key、停用状态、历史审计均保留。
- Compose 配置及备份脚本语法校验通过；备份加密、校验、解密和内容恢复通过
  临时文件测试，其中 Docker 停启使用模拟命令。

尚未完成生产验收：本机 Docker daemon 未启动，未运行 Linux 容器、
实际容器重启及恢复；未配置真实上游，OAuth 登录、刷新和真实账号失效
仍需在预发布验证。30 并发结果只证明本地网关测试链路，不代表供应商并发额度。
生产保持 `debug: false`；第三方插件和调试日志需另外检查其数据留存行为。
