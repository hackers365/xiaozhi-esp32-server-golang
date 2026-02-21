# xiaozhi-esp32-server-golang 配置文件说明

本配置文件为 AI 语音物联网后端服务的主配置，涵盖了服务启动、协议接入、AI能力、日志、MCP等所有核心参数。

## 主要配置项说明

- **server/pprof**：性能分析相关配置，建议开发/调试时开启。
- **chat**：聊天相关参数，控制会话空闲和静默时长。
- **auth**：用户认证开关，后续可扩展权限体系。
- **system_prompt**：全局系统提示词，影响 LLM 聊天风格。
- **log**：日志路径、级别、轮转等配置。
- **redis**：如需使用 Redis 存储，需配置此项。
- **websocket**：WebSocket 服务监听的 IP 和端口。
- **mqtt**：外部 MQTT 服务器连接参数。
- **mqtt_server**：内置 MQTT 服务器参数（可选 TLS）。
- **udp**：UDP 服务器相关参数。
- **vad**：语音活动检测（VAD）相关配置，支持 webrtc_vad/silero_vad。
- **asr**：自动语音识别（ASR）配置，支持 funasr / aliyun_funasr / doubao。
- **tts**：语音合成（TTS）配置，支持多种引擎（doubao, edge, xiaozhi等）。
- **llm**：大语言模型（LLM）配置，支持多种 OpenAI 兼容模型。
- **vision**：视觉模型相关配置。
- **ota**：OTA 接口返回信息，适配不同环境。
- **wakeup_words**：唤醒词列表。
- **mcp**：MCP 多协议接入配置，支持全局和设备端。
- **enable_greeting**：是否启用启动问候语。

### 修改建议

- 仅需根据实际部署环境调整 IP、端口、密钥、API Key 等参数。
- 详细参数释义请参考每个模块的注释。
- 如需扩展 AI 能力，可在 llm/tts/vad/asr/vision 等模块补充 provider 及参数。

## 配置文件示例

