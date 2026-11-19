# Docker Compose 部署指南

## 概述

本项目使用 Docker Compose 进行容器化部署，包含以下核心服务：

- **MySQL 数据库服务**：数据存储
- **主程序服务**：核心业务逻辑
- **后端管理服务**：API 接口服务
- **前端管理服务**：Web 管理界面

## 快速指引（补充）

本节为 `doc/docker.md` 的补充说明，帮助快速选择与落地部署方式。

### 1. 选择部署方式

- 推荐：Docker Compose（包含管理后台与完整服务）
- 简化：单容器 Docker（无控制台或精简模式）

### 2. Docker Compose 快速路径

1) 拉取代码或准备 `docker-compose.yml`
2) 参考本文后续“配置文件准备”与“启动服务”完成配置
3) 启动：

```bash
docker compose up -d
```

4) 管理后台默认地址：`http://<服务器IP或域名>:8080/`

### 3. 单容器 Docker（补充）

按 `doc/docker.md` 构建或拉取镜像后运行。常见建议：

- 映射 `config/`、`logs/`、`storage/` 目录为数据卷
- 对外暴露 WebSocket / MQTT / UDP 端口
- 需要管理后台时启用对应参数或使用 Compose

### 4. 配置向导与测试

启动后可在管理后台使用配置向导完成引擎配置，并在测试工具中进行 VAD/ASR/LLM/TTS 可用性与延迟测试，以及 OTA 全流程验证。

### 5. 常见问题

- 端口冲突：检查 8080/8989/2883/8990 占用情况
- 配置未生效：确认数据卷挂载路径正确，重启容器生效
- 权限问题：Linux 下注意挂载目录权限与 SELinux 限制

## 服务架构

### 1. MySQL 数据库服务 (xiaozhi-mysql)

**配置信息：**
- 镜像：`docker.jsdelivr.fyi/mysql:8.0`
- 端口映射：`23306:3306`
- 数据库名：`xiaozhi_admin`
- 用户名：`root`
- 密码：`password`

**特性：**
- 使用 MySQL 8.0
- 配置健康检查
- 数据持久化

### 2. 主程序服务 (xiaozhi-main-server)

**配置信息：**
- 镜像：`docker.jsdelivr.fyi/hackers365/xiaozhi_server:0.5`
- 端口映射：
  - `8989:8989` - WebSocket 服务
  - `2882:2883` - MQTT 服务
  - `8888:8888/udp` - UDP 服务

**依赖关系：**
- 依赖 MySQL 服务健康状态
- 依赖后端服务启动完成

**配置文件支持：**
- 通过卷挂载导入自定义配置文件
- 配置文件路径：`../../config:/workspace/config`

**ten_vad 支持：**
- Docker 镜像已包含 ten_vad 库（`/workspace/lib/ten-vad/`）
- 运行时库路径已通过 `LD_LIBRARY_PATH` 自动配置

### 3. 后端管理服务 (xiaozhi-backend)

**配置信息：**
- 镜像：`docker.jsdelivr.fyi/hackers365/xiaozhi_manager_backend:0.5`
- 端口映射：`8081:8080`

**功能：**
- 提供 RESTful API
- 设备与用户管理

**配置文件支持：**
- 通过卷挂载导入自定义配置文件
- 配置文件路径：`../../manager/backend/config:/root/config`

### 4. 前端管理服务 (xiaozhi-frontend)

**配置信息：**
- 镜像：`docker.jsdelivr.fyi/hackers365/xiaozhi_manager_frontend:0.5`
- 端口映射：`8080:80`

**功能：**
- Web 管理界面（内控入口）
- 设备状态与系统配置管理

## 部署流程

### 1. 环境准备

确保系统已安装 Docker 和 Docker Compose：

```bash
docker --version
docker compose version
```

### 2. 配置文件准备

确保以下目录与文件存在：

```
xiaozhi-esp32-server-golang/
├─ docker/docker-composer/
│  └─ docker-compose.yml
├─ config/
│  ├─ config.yaml
│  ├─ config.json
│  └─ (其他配置文件)
├─ logs/
│  └─ (日志目录)
└─ manager/backend/config/
   ├─ config.yaml
   └─ (其他配置文件)
```

**配置文件导入说明：**
- 主程序配置文件通过卷挂载 `../../config:/workspace/config` 导入
- 后端配置文件通过卷挂载 `../../manager/backend/config:/root/config` 导入

