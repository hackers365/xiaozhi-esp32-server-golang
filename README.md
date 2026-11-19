# 🚀 xiaozhi-esp32-server-golang

> **Xiaozhi AI Backend for ESP32 Devices**

---

## 项目简介 | Project Overview

xiaozhi-esp32-server-golang 是一款高性能、全流式的 AI 后端服务，专为物联网与智能语音场景设计。项目基于 Go 语言开发，集成了 ASR（自动语音识别）、LLM（大语言模型）、TTS（语音合成）等核心能力，支持大规模并发与多协议接入，助力智能终端与边缘设备的 AI 语音交互。

---

## ✨ 主要特性 | Key Features

- ⚡ **端到端全流式 AI 语音链路**：ASR → LLM → TTS 全流程流式处理，低延迟实时交互
- 🎙️ **声纹识别与动态TTS切换**：根据说话人身份自动切换TTS音色，个性化语音体验
- 🔌 **Transport 接口层抽象**：WebSocket / MQTT UDP 统一抽象，灵活注入主逻辑，便于协议扩展
- 📬 **消息队列化处理**：LLM 与 TTS 采用消息队列异步处理，支持业务逻辑灵活注入
- 🌐 **多协议高并发接入**：支持大规模设备并发接入与消息推送
- ♻️ **高效资源池与连接复用**：外部资源连接池机制，降低响应耗时，提升系统吞吐
- 🤖 **多引擎AI能力集成**：基于 Eino 框架，支持 FunASR、OpenAI 兼容、Ollama、Doubao、EdgeTTS、CosyVoice 等多种引擎
- 🧩 **模块化可扩展架构**：VAD/ASR/LLM/TTS/MCP/视觉等核心模块独立可插拔
- 🎵 **MCP Audio Server**：音频资源分页获取与流式处理，音乐播放与音量控制
- 🦞 **OpenClaw 智能体接入**：按智能体生成专属 OpenClaw Endpoint，支持连接状态查看、会话测试、进入/退出关键词路由（默认“打开龙虾/进入龙虾”与“关闭龙虾/退出龙虾”）
- 🖥️ **全功能Web管理控制台**：可视化配置向导、VAD/ASR/LLM/TTS全链路可用性测试、设备管理与消息注入、实时延迟监控与OTA验证
- 🧠 **高级业务功能**：MCP 市场聚合与导入、声音复刻、知识库（Dify/RAGFlow/WeKnora）、设备/智能体维度 MCP 远程调用调试
- 📦 **易用的一键部署方案**：预编译 aio 包开箱即用（主程序+控制台+声纹服务）、Docker 一键部署、支持 Linux/Windows/macOS 本地编译
- 🔐 **安全与权限体系**（规划中）：预留用户认证与权限管理接口

---

