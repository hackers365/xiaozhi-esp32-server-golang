# 小智服务 macOS 使用说明

欢迎使用小智服务 macOS aio 包。本文档包含依赖安装、启动和配置说明。

## 目录结构

```
xiaozhi_server-macos-<arch>-<version>/
├── xiaozhi_server              # 主程序
├── ten-vad/
│   └── lib/macOS/
│       ├── ten_vad.framework/  # VAD 框架
│       ├── libonnxruntime.*.dylib
│       └── libsherpa-onnx-*.dylib
├── main_config.yaml            # 主配置文件
├── manager.json                # 管理后台配置
├── asr_server.json             # ASR 服务配置
├── models/                     # 模型文件目录
├── data/                       # 数据目录
└── logs/                       # 日志目录
```

> **注意**：macOS 版本分为 **amd64** (Intel) 和 **arm64** (Apple Silicon)，请下载与您的 Mac 匹配的版本。

## 运行依赖

### 系统要求

- **macOS 版本**：macOS 11 (Big Sur) 或更高版本
- **架构**：Intel (x86_64) 或 Apple Silicon (arm64)

### 安装依赖

使用 Homebrew 安装必要的依赖：

```bash
# 安装 Homebrew（如果尚未安装）
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# 安装依赖
brew install pkg-config
```

## 快速启动

```bash
# 添加执行权限
chmod +x xiaozhi_server

# 如果这是你自己构建的发布包，先修正 rpath
./build/macos/fix_rpath.sh ./xiaozhi_server

# 启动服务
./xiaozhi_server
```

说明：

- 官方发布包如果已经完成打包，一般不需要再次执行 `fix_rpath.sh`
- 只有你在源码仓库里自行构建 macOS 分发包时，才需要补这一步
- 这一步会把二进制里的开发机绝对路径 `rpath` 改成 `@executable_path/ten-vad/lib/macOS`

### 首次运行安全提示

首次运行时，macOS 可能会弹出安全提示，因为程序未经过 Apple 认证。请：

1. 打开「系统设置」→「隐私与安全性」
2. 找到关于 `xiaozhi_server` 的提示
3. 点击「仍要打开」或「允许」

或使用以下命令解除隔离：

```bash
xattr -cr xiaozhi_server
```

## 端口与服务

| 端口 | 配置来源 | 说明 |
|------|----------|------|
| **8080** | `manager.json` → `server.port` | **管理后台**：Web 控制台 + HTTP API |
| **8989** | `main_config.yaml` → `websocket.port` | **主服务 WebSocket**：设备/客户端连接 |
| **9000** | `asr_server.json` → `server.port` | **ASR/声纹服务**：语音识别内部接口 |
| **2883** | 控制台配置 | **MQTT 服务**：设备 MQTT 连接 |
| **8990** | 控制台配置 | **UDP 服务**：设备 UDP 通信 |
| **6060** | 控制台配置 | **pprof**：性能分析（默认关闭） |

## 访问地址

### 管理后台

- **本地访问**：`http://localhost:8080/`
- **局域网访问**：`http://<本机IP>:8080/`

### 设备/客户端连接

- **WebSocket**：`ws://<服务器IP>:8989/`
- **MQTT**：`<服务器IP>:2883`
- **UDP**：`<服务器IP>:8990`

## 修改配置

### 需在配置文件中修改的端口

以下端口修改后需重启服务生效：

| 端口 | 配置文件 | 配置项 |
|------|----------|--------|
| 8080 | `manager.json` | `server.port` |
| 8989 | `main_config.yaml` | `websocket.port` |
| 9000 | `asr_server.json` | `server.port` |

### 控制台配置

以下端口及所有其他配置通过管理后台控制台进行变更：

- **端口配置**：MQTT (2883)、UDP (8990)、pprof (6060)
- **功能配置**：LLM、TTS、ASR、声纹识别等
- 访问 `http://localhost:8080/` 进入管理后台
- 配置变更实时生效，无需重启服务

## 后台运行

### 使用 nohup

```bash
nohup ./xiaozhi_server > logs/output.log 2>&1 &
```

### 创建 launchd 服务（推荐）

创建 `~/Library/LaunchAgents/com.xiaozhi.server.plist`：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.xiaozhi.server</string>
    <key>ProgramArguments</key>
    <array>
        <string>/path/to/xiaozhi_server</string>
    </array>
    <key>WorkingDirectory</key>
    <string>/path/to/xiaozhi_server-macos-<arch>-<version></string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/path/to/logs/output.log</string>
    <key>StandardErrorPath</key>
    <string>/path/to/logs/error.log</string>
</dict>
</plist>
```

加载服务：

```bash
# 加载服务
launchctl load ~/Library/LaunchAgents/com.xiaozhi.server.plist

# 启动服务
launchctl start com.xiaozhi.server

# 查看状态
launchctl list | grep xiaozhi

# 停止服务
launchctl stop com.xiaozhi.server

# 卸载服务
launchctl unload ~/Library/LaunchAgents/com.xiaozhi.server.plist
```

## 防火墙配置

如果启用了防火墙，需要允许 `xiaozhi_server` 接受入站连接：

1. 打开「系统设置」→「网络」→「防火墙」
2. 点击「选项」
3. 找到 `xiaozhi_server`，设置为「允许入站连接」

或在终端中使用命令：

```bash
# 添加防火墙例外（需要 sudo）
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --add /path/to/xiaozhi_server
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --unblock /path/to/xiaozhi_server
```

## 常见问题

### 安全提示"已损坏"

如果提示应用已损坏，运行以下命令：

```bash
xattr -cr xiaozhi_server
```

### 动态库加载失败

如果出现 `dylib` 加载失败，检查：

```bash
# 查看依赖
otool -L xiaozhi_server

# 查看 rpath
otool -l xiaozhi_server | grep -A2 LC_RPATH

# 确保动态库在正确位置
ls -la ten-vad/lib/macOS/
```

如果 `LC_RPATH` 仍然是开发机源码绝对路径，而不是 `@executable_path/ten-vad/lib/macOS`，请执行：

```bash
./build/macos/fix_rpath.sh ./xiaozhi_server
```

如果你是在 IDE 临时目录调试，或手动移动了二进制导致目录结构不一致，可临时使用：

```bash
DYLD_FRAMEWORK_PATH="$PWD/ten-vad/lib/macOS" ./xiaozhi_server
```

### 端口被占用

```bash
# 查看端口占用
lsof -i :端口号

# 结束占用进程或修改配置文件中的端口
```

### Apple Silicon (M1/M2/M3) 运行 Intel 版本

在 Apple Silicon Mac 上运行 Intel 版本需要 Rosetta 2：

```bash
# 安装 Rosetta 2
softwareupdate --install-rosetta
```

但建议下载对应的 arm64 版本以获得最佳性能。