Manager 未配置密钥时会生成进程内随机密钥，容器重启后需要重新登录。若需跨重启或多副本共享登录，请在 Compose 的 `.env` 中设置 `JWT_SECRET`，或在挂载的后端配置中设置 `jwt.secret`。显式密钥至少 32 字节，应独立随机生成（例如 `openssl rand -hex 32`）。升级时请清空或替换旧默认密钥。

### 3. 启动服务

**必须先进入 `docker/docker-composer/` 目录执行命令：**

```bash
cd docker/docker-composer/
docker compose up -d

docker compose ps
docker compose logs -f
```

### 4. 服务访问

- 前端管理界面：`http://<服务器IP或域名>:8080`
- 后端 API：`http://localhost:8081`
- WebSocket：`ws://localhost:8989`
- MQTT：`localhost:2882`
- UDP：`localhost:8888`
- MySQL：`localhost:23306`

## 常用操作

```bash
cd docker/docker-composer/

docker compose ps

docker compose logs

docker compose logs -f main-server

docker compose restart

docker compose down

docker compose down -v

docker compose pull

docker compose up -d
```

## 网络配置

项目使用自定义网络 `xiaozhi-network`：

- MySQL：`mysql:3306`
- 后端：`backend:8080`
- 前端：`frontend:80`
- 主程序：`main-server:8989`（WebSocket）/ `main-server:2883`（MQTT）/ `main-server:8888`（UDP）

**端口映射汇总：**
- 8080 → 前端管理界面
- 8081 → 后端 API
- 8989 → WebSocket
- 2882 → MQTT
- 8888 → UDP
- 23306 → MySQL

## 数据持久化

### MySQL 数据

通过 Docker 卷 `mysql_data` 持久化，容器重启不丢失数据。

### 配置文件

- 主程序配置：`../../config:/workspace/config`
- 后端配置：`../../manager/backend/config:/root/config`

修改配置后重启对应服务生效：

```bash
cd docker/docker-composer/
docker compose restart main-server

docker compose restart backend
```

### 日志文件

- 主程序日志：`../../logs:/workspace/logs`

## 配置文件导入方法

### 1. 主程序配置

**位置：**
```
xiaozhi-esp32-server-golang/config/
├─ config.yaml
├─ config.json
├─ mqtt_config.json
└─ (其他配置文件)
```

**导入：**
1) 将配置文件放入 `config/`
2) 启动后自动挂载到容器 `/workspace/config/`
3) 修改后重启主程序服务：

```bash
cd docker/docker-composer/
docker compose restart main-server
```

### 2. 后端管理配置

**位置：**
```
xiaozhi-esp32-server-golang/manager/backend/config/
├─ config.yaml
└─ (其他配置文件)
```

**导入：**
1) 将配置文件放入 `manager/backend/config/`
2) 启动后自动挂载到容器 `/root/config/`
3) 修改后重启后端服务：

```bash
cd docker/docker-composer/
docker compose restart backend
```

### 3. ten_vad 库文件

**说明：**
- Docker 镜像已包含 ten_vad 库（`/workspace/lib/ten-vad/`）
- 运行时库路径已通过 `LD_LIBRARY_PATH` 自动配置
- 使用 ten_vad 无需额外挂载

## 健康检查

MySQL 服务配置了健康检查：

```yaml
healthcheck:
  test: ["CMD", "mysqladmin", "ping", "-h", "localhost", "-u", "root", "-ppassword"]
  timeout: 20s
  retries: 10
  interval: 10s
  start_period: 30s
```

## 故障排除

### 1. 服务启动失败

```bash
cd docker/docker-composer/

docker compose logs [服务名]

# 端口占用检查（Linux）
netstat -tulpn | grep [端口]
```

### 2. 数据库连接失败

```bash
cd docker/docker-composer/

docker compose ps mysql

docker compose logs mysql

docker compose exec mysql mysql -u root -ppassword
```

### 3. 网络连接问题

```bash
cd docker/docker-composer/

docker network ls
docker network inspect xiaozhi-network

docker compose exec main-server ping mysql
```

## 性能优化建议

1) 生产环境为各服务设置资源限制
2) 配置日志轮转，避免日志过大
3) 定期备份 MySQL 数据
4) 集成监控系统

## 安全注意事项

1) 生产环境修改默认数据库密码
2) 按需暴露端口
3) 配置防火墙与访问控制
4) 使用可信镜像源

---

## 下一步

### 访问管理后台

服务启动后，访问 http://<服务器IP或域名>:8080 进入管理后台。

**[管理后台使用指南 →](manager_console_guide.md)**

### 配置 ESP32 设备

参考 [ESP32端接入指南](esp32_xiaozhi_backend_guide.md) 完成设备接入。
