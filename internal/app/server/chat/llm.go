package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	. "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/play_music"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcp_go "github.com/mark3labs/mcp-go/mcp"
)

const (
	MaxMessageCount = 10

	McpReadResourcePageSize       = 100 * 1024
	McpReadResourceStreamDoneFlag = "[DONE]"
)

type LLMResponseChannelItem struct {
	ctx          context.Context
	userMessage  *schema.Message
	responseChan chan llm_common.LLMResponseStruct
	onStartFunc  func(args ...any)
	onEndFunc    func(err error, args ...any)
}

type LLMManager struct {
	clientState     *ClientState
	serverTransport *ServerTransport
	ttsManager      *TTSManager

	einoTools []*schema.ToolInfo

	llmResponseQueue *util.Queue[LLMResponseChannelItem]
}

func NewLLMManager(clientState *ClientState, serverTransport *ServerTransport, ttsManager *TTSManager) *LLMManager {
	return &LLMManager{
		clientState:      clientState,
		serverTransport:  serverTransport,
		ttsManager:       ttsManager,
		llmResponseQueue: util.NewQueue[LLMResponseChannelItem](10),
	}
}

func (l *LLMManager) Start(ctx context.Context) {
	l.processLLMResponseQueue(ctx)
}

func (l *LLMManager) processLLMResponseQueue(ctx context.Context) {
	for {
		item, err := l.llmResponseQueue.Pop(ctx, 0) // 阻塞式
		if err != nil {
			if err == util.ErrQueueCtxDone {
				return
			}
			// 其他错误
			continue
		}

		log.Debugf("processLLMResponseQueue item: %+v", item)
		if item.onStartFunc != nil {
			item.onStartFunc()
		}
		_, err = l.handleLLMResponse(item.ctx, item.userMessage, item.responseChan)
		if item.onEndFunc != nil {
			item.onEndFunc(err)
		}
	}
}

func (l *LLMManager) ClearLLMResponseQueue() {
	l.llmResponseQueue.Clear()
}

func (l *LLMManager) AddTextToTTSQueue(text string) error {
	log.Debugf("AddTextToTTSQueue text: %s", text)
	msg := &schema.Message{
		Role:    schema.User,
		Content: text,
	}
	llmResponseChan := make(chan llm_common.LLMResponseStruct, 10)
	llmResponseChan <- llm_common.LLMResponseStruct{
		IsStart: true,
		IsEnd:   true,
		Text:    text,
	}
	close(llmResponseChan)

	sessionCtx := l.clientState.SessionCtx.Get(l.clientState.Ctx)
	ctx := l.clientState.AfterAsrSessionCtx.Get(sessionCtx)
	l.HandleLLMResponseChannelAsync(ctx, msg, llmResponseChan)

	return nil
}

func (l *LLMManager) HandleLLMResponseChannelAsync(ctx context.Context, userMessage *schema.Message, responseChan chan llm_common.LLMResponseStruct) error {
	needSendTtsCmd := true
	val := ctx.Value("nest")
	log.Debugf("AddLLMResponseChannel nest: %+v", val)
	if nest, ok := val.(int); ok {
		if nest > 1 {
			needSendTtsCmd = false
		}
	}

	var onStartFunc func(...any)
	var onEndFunc func(err error, args ...any)

	if needSendTtsCmd {
		onStartFunc = func(...any) {
			l.serverTransport.SendTtsStart()
		}
		onEndFunc = func(err error, args ...any) {
			l.serverTransport.SendTtsStop()
		}
	}

	item := LLMResponseChannelItem{
		ctx:          ctx,
		userMessage:  userMessage,
		responseChan: responseChan,
		onStartFunc:  onStartFunc,
		onEndFunc:    onEndFunc,
	}
	err := l.llmResponseQueue.Push(item)
	if err != nil {
		log.Warnf("llmResponseQueue 已满或已关闭, 丢弃消息")
		return fmt.Errorf("llmResponseQueue 已满或已关闭, 丢弃消息")
	}
	return nil
}

