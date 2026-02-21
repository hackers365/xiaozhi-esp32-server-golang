# 声纹识别功能文档

> 声纹识别（Speaker Identification）是 xiaozhi-esp32-server-golang 项目中的一项核心功能，用于识别设备端用户的身份，并根据识别结果动态切换 TTS 音色。

---

## 一、功能概述

声纹识别通过提取用户语音的声纹特征（embedding），与预先注册的声纹数据进行比对，实现用户身份识别。

### 核心能力

| 能力 | 说明 |
|------|------|
| 🎤 **声纹注册** | 上传用户音频样本，提取声纹特征并存储 |
| 🔍 **声纹识别** | 实时识别说话人身份 |
| ✅ **声纹验证** | 验证音频是否属于指定用户 |
| 📡 **流式识别** | 通过 WebSocket 进行实时流式声纹识别 |
| 🔊 **动态 TTS 切换** | 根据识别结果动态切换对应用户的 TTS 音色 |

---

## 二、系统架构

### 2.1 整体架构

```
┌──────────────────┐     ┌──────────────────────┐     ┌──────────────────┐
│   ESP32 设备     │────▶│ xiaozhi-esp32-server │────▶│   voice-server   │
│  (采集音频)      │     │     (主服务)          │     │ (声纹识别服务)   │
└──────────────────┘     └──────────────────────┘     └──────────────────┘
                                                              │
                                                              ▼
                                                      ┌──────────────────┐
                                                      │  Qdrant 向量库   │
                                                      │ (存储声纹特征)   │
                                                      └──────────────────┘
```

### 2.2 组件说明

| 组件 | 职责 |
|------|------|
| **xiaozhi-esp32-server** | 主服务，负责设备连接、会话管理、声纹识别结果处理 |
| **voice-server (asr_server)** | 声纹识别服务，负责特征提取、注册、识别、验证 |
| **Manager (后台管理)** | Web 管理后台，提供声纹组管理、样本管理的 API 和 UI |
| **Qdrant** | 向量数据库，存储声纹特征向量 |

---

## 三、完整流程描述

### 3.1 声纹注册流程

```
用户上传音频 → Manager API → voice-server 注册接口 → 提取 embedding → 存入 Qdrant
                  │
                  ▼
            保存到本地文件 + 数据库记录
```

**详细步骤：**

1. 用户在 Manager Web 界面上传音频文件（WAV 格式）
2. Manager 后端生成唯一 UUID，保存音频文件到本地存储
3. 调用 voice-server 的 `/api/v1/speaker/register` 接口
4. voice-server 使用 sherpa-onnx 模型提取声纹特征（192 维向量）
5. 声纹特征存入 Qdrant 向量数据库
6. Manager 创建 `SpeakerSample` 数据库记录

### 3.2 实时声纹识别流程

```
ESP32 采集音频 → VAD 检测语音 → 同时发送到 ASR 和声纹识别
                                        │
                                        ▼
                              WebSocket 流式识别
                                        │
                                        ▼
                              语音结束时获取识别结果
                                        │
                                        ▼
                              根据识别结果切换 TTS 音色
```

**详细步骤：**

1. **VAD 检测**：ESP32 采集的音频经过 VAD（Voice Activity Detection）检测
2. **双通道发送**：检测到语音时，音频数据同时发送到：
   - ASR 服务（语音转文字）
   - 声纹识别服务（WebSocket 流式识别）
3. **流式处理**：声纹识别服务持续接收音频块
4. **结果获取**：当检测到语音结束（静默）时，调用 `FinishAndIdentify` 获取识别结果
5. **TTS 切换**：根据识别结果，动态切换对应用户配置的 TTS 音色

### 3.3 启用条件

声纹识别需要同时满足以下条件才会启动：

- `voice_identify.enable = true`：全局配置中启用声纹识别
- 设备配置中存在声纹组配置
- `speakerManager` 已成功初始化

---

## 四、配置说明

### 4.1 配置边界（重要）

| 配置项 | 存放位置 | 主要用途 | 常用字段 |
|--------|----------|----------|----------|
| `voice_identify` | Manager 系统配置表（`type=voice_identify`），并下发给主服务 | 主服务实时声纹识别 | `enable`、`mode`、`base_url`、`threshold` |
| `speaker_service` | `manager/backend/config/config.json`（可被环境变量覆盖） | Manager 后台的声纹组/样本管理调用 | `mode`、`url` |

> 两套配置通常会指向同一个 voice-server，但它们不是同一个配置对象，职责不同，不应互相替代。

### 4.2 主服务运行时配置（voice_identify）

在 `config.yaml` 中添加以下配置：

