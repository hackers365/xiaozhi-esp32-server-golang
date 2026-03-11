# 一键启动包部署教程

## 下载

访问 [Release 页面](https://github.com/hackers365/xiaozhi-esp32-server-golang/releases) 下载对应平台：

| 平台 | 文件名 |
|-----|-------|
| Windows | `xiaozhi-server-windows-xxx.zip` |
| Linux | `xiaozhi-server-linux-xxx.tar.gz` |
| macOS | `xiaozhi-server-macos-xxx.tar.gz` |

---

## 解压与目录结构

解压后目录结构：

```
xiaozhi-aio/
├── xiaozhi_server          # 主程序
├── config/                 # 配置文件目录
├── models/                 # 模型文件目录（如使用本地ASR/TTS）
└── data/                   # 数据目录
```

---

## 启动服务

### Windows
双击 `start.bat`

### Linux
```bash
# ten_vad 运行时依赖
sudo apt install -y libc++1 libc++abi1

chmod +x xiaozhi_server
LD_LIBRARY_PATH="$PWD/ten-vad/lib/Linux/x64:${LD_LIBRARY_PATH:-}" ./xiaozhi_server
```

### macOS
```bash
chmod +x xiaozhi_server
./build/macos/fix_rpath.sh ./xiaozhi_server
./xiaozhi_server
```

如果目录结构保持为：

```text
./xiaozhi_server
./ten-vad/lib/macOS/ten_vad.framework
```

则 macOS 包在执行过 `fix_rpath.sh` 后，默认不需要再手工设置 `DYLD_FRAMEWORK_PATH`。

如果你是从 IDE 临时目录调试，或手动移动了二进制导致相对目录结构被破坏，可使用兜底方式：

```bash
DYLD_FRAMEWORK_PATH="$PWD/ten-vad/lib/macOS" ./xiaozhi_server
```

如果你是在源码仓库里自行打 macOS 分发包，发布前需要额外执行一次：

```bash
./build/macos/fix_rpath.sh ./xiaozhi_server
```

这一步会把二进制里的 `rpath` 从开发机源码路径修正为 `@executable_path/ten-vad/lib/macOS`，让发布包在目录结构正确时直接运行。

---

## 下一步

### 1. 访问Web控制台

浏览器访问：**http://<服务器IP或域名>:8080**

<!-- 截图位置：登录界面 -->
> 图：Web控制台登录界面

### 2. 配置服务

首次使用请按照配置向导完成设置，详见：

**[管理后台使用指南 →](manager_console_guide.md)**

---

## 声纹识别服务（可选）

程序中已集成声纹服务

---

## 常见问题

### Q1: 启动后无法访问Web控制台？

检查防火墙设置，确保8080端口可访问。

### Q2: 如何重启服务？

关闭程序后重新运行即可。配置文件保存在 `config/` 目录。

### Q3: 如何查看日志？

控制台输出实时日志，如需保存可重定向：

```bash
./xiaozhi_server > server.log 2>&1
```