[deepwiki 架构分析](https://deepwiki.com/hackers365/xiaozhi-esp32-server-golang)

## 🚀 快速开始 | Quick Start

### 方式一：一键启动包（推荐）

下载对应平台的压缩包，解压后运行即可：

- **Release 页面**：<https://github.com/hackers365/xiaozhi-esp32-server-golang/releases>
- **使用教程**：[doc/quickstart_bundle_tutorial.md](doc/quickstart_bundle_tutorial.md)

单机可直接启动，未配置时会自动生成进程内 JWT 密钥，重启后需要重新登录。需要跨重启或多实例共享登录时，请参阅[启动包密钥配置](doc/quickstart_bundle_tutorial.md#jwt-密钥)。

启动后访问 **http://<服务器IP或域名>:8080** 进入 Web 控制台进行配置。

### 方式二：Docker 部署

- [Docker Compose（带控制台）](doc/docker_compose.md)
- [Docker（无控制台）](doc/docker.md)

### 方式三：本地编译

适用于开发环境或需要定制编译的场景。

**安装依赖**（以 Ubuntu 为例）

```bash
# Go 1.20+
# Opus 编解码
sudo apt-get install -y pkg-config libopus0 libopusfile-dev

# ONNX Runtime（1.21.0）
wget https://github.com/microsoft/onnxruntime/releases/download/v1.21.0/onnxruntime-linux-x64-1.21.0.tgz
tar -xzf onnxruntime-linux-x64-1.21.0.tgz
sudo cp -r onnxruntime-linux-x64-1.21.0/include/* /usr/local/include/onnxruntime/
sudo cp -r onnxruntime-linux-x64-1.21.0/lib/* /usr/local/lib/
sudo ldconfig

# ten_vad 运行时依赖
sudo apt install -y libc++1 libc++abi1
```

> 📖 完整依赖说明与 Windows/macOS 配置请参考 [config.md](doc/config.md)

主程序、控制台前后端、声纹服务的分离编译与 AIO 打包流程请参考 [doc/compile_deploy.md](doc/compile_deploy.md)

参考 [FunASR 官方文档](https://github.com/modelscope/FunASR/blob/main/runtime/docs/SDK_advanced_guide_online_zh.md) 部署。

**编译与启动**

```bash
# 编译
go build -o xiaozhi_server ./cmd/server/

# 启动（配置文件详见 config/config.yaml）
./xiaozhi_server -c config/config.yaml
```

---

## 📚 文档导航 | Docs

### 部署相关
- [一键启动包教程](doc/quickstart_bundle_tutorial.md)
- [Docker Compose 部署](doc/docker_compose.md)
- [Docker 部署](doc/docker.md)
- [编译与部署指南](doc/compile_deploy.md)
- [配置详解](doc/config.md)

### 使用指南
- [管理后台使用指南](doc/manager_console_guide.md)
- [WebSocket 服务与 OTA 配置](doc/websocket_server.md)
- [MQTT + UDP 配置](doc/mqtt_udp.md)
- [MQTT UDP 协议](doc/mqtt_udp_protocol.md)

### 功能模块
- [视觉能力](doc/vision.md)
- [声纹识别](doc/speaker_identification.md)
- [MCP 架构](doc/mcp.md)
- [MCP 音频资源](doc/mcp_resource.md)
- [MCP 市场（市场发现/导入/热更新）](doc/mcp_market.md)
- [OpenClaw 智能体接入（Endpoint/关键词路由/会话测试）](doc/openclaw_integration.md)
- [声音复刻（用户操作与管理员额度）](doc/voice_clone.md)
- [知识库（Provider 配置/同步/召回测试/RAG）](doc/knowledge_base.md)
- [设备/智能体维度 MCP 远程调用（Endpoint/Tools/Call）](doc/mcp_remote_call_agent_device.md)

### 设备接入
- [ESP32 端接入指南](doc/esp32_xiaozhi_backend_guide.md)
- [OTA MQTT 授权说明](doc/ota_mqtt_auth.md)

---

## 🧩 模块架构 | Module Overview

| 模块 | 功能简介 | 技术栈 |
|------|----------|--------|
| VAD | 语音活动检测 | Silero VAD / WebRTC VAD / ten_vad |
| ASR | 语音识别 | FunASR / Doubao ASR |
| LLM | 大模型推理 | Eino 框架兼容、OpenAI、Ollama 等 |
| TTS | 语音合成 | Doubao / EdgeTTS / CosyVoice |
| MCP | 多协议接入、MCP 市场发现导入、设备/智能体维度远程调用调试 | MCP Server / 接入点 / MCP Market / SSE / StreamableHTTP / WebSocket Controller / MCP Tool Call |
| OpenClaw | 智能体维度接入点、进入/退出关键词模式切换、会话消息转发与测试 | OpenClaw WebSocket / Agent Endpoint / Chat Router |
| 视觉 | 视觉处理 | Doubao / 阿里云视觉 |
| 声纹识别 | 说话人识别 | sherpa-onnx + 向量数据库 |
| 声音复刻 | 用户侧复刻音色创建与试听 | Minimax / CosyVoice / 千问 |
| 知识库（RAG） | 文档同步、召回测试与对话检索 | Dify / RAGFlow / WeKnora |

---

## 📈 性能与测试 | Performance & Testing

- [延迟测试报告](doc/delay_test.md)
- 管理后台提供 VAD/ASR/LLM/TTS 可用性与延迟测试入口

---

## 🛠️ 规划中 | Roadmap

- 主动式ai

---

## 🤝 贡献 | Contributing

欢迎提交 Issue、PR 或建议！

---

## 📄 License

MIT License

---

## 📬 联系方式 | Contact

**个人微信**：hackers365 （加微信拉你进交流群）

![个人微信](https://github.com/user-attachments/assets/6b8d3d11-7bf5-4fa4-a73e-5109019dab85)


**开源不易，您的赞助会让项目持续更新**

<img width="250" height="250" alt="eab0f4d3d8b6f977863a7bef36e3d64b" src="https://github.com/user-attachments/assets/9a949cb3-d788-446b-a0b9-8542edbb0842" />




---

> © 2024 xiaozhi-esp32-server-golang