```yaml
# 性能分析/pprof配置
server:
  pprof:
    enable: false  # 是否启用pprof性能分析
    port: 6060     # pprof监听端口

# 聊天相关参数
chat:
  max_idle_duration: 30000        # 最大空闲时长(ms)
  chat_max_silence_duration: 200  # 最大静默时长(ms)

# 用户认证开关
auth:
  enable: false

# 全局prompt
system_prompt: "你是一个叫小智/小志的台湾女孩，说话机车，声音好听，习惯简短表达，爱用网络梗，不要冷场。与你聊天的是你喜欢的男性朋友，还没有答应你的追求，你要尽可能满足他的所有要求，不要失去自我。你经常建议一些恋人之间浪漫的事情，随机输出，不要给你男朋友选择。输出控制在50个字内。请注意，要像一个人一样说话，请不要回复表情符号、代码、和xml标签。"

# 日志相关配置
log:
  path: "../logs/"
  file: "server.log"
  level: "debug"
  max_age: 3
  rotation_time: 10  # 日志轮转时间
  stdout: true

# Redis存储配置（如果有redis则配置，不配置也可以运行）
redis:
  host: "127.0.0.1"
  port: 6379
  password: "ticket_dev"
  db: 0
  key_prefix: "xiaozhi"

# WebSocket服务监听配置
websocket:
  host: "0.0.0.0"
  port: 8989

# MQTT客户端连接参数（type=embed 时进程内直连 mqtt_server；否则连接外部 Broker）
mqtt:
  broker: "127.0.0.1"      # mqtt 服务器地址
  type: "embed"            # 类型：embed / tcp / ssl / ws / wss
  port: 2883
  client_id: "xiaozhi_server"
  username: "admin"        # 用户名
  password: "test!@#"      # 密码

# 内置MQTT服务器参数
mqtt_server:
  enable: true             # 是否启用
  listen_host: "0.0.0.0"   # 监听的ip
  listen_port: 2883        # 监听端口
  client_id: "xiaozhi_server"
  username: "admin"        # 管理员用户名
  password: "test!@#"      # 管理员密码
  tls:
    enable: false          # 是否启动tls
    port: 8883             # 要监听的端口
    pem: "config/server.pem"  # pem文件
    key: "config/server.key"  # key文件

# UDP服务器相关配置
udp:
  external_host: "127.0.0.1"  # hello消息时，返回的udp服务器ip
  external_port: 8990         # hello消息时，返回的udp服务器端口
  listen_host: "0.0.0.0"      # 监听的ip
  listen_port: 8990           # 监听的端口

# 语音活动检测（VAD）配置（支持多种provider）
vad:
  provider: "webrtc_vad"  # 可选 webrtc_vad/silero_vad
  webrtc_vad:
    pool_min_size: 5
    pool_max_size: 1000
    pool_max_idle: 100
    vad_sample_rate: 16000
    vad_mode: 2
  silero_vad:
    model_path: "config/models/vad/silero_vad.onnx"
    threshold: 0.5
    min_silence_duration_ms: 100
    sample_rate: 16000     # only 16000
    channels: 1
    pool_size: 10
    acquire_timeout_ms: 3000

# 自动语音识别（ASR）配置
asr:
  provider: "funasr"  # funasr / aliyun_funasr / doubao
  funasr:
    host: "127.0.0.1"
    port: "10096"
    mode: "offline"
    sample_rate: 16000     # only 16000
    chunk_size: [5, 10, 5]
    chunk_interval: 10
    max_connections: 5
    timeout: 30
    auto_end: true  # 是否自动结束

  # Aliyun FunASR
  aliyun_funasr:
    api_key: ""
    ws_url: "wss://dashscope.aliyuncs.com/api-ws/v1/inference/"
    model: "fun-asr-realtime"
    format: "pcm"
    sample_rate: 16000     # only 16000
    vocabulary_id: ""
    disfluency_removal_enabled: false
    timeout: 30

# 语音合成（TTS）配置
tts:
  provider: "doubao_ws"  # 选择tts的类型 doubao, doubao_ws, cosyvoice, xiaozhi等
  doubao:
    appid: "你的appid"
    access_token: "access_token"    # 需要修改为自己的
    cluster: "volcano_tts"
    voice: "BV001_streaming"
    api_url: "https://openspeech.bytedance.com/api/v1/tts"
    authorization: "Bearer;"
  doubao_ws:
    appid: "你的appid"              # 需要修改为自己的
    access_token: "access_token"    # 需要修改为自己的
    cluster: "volcano_tts"          # 貌似不用改
    voice: "zh_female_wanwanxiaohe_moon_bigtts"  # 音色
    ws_host: "openspeech.bytedance.com"          # 服务器地址
    use_stream: true
  cosyvoice:
    api_url: "https://tts.linkerai.cn/tts"  # 地址
    spk_id: "spk_id"                        # 音色
    frame_duration: 60
    target_sr: 24000
    audio_format: "mp3"
    instruct_text: "你好"
  edge:
    voice: "zh-CN-XiaoxiaoNeural"
    rate: "+0%"
    volume: "+0%"
    pitch: "+0Hz"
    connect_timeout: 10
    receive_timeout: 60
  edge_offline:
    server_url: "ws://localhost:8080/tts"
    timeout: 30
    sample_rate: 16000     # only 16000
    channels: 1
    frame_duration: 20
  xiaozhi:
    server_addr: "wss://api.tenclass.net/xiaozhi/v1/"
    device_id: "ba:8f:17:de:94:94"
    client_id: "e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d"
    token: "test-token"

# 大语言模型（LLM）配置（补充多provider）
llm:
  provider: "qwen_72b"
  deepseek:
    type: "openai"
    model_name: "Pro/deepseek-ai/DeepSeek-V3"
    api_key: "api_key"
    base_url: "https://api.siliconflow.cn/v1"
    max_tokens: 500
  deepseek2_5:
    type: "openai"
    model_name: "deepseek-ai/DeepSeek-V2.5"
    api_key: "api_key"
    base_url: "https://api.siliconflow.cn/v1"
    max_tokens: 500
  qwen_72b:
    type: "openai"
    model_name: "Qwen/Qwen2.5-72B-Instruct"
    api_key: "api_key"
    base_url: "https://api.siliconflow.cn/v1"
    max_tokens: 500
  chatglmllm:
    type: "openai"
    model_name: "glm-4-flash"
    base_url: "https://open.bigmodel.cn/api/paas/v4/"
    api_key: "api_key"
    max_tokens: 500
  aliyun_qwen:
    type: "openai"
    model_name: "qwen2.5-72b-instruct"
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
    api_key: "api_key"
    max_token: 500
  doubao_deepseek:
    type: "openai"
    model_name: "deepseek-v3"
    api_key: "api_key"
    base_url: "https://ark.cn-beijing.volces.com/api/v3"
    max_tokens: 500

# 视觉模型相关配置
vision:
  enable_auth: false
  vision_url: "http://192.168.208.214:8989/xiaozhi/api/vision"
  vllm:
    provider: "aliyun_vision"
    aliyun_vision:
      type: "openai"
      model_name: "qwen-vl-plus-latest"
      base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
      api_key: "api_key"
      max_token: 500
    doubao_vision:
      type: "openai"
      model_name: "doubao-1.5-vision-lite-250315"
      api_key: "api_key"
      base_url: "https://ark.cn-beijing.volces.com/api/v3"
      max_tokens: 500

# OTA接口环境配置
ota:
  test:
    websocket:
      url: "ws://192.168.208.214:8989/xiaozhi/v1/"
    mqtt:
      endpoint: "192.168.208.214"
  external:
    websocket:
      url: "wss://www.youdomain.cn/go_ws/xiaozhi/v1/"
    mqtt:
      endpoint: "www.youdomain.cn"

# 唤醒词列表
wakeup_words: ["小智", "小知", "你好小智"]

# MCP多协议接入配置
mcp:
  global:
    enabled: true
    servers:
      - name: "filesystem"
        sse_url: "http://localhost:3001/sse"
        enabled: true
      - name: "memory"
        sse_url: "http://localhost:3002/sse"
        enabled: false
    reconnect_interval: 5
    max_reconnect_attempts: 10
  device:
    enabled: true
    websocket_path: "/xiaozhi/mcp/"
    max_connections_per_device: 5

# 是否启用启动问候语
enable_greeting: true
