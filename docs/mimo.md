# 小米 MiMo ASR 与 TTS

本分支在现有 ASR、TTS 提供商框架中接入小米 MiMo。无需修改 ESP32 固件或迁移数据库。

## 配置

在管理后台的 **ASR 配置**和 **TTS 配置**中分别选择「小米 MiMo」。两者都需要 API Key：可填入表单，也可在**运行 Go 主服务的环境**中设置 `MIMO_API_KEY`。默认 API 地址为 `https://api.xiaomimimo.com/v1/chat/completions`。不要把 Key 提交到仓库或贴到 PR 中。

API 地址支持填写根域名、以 `/v1` 结尾的基础地址，或完整的 `/chat/completions` 地址；客户端会自动补全接口路径，并保留反向代理的路径前缀和查询参数。支持 HTTP 和 HTTPS，例如 Docker Mock 地址 `http://mock-ai:18080` 会补全为 `http://mock-ai:18080/v1/chat/completions`。智能体配置中的 MiMo 音色候选通过现有音色接口提供。

| 功能 | 模型和用法 |
| --- | --- |
| 语音识别 | `mimo-v2.5-asr`；支持 `auto`、`zh`、`en`。服务端收齐一段由 VAD 划分的语音后，以 16 kHz WAV 上传；不支持边说边上传的实时输入。 |
| 普通语音合成 | `mimo-v2.5-tts`；默认音色 `mimo_default`。后台还提供冰糖、茉莉、苏打、白桦、Mia、Chloe、Milo、Dean 等候选音色。服务端接收 24 kHz PCM16 音频流，再通过现有链路编码供设备播放。 |
| 声音设计 | `mimo-v2.5-tts-voicedesign`；填写 `voice_prompt` 描述目标声音。 |
| 声音复刻 | `mimo-v2.5-tts-voiceclone`；填写 `reference_audio_path`，指向**运行 Go 主服务的机器或容器内**可读取的 WAV/MP3 文件。每次合成都会发送该文件；此路径不是远端音色 ID，也没有接入现有声音素材上传界面。 |

声音设计和声音复刻虽然走流式响应，实际可能要等推理完成才开始返回音频。

## 检查与验收

先按项目的[编译与部署指南](../doc/compile_deploy.md)安装 Go 和主服务所需的原生音频依赖。单独运行 `go test` 或 `go build` 只能检查代码，不能证明 MiMo API 或 ESP32 播放正常。

```bash
go test ./pkg/mimo ./internal/domain/asr ./internal/domain/tts/mimo
go build -o xiaozhi_server ./cmd/server
```

真实验收需要在运行中的主服务配置有效 Key，并依次检查：短句识别结果、普通 TTS 在 ESP32 上播放、播放中断，以及无效 Key 时的错误反馈。声音设计和声音复刻可在普通 TTS 通过后单独测试。服务端产生音频不等于设备扬声器已播放，最后一步需要在设备上确认。

API 参考：[小米 MiMo 语音合成文档](https://mimo.mi.com/docs/zh-CN/quick-start/usage-guide/multimodal-understanding/speech-synthesis-v2.5)。