func (l *LLMManager) HandleLLMResponseChannelSync(ctx context.Context, userMessage *schema.Message, llmResponseChannel chan llm_common.LLMResponseStruct, einoTools []*schema.ToolInfo) (bool, error) {
	needSendTtsCmd := true
	val := ctx.Value("nest")
	log.Debugf("AddLLMResponseChannel nest: %+v", val)
	if nest, ok := val.(int); ok {
		if nest > 1 {
			needSendTtsCmd = false
		}
	}
	if needSendTtsCmd {
		l.serverTransport.SendTtsStart()
	}
	ok, err := l.handleLLMResponse(ctx, userMessage, llmResponseChannel)
	if needSendTtsCmd {
		l.serverTransport.SendTtsStop()
	}

	return ok, err
}

// HandleLLMResponse 处理LLM响应
func (l *LLMManager) handleLLMResponse(ctx context.Context, userMessage *schema.Message, llmResponseChannel chan llm_common.LLMResponseStruct) (bool, error) {
	log.Debugf("handleLLMResponse start")
	defer log.Debugf("handleLLMResponse end")
	select {
	case <-ctx.Done():
		log.Debugf("handleLLMResponse ctx done, return")
		return false, nil
	default:
	}

	state := l.clientState
	var toolCalls []schema.ToolCall
	var fullText bytes.Buffer

	//var hasTextResponse bool
	for {
		select {
		case <-ctx.Done():
			// 上下文已取消，优先处理取消逻辑
			log.Infof("%s 上下文已取消，停止处理LLM响应, context done, exit", state.DeviceID)
			//sendTtsStartEndFunc(false)
			return false, nil
		default:
			// 非阻塞检查，如果ctx没有Done，继续处理LLM响应
			select {
			case llmResponse, ok := <-llmResponseChannel:
				if !ok {
					// 通道已关闭，退出协程
					log.Infof("LLM 响应通道已关闭，退出协程")
					return true, nil
				}

				log.Debugf("LLM 响应: %+v", llmResponse)

				if len(llmResponse.ToolCalls) > 0 {
					log.Debugf("获取到工具: %+v", llmResponse.ToolCalls)
					toolCalls = append(toolCalls, llmResponse.ToolCalls...)
				}

				if llmResponse.Text != "" {
					//hasTextResponse = true
					// 处理文本内容响应
					if err := l.ttsManager.handleTextResponse(ctx, llmResponse, true); err != nil {
						return true, err
					}
					fullText.WriteString(llmResponse.Text)
				}

				if llmResponse.IsEnd {
					if len(toolCalls) == 0 {
						//写到redis中
						if userMessage != nil {
							if userMessage.Role == schema.User {
								l.AddLlmMessage(ctx, userMessage)
							}
						}
						strFullText := fullText.String()
						if strFullText != "" || len(toolCalls) > 0 {
							l.AddLlmMessage(ctx, schema.AssistantMessage(strFullText, toolCalls))
						}
					}
					if len(toolCalls) > 0 {
						/*
							if !hasTextResponse {
								//有工具调用 && 没有文本响应，发送"查询中", 异步tts
								l.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{
									Text: "查询中, 请稍候",
								}, false)
							}*/

						lctx := context.WithValue(ctx, "nest", 2)
						invokeToolSuccess, err := l.HandleToolCallResponse(lctx, userMessage, schema.AssistantMessage(fullText.String(), toolCalls), toolCalls)
						if err != nil {
							log.Errorf("处理工具调用响应失败: %v", err)
							return true, fmt.Errorf("处理工具调用响应失败: %v", err)
						}
						if !invokeToolSuccess {
							//工具调用失败
							if err := l.ttsManager.handleTextResponse(ctx, llmResponse, false); err != nil {
								return true, err
							}
							fullText.WriteString(llmResponse.Text)
							//sendTtsStartEndFunc(false)
						}
					} else {
						//sendTtsStartEndFunc(false)
					}

					return ok, nil
				}
			case <-ctx.Done():
				// 上下文已取消，退出协程
				log.Infof("%s 上下文已取消，停止处理LLM响应, context done, exit", state.DeviceID)
				//sendTtsStartEndFunc(false)
				return false, nil
			}
		}
	}
}