```yaml
# 声纹识别配置
voice_identify:
  enable: true                              # 是否启用声纹识别
  mode: "http"                              # http / embed
  base_url: "http://voice-server:8080"      # mode=http 时的 voice-server 服务地址
  threshold: 0.6                            # 声纹识别阈值，范围 0.0-1.0
```

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enable` | bool | false | 是否启用声纹识别功能 |
| `mode` | string | `http` | `http` 走网络请求；`embed` 走进程内引擎调用 |
| `base_url` | string | - | `mode=http` 时使用的 voice-server HTTP 地址 |
| `threshold` | float | 0.6 | 识别阈值，值越高要求匹配越严格 |

### 4.3 Manager 后台配置（speaker_service）

在 `manager/backend/config/config.json` 中配置：

```json
{
  "speaker_service": {
    "mode": "http",
    "url": "http://voice-server:8080"
  }
}
```

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `mode` | string | `http` | Manager 调用声纹服务的模式：`http` / `embed` |
| `url` | string | - | `mode=http` 时的服务地址 |

### 4.4 Docker Compose 配置

#### Backend 服务环境变量

```yaml
backend:
  environment:
    - SPEAKER_SERVICE_URL=http://voice-server:8080
    - SPEAKER_SERVICE_MODE=http
```

说明：
- `SPEAKER_SERVICE_URL`：覆盖 Manager 的 `speaker_service.url`，并可用于系统配置中的 `voice_identify.base_url`
- `SPEAKER_SERVICE_MODE`：覆盖 Manager 的 `speaker_service.mode`，并可用于系统配置中的 `voice_identify.mode`

#### voice-server 服务环境变量

```yaml
voice-server:
  environment:
    - VAD_ASR_SPEAKER_ENABLED=true
    - VAD_ASR_SPEAKER_VECTOR_DB_HOST=qdrant
    - VAD_ASR_SPEAKER_VECTOR_DB_PORT=6334
    - VAD_ASR_SPEAKER_VECTOR_DB_COLLECTION_NAME=speaker_embeddings
    - VAD_ASR_SPEAKER_THRESHOLD=0.6
    - VAD_ASR_LOGGING_LEVEL=info
```

| 环境变量 | 说明 |
|----------|------|
| `VAD_ASR_SPEAKER_ENABLED` | 是否启用声纹识别功能 |
| `VAD_ASR_SPEAKER_VECTOR_DB_HOST` | Qdrant 服务地址 |
| `VAD_ASR_SPEAKER_VECTOR_DB_PORT` | Qdrant gRPC 端口 |
| `VAD_ASR_SPEAKER_VECTOR_DB_COLLECTION_NAME` | Qdrant Collection 名称 |
| `VAD_ASR_SPEAKER_THRESHOLD` | 声纹识别阈值 |
| `VAD_ASR_LOGGING_LEVEL` | 日志级别 |

---

## 五、API 接口说明

### 5.1 Manager 后台 API

#### 声纹组管理

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/speaker-groups` | 创建声纹组 |
| GET | `/api/speaker-groups` | 获取声纹组列表 |
| GET | `/api/speaker-groups/:id` | 获取声纹组详情 |
| PUT | `/api/speaker-groups/:id` | 更新声纹组 |
| DELETE | `/api/speaker-groups/:id` | 删除声纹组 |
| POST | `/api/speaker-groups/:id/verify` | 验证声纹 |

#### 声纹样本管理

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/speaker-groups/:id/samples` | 添加声纹样本 |
| GET | `/api/speaker-groups/:id/samples` | 获取样本列表 |
| GET | `/api/speaker-samples/:id/audio` | 获取样本音频文件 |
| DELETE | `/api/speaker-samples/:id` | 删除样本 |

### 5.2 voice-server API

#### HTTP 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/speaker/register` | 注册声纹 |
| POST | `/api/v1/speaker/identify` | 识别声纹 |
| POST | `/api/v1/speaker/verify` | 验证声纹 |
| GET | `/api/v1/speaker/list` | 获取所有说话人 |
| DELETE | `/api/v1/speaker/:id` | 删除说话人 |
| GET | `/api/v1/speaker/stats` | 获取统计信息 |

#### WebSocket 流式识别

**连接地址：** `ws://voice-server:8080/api/v1/speaker/stream`

**消息流程：**

1. 客户端发送音频块（PCM float32，小端序）
2. 客户端发送完成命令：`{"action": "finish"}`
3. 服务端返回识别结果

---

## 六、向量数据库（Qdrant）

### 6.1 数据存储结构

```json
{
    "uid": "用户 ID",
    "agent_id": "智能体 ID",
    "speaker_id": "说话人 ID（声纹组主键）",
    "speaker_name": "说话人名称（声纹组名称）",
    "uuid": "样本的唯一标识",
    "sample_index": 0,
    "created_at": 1704672000,
    "updated_at": 1704672000
}
```