// handleToolCallResponse 处理工具调用响应
func (l *LLMManager) HandleToolCallResponse(ctx context.Context, userMessage *schema.Message, respMsg *schema.Message, tools []schema.ToolCall) (bool, error) {
	if len(tools) == 0 {
		return false, nil
	}

	state := l.clientState

	log.Infof("处理 %d 个工具调用", len(tools))

	var invokeToolSuccess bool

	// 从 context 中获取 chat_session_operator（如果存在）
	// 如果不存在，说明没有需要 ChatSession 操作的工具，可以正常执行
	var toolCtx context.Context = ctx
	if chatSessionOperator, ok := ctx.Value("chat_session_operator").(ChatSessionOperator); ok {
		// 在 context 中传递 chat_session_operator，供 local mcp tool 使用
		toolCtx = context.WithValue(ctx, "chat_session_operator", chatSessionOperator)
	}

	var shouldStopLLMProcessing bool

	var wg sync.WaitGroup

	var messageList []*schema.Message

	messageList = append(messageList, userMessage, respMsg)

	addMessageFunc := func(toolCall schema.ToolCall, result string) {
		toolResultMsg := &schema.Message{
			Role:       schema.Tool,
			ToolCallID: toolCall.ID,
			Content:    result,
		}
		messageList = append(messageList, toolResultMsg)
	}

	for _, toolCall := range tools {
		toolName := toolCall.Function.Name
		tool, ok := mcp.GetToolByName(state.DeviceID, toolName)
		if !ok || tool == nil {
			log.Errorf("未找到工具: %s", toolName)
			addMessageFunc(toolCall, fmt.Sprintf("未找到工具: %s", toolName))
			continue
		}
		log.Infof("进行工具调用请求: %s, 参数: %+v", toolName, toolCall.Function.Arguments)
		startTs := time.Now().UnixMilli()
		fcResult, err := tool.InvokableRun(toolCtx, toolCall.Function.Arguments)
		if err != nil {
			log.Errorf("工具调用失败: %v", err)
			addMessageFunc(toolCall, fmt.Sprintf("工具 %s 调用失败: %v", toolName, err))
			continue
		}
		costTs := time.Now().UnixMilli() - startTs
		invokeToolSuccess = true
		if len(fcResult) > 2048 {
			log.Infof("工具调用结果 len: %d, 耗时: %dms", len(fcResult), costTs)
		} else {
			log.Infof("工具调用结果 %s, 耗时: %dms", fcResult, costTs)
		}

		result, shouldStop := l.processToolCallResult(ctx, toolName, fcResult, tool, &wg)
		if shouldStop {
			shouldStopLLMProcessing = true
		}
		addMessageFunc(toolCall, result)
	}

	if len(messageList) > 0 {
		for _, msg := range messageList {
			l.AddLlmMessage(ctx, msg)
		}
	}

	wg.Wait()

	// 如果工具调用成功且没有被标记为停止处理，则继续LLM调用
	if invokeToolSuccess && !shouldStopLLMProcessing {
		l.DoLLmRequest(ctx, nil, l.einoTools, true)
	}

	return invokeToolSuccess, nil
}

func (l *LLMManager) handleResourceLink(ctx context.Context, resourceLink mcp_go.ResourceLink, toolCall tool.InvokableTool, wg *sync.WaitGroup) error {
	wg.Add(1)
	//从resourceLink中获取资源
	client := toolCall.(*mcp.McpTool).GetClient()

	var pipeReader *io.PipeReader
	var pipeWriter *io.PipeWriter
	pipeReader, pipeWriter = io.Pipe()

	audioFormat := util.GetAudioFormatByMimeType(resourceLink.MIMEType)

	streamChan := make(chan []byte, 0) // 增加缓冲区大小
	go func() error {
		defer func() {
			close(streamChan)
		}()

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case audioData, ok := <-streamChan:
					if !ok {
						pipeWriter.Close()
						return
					}
					if _, err := pipeWriter.Write(audioData); err != nil {
						log.Errorf("写入pipe失败: %v", err)
						return
					}
				}
			}
		}()

		start := 0
		page := McpReadResourcePageSize
		totalRead := 0
		pageCount := 0

		log.Infof("开始读取资源: %s, 分页大小: %d", resourceLink.URI, page)

		for {
			select {
			case <-ctx.Done():
				log.Debugf("资源读取被取消")
				return nil
			default:
				pageCount++
				log.Debugf("读取第 %d 页资源，起始位置: %d, 结束位置: %d", pageCount, start, start+page)

				// 创建带超时的上下文
				readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

				// 读取资源
				resourceResult, err := client.ReadResource(readCtx, mcp_go.ReadResourceRequest{
					Params: mcp_go.ReadResourceParams{
						URI:       resourceLink.URI,
						Arguments: map[string]any{"url": resourceLink.Description, "start": start, "end": start + page},
					},
				})
				cancel()

				if err != nil {
					log.Errorf("读取资源失败 (第 %d 页), resourceUri: %s, resourceResult: %+v, err: %v", pageCount, resourceLink.Description, resourceResult, err)

					// 如果是超时错误，尝试重试
					if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
						log.Warnf("资源读取超时，尝试重试...")
						time.Sleep(1 * time.Second)
						continue
					}

					return fmt.Errorf("读取资源失败: %v", err)
				}

				if len(resourceResult.Contents) == 0 {
					log.Infof("资源读取完成，总共读取 %d 字节，共 %d 页", totalRead, pageCount-1)
					return nil
				}

				hasData := false
				for _, content := range resourceResult.Contents {
					if audioContent, ok := content.(mcp_go.BlobResourceContents); ok {
						if len(audioContent.Blob) == 0 {
							log.Debugf("音频数据为空，跳过")
							continue
						}
						log.Debugf("第 %d 页 resourceResult len: %d", pageCount, len(audioContent.Blob))
						rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Blob)
						if err != nil {
							log.Errorf("解码音频数据失败: %v", err)
							return fmt.Errorf("解码音频数据失败: %v", err)
						}

						if string(rawAudioData) == McpReadResourceStreamDoneFlag {
							log.Debugf("资源读取完成")
							return nil
						}

						select {
						case <-ctx.Done():
							log.Debugf("资源读取被取消")
							return nil
						case streamChan <- rawAudioData:
							totalRead += len(rawAudioData)
							hasData = true
							log.Debugf("成功发送第 %d 页数据，长度: %d, 累计: %d", pageCount, len(rawAudioData), totalRead)
						}

						if len(rawAudioData) < page {
							log.Debugf("资源读取完成")
							return nil
						}
					}
				}

				// 如果这一页没有数据，说明已经读取完毕
				if !hasData {
					log.Infof("资源读取完成，总共读取 %d 字节，共 %d 页", totalRead, pageCount)
					return nil
				}

				start += page
			}
		}
	}()

	// 使用music_player播放音乐
	audioChan, err := play_music.PlayMusicFromPipe(ctx, pipeReader, l.clientState.OutputAudioFormat.SampleRate, l.clientState.OutputAudioFormat.FrameDuration, audioFormat)
	if err != nil {
		log.Errorf("播放音乐失败: %v", err)
		return fmt.Errorf("播放音乐失败: %v", err)
	}

	playText := fmt.Sprintf("正在播放音乐: %s", resourceLink.Name)
	l.serverTransport.SendSentenceStart(playText)

	go func() {
		defer func() {
			l.serverTransport.SendSentenceEnd(playText)
			log.Infof("音乐播放完成: %s", resourceLink.Name)
		}()

		l.ttsManager.SendTTSAudio(ctx, audioChan, true)
		wg.Done()
	}()

	return nil
}