### 6.2 向量配置

| 配置 | 值 |
|------|-----|
| 向量维度 | 192 |
| 距离度量 | Cosine（余弦相似度） |
| Collection 名称 | `speaker_embeddings`（可配置） |

### 6.3 数据隔离

支持多维度数据隔离：

- **UID**：用户级别隔离
- **Agent ID**：智能体级别隔离
- 同一用户的不同智能体可以有独立的声纹数据

---

## 七、数据库表结构

### 7.1 SpeakerGroup（声纹组表）

```sql
CREATE TABLE `speaker_groups` (
  `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
  `user_id` INT UNSIGNED NOT NULL COMMENT '所属用户ID',
  `agent_id` INT UNSIGNED NOT NULL COMMENT '关联的智能体ID',
  `name` VARCHAR(100) NOT NULL COMMENT '声纹名称',
  `prompt` TEXT COMMENT '角色提示词',
  `description` TEXT COMMENT '描述信息',
  `tts_config_id` VARCHAR(100) COMMENT 'TTS配置ID',
  `voice` VARCHAR(200) COMMENT '音色值',
  `status` VARCHAR(20) NOT NULL DEFAULT 'active',
  `sample_count` INT NOT NULL DEFAULT 0 COMMENT '样本数量',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
);
```

### 7.2 SpeakerSample（声纹样本表）

```sql
CREATE TABLE `speaker_samples` (
  `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
  `speaker_group_id` INT UNSIGNED NOT NULL COMMENT '关联的声纹组ID',
  `user_id` INT UNSIGNED NOT NULL COMMENT '所属用户ID',
  `uuid` VARCHAR(36) NOT NULL COMMENT 'UUID唯一标识',
  `file_path` VARCHAR(500) NOT NULL COMMENT '音频文件本地存储路径',
  `file_name` VARCHAR(255) COMMENT '原始文件名',
  `file_size` BIGINT COMMENT '文件大小（字节）',
  `duration` FLOAT COMMENT '音频时长（秒）',
  `status` VARCHAR(20) NOT NULL DEFAULT 'active',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE INDEX `idx_uuid` (`uuid`)
);
```

---

## 八、使用指南

### 8.1 部署 voice-server

参考 [docker_compose.md](docker_compose.md) 中的完整部署配置，确保以下服务已启动：

- **Qdrant**：向量数据库
- **voice-server**：声纹识别服务

### 8.2 配置主程序

在主程序的 `config.yaml` 中添加声纹识别配置：

```yaml
voice_identify:
  enable: true
  mode: "http"
  base_url: "http://voice-server:8080"
  threshold: 0.6
```

### 8.3 创建声纹组

1. 登录 Manager Web 控制台
2. 进入"智能体" → 选择目标智能体 → "声纹管理"
3. 点击"新建声纹组"，填写名称、描述等信息
4. 配置对应的 TTS 音色（可选）

### 8.4 上传声纹样本

1. 在声纹组详情页点击"添加样本"
2. 上传 WAV 格式的音频文件（建议 3-10 秒清晰语音）
3. 系统自动提取声纹特征并存储

### 8.5 测试声纹识别

1. 在声纹组详情页点击"验证"
2. 上传测试音频
3. 查看识别结果和置信度

---

## 九、关键技术点

### 9.1 声纹特征提取

- 使用 **sherpa-onnx** 模型提取声纹特征
- 输出 192 维的 embedding 向量
- 支持任意采样率输入，自动重采样

### 9.2 相似度计算

- 使用 **余弦相似度**（Cosine Similarity）计算声纹匹配度
- 相似度范围：[-1, 1]
- 默认阈值 0.6，可根据实际场景调整

### 9.3 VAD 预处理

- 使用 TEN-VAD 进行静音过滤
- 注册时保留前后 100ms 的静音边界
- 实时识别时仅发送语音活动检测到的音频段

---

## 十、常见问题

### Q1: 声纹识别不生效？

检查以下配置：
1. `voice_identify.enable` 是否为 `true`
2. `voice_identify.mode` 是否配置为 `http` 或 `embed`
3. 当 `mode=http` 时，`voice_identify.base_url` 是否正确
4. 设备是否已配置声纹组
5. voice-server 服务是否正常运行（`mode=http` 时）

### Q2: 识别准确率低？

- 提高声纹样本质量（清晰、无噪音、3-10秒）
- 增加声纹样本数量（建议 3-5 个样本）
- 调整识别阈值

### Q3: TTS 音色未切换？

检查声纹组配置中的 `tts_config_id` 或 `voice` 字段是否正确配置。

---

## 十一、相关文档

- [Docker Compose 部署](docker_compose.md)
- [配置文档](config.md)
- [视觉识别](vision.md)