func (l *LLMManager) handleAudioContent(ctx context.Context, realMusicName string, audioContent mcp_go.AudioContent, wg *sync.WaitGroup) error {
	wg.Add(1)
	rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Data)
	if err != nil {
		log.Errorf("解码音频数据失败: %v", err)
		return fmt.Errorf("解码音频数据失败: %v", err)
	}
	audioFormat := util.GetAudioFormatByMimeType(audioContent.MIMEType)
	// 使用music_player播放音乐
	audioChan, err := play_music.PlayMusicFromAudioData(ctx, rawAudioData, l.clientState.OutputAudioFormat.SampleRate, l.clientState.OutputAudioFormat.FrameDuration, audioFormat)
	if err != nil {
		log.Errorf("播放音乐失败: %v", err)
		return fmt.Errorf("播放音乐失败: %v", err)
	}

	playText := fmt.Sprintf("正在播放音乐: %s", realMusicName)
	l.serverTransport.SendSentenceStart(playText)

	go func() {
		defer func() {
			l.serverTransport.SendSentenceEnd(playText)
			log.Infof("音乐播放完成: %s", realMusicName)
		}()
		l.ttsManager.SendTTSAudio(ctx, audioChan, true)
		wg.Done()
	}()

	return nil
}

// processToolCallResult 处理工具调用结果，提取并处理音频、资源链接、文本等内容
// 返回处理后的结果字符串和是否应该停止 LLM 处理
func (l *LLMManager) processToolCallResult(ctx context.Context, toolName string, fcResult string, toolCall tool.InvokableTool, wg *sync.WaitGroup) (string, bool) {
	var result string = fcResult
	var contentList []mcp_go.Content
	if mcpResp, ok := l.handleLocalToolResult(fcResult); ok {
		contentList = mcpResp.GetContent()
	} else if toolCallResult, ok := l.handleToolResult(fcResult); ok {
		if toolCallResult.IsError {
			log.Errorf("工具调用失败: %s, 错误: %s", fcResult, toolCallResult.IsError)
		}
		contentList = toolCallResult.Content
	}
	if len(contentList) > 0 {
		var mcpContent string
		//如果有audio数据, 则进行播放
		for _, content := range contentList {
			if audioContent, ok := content.(mcp_go.AudioContent); ok {
				log.Debugf("调用工具 %s 返回音频资源长度: %d", toolName, len(audioContent.Data))

				mcpContent = "执行成功"
				//播放音频资源,此时mcpContent是
				err := l.handleAudioContent(ctx, mcpContent, audioContent, wg)
				if err != nil {
					log.Errorf("mcp播放音频资源失败: %v", err)
					mcpContent = "执行失败"
				}
				return mcpContent, true
			} else if resourceLink, ok := content.(mcp_go.ResourceLink); ok {
				log.Debugf("调用工具 %s 返回资源链接: %+v", toolName, resourceLink)
				mcpContent = "执行成功"
				err := l.handleResourceLink(ctx, resourceLink, toolCall, wg)
				if err != nil {
					log.Errorf("mcp播放资源链接失败: %v", err)
					mcpContent = "执行失败"
				}
				return mcpContent, true
			} else if textContent, ok := content.(mcp_go.TextContent); ok {
				log.Debugf("调用工具 %s 返回文本资源长度: %s", toolName, textContent.Text)
				mcpContent += textContent.Text
			}
		}
		if mcpContent != "" {
			result = mcpContent
		}
	}
	return result, false
}

func (l *LLMManager) handleLocalToolResult(toolResult string) (MCPResponse, bool) {
	// 首先尝试解析新的结构化响应
	var response MCPResponse
	var err error
	if response, err = ParseMCPResponse(toolResult); err != nil {
		return nil, false
	}
	return response, true
}

func (l *LLMManager) handleToolResult(toolResultStr string) (mcp_go.CallToolResult, bool) {
	var toolResult mcp_go.CallToolResult
	if err := json.Unmarshal([]byte(toolResultStr), &toolResult); err != nil {
		log.Errorf("解析工具结果失败: %v", err)
		return toolResult, false
	}

	return toolResult, true
}

func (l *LLMManager) DoLLmRequest(ctx context.Context, userMessage *schema.Message, einoTools []*schema.ToolInfo, isSync bool) error {
	log.Debugf("发送带工具的 LLM 请求, seesionID: %s, requestEinoMessages: %+v", l.clientState.SessionID, userMessage)
	clientState := l.clientState

	l.einoTools = einoTools
	//组装历史消息和当前用户的消息
	requestMessages := l.GetMessages(ctx, userMessage, MaxMessageCount)
	clientState.SetStatus(ClientStatusLLMStart)
	responseSentences, err := llm.HandleLLMWithContextAndTools(
		ctx,
		clientState.LLMProvider,
		requestMessages,
		einoTools,
		l.clientState.SessionID,
	)
	if err != nil {
		log.Errorf("发送带工具的 LLM 请求失败, seesionID: %s, error: %v", l.clientState.SessionID, err)
		return fmt.Errorf("发送带工具的 LLM 请求失败: %v", err)
	}

	log.Debugf("DoLLmRequest goroutine开始 - SessionID: %s, context状态: %v", l.clientState.SessionID, ctx.Err())

	if isSync {
		_, err := l.HandleLLMResponseChannelSync(ctx, userMessage, responseSentences, einoTools)
		if err != nil {
			log.Errorf("处理 LLM 响应失败, seesionID: %s, error: %v", l.clientState.SessionID, err)
			return err
		}
	} else {
		err = l.HandleLLMResponseChannelAsync(ctx, userMessage, responseSentences)
		if err != nil {
			log.Errorf("处理 LLM 响应失败, seesionID: %s, error: %v", l.clientState.SessionID, err)
		}
	}

	log.Debugf("DoLLmRequest 结束 - SessionID: %s", l.clientState.SessionID)

	return nil
}

func (l *LLMManager) AddLlmMessage(ctx context.Context, msg *schema.Message) error {
	if msg == nil {
		log.Warnf("尝试添加 nil 消息到 LLM 对话历史")
		return fmt.Errorf("消息不能为 nil")
	}
	//同步添加到内存中
	l.clientState.AddMessage(msg)
	//发送消息到evenbus进行异步处理, 存储消息、添加到记忆体中
	eventbus.Get().Publish(eventbus.TopicAddMessage, l.clientState, *msg)

	return nil
}

func (l *LLMManager) GetMessages(ctx context.Context, userMessage *schema.Message, count int) []*schema.Message {
	chatTemplate := l.GetMessagesTemplates(ctx)

	variables := l.GetTemplateVariables(ctx, userMessage, count)
	msgList, err := chatTemplate.Format(ctx, variables)
	if err != nil {
		log.Errorf("格式化消息失败: %v", err)
		return nil
	}
	return msgList

}

func (l *LLMManager) GetMessagesTemplates(ctx context.Context) prompt.ChatTemplate {
	// 构建 system prompt
	systemTpl := fmt.Sprintf("{system_prompt} \n {user_prompt} \n {user_profile} \n {history_context}")

	return prompt.FromMessages(schema.FString,
		schema.SystemMessage(systemTpl),
		schema.MessagesPlaceholder("message_histories", true),
		schema.UserMessage("{user_query}"),
	)
}

// 获取模板所需变量数据
func (l *LLMManager) GetTemplateVariables(ctx context.Context, userMessage *schema.Message, count int) map[string]any {
	//从dialogue中获取
	messageList := l.clientState.GetMessages(count)

	var userProfile, historyContext string

	if l.clientState.MemoryContext != "" {
		userProfile = fmt.Sprintf("\n用户个性化信息:\n %s", l.clientState.MemoryContext)
	}

	//search memory
	if l.clientState.MemoryProvider != nil && userMessage != nil {
		memoryContext, err := l.clientState.MemoryProvider.Search(ctx, l.clientState.GetDeviceIDOrAgentID(), userMessage.Content, 10, 180)
		if err != nil {
			log.Errorf("搜索记忆失败: %v", err)
		}
		log.Debugf("搜索记忆成功, 输入内容: %s, 记忆内容: %s", userMessage.Content, memoryContext)
		if memoryContext != "" {
			historyContext = fmt.Sprintf("\n以下是相关对话:\n %s", memoryContext)
		}
	}
	return map[string]any{
		"system_prompt":   "",                         //系统全局prompt
		"user_prompt":     l.clientState.SystemPrompt, //agent专属prompt
		"user_profile":    userProfile,                //用户个性化信息
		"history_context": historyContext,             //历史相关对话
		"message_list":    messageList,                //历史消息
		"user_query":      userMessage.Content,        //用户输入
	}
}
