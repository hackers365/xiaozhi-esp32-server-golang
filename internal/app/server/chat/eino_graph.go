package chat

import (
	"context"
	"io"
	"sync"
	"xiaozhi-esp32-server-golang/internal/component/stream_sentence"
	"xiaozhi-esp32-server-golang/internal/data/eino"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	log "xiaozhi-esp32-server-golang/logger"

	tts_types "xiaozhi-esp32-server-golang/internal/domain/tts/types"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type EinoGraphDef compose.Runnable[map[string]any, []*schema.Message]

// graphState 用于在图中存储历史消息状态
type graphState struct {
	history []*schema.Message
}

func (s *ChatSession) RunEinoGraph(ctx context.Context, text string) error {
	g, err := s.CreateLlmTtsGraph(ctx)
	if err != nil {
		log.Errorf("创建EinoGraph失败: %v", err)
		return err
	}

	// 输入改为 map[string]any，对应 chatTemplate 的占位符
	inputData := s.llmManager.GetTemplateVariables(ctx, &schema.Message{
		Role:    schema.User,
		Content: text,
	}, 20)

	handler := s.GetEinoCallbackHandle()

	// 执行图，使用状态修改器初始化历史消息
	streamReader, err := g.Stream(
		ctx,
		inputData,
		compose.WithCallbacks(handler),
	)
	if err != nil {
		log.Errorf("执行EinoGraph失败: %v", err)
		return err
	}
	for {
		msgs, err := streamReader.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Errorf("读取EinoGraph结果失败: %v", err)
			return err
		}
		for _, msg := range msgs {
			log.Debugf("EinoGraph结果: %+v", msg)
		}
	}
	return nil
}

func (s *ChatSession) CreateLlmTtsGraph(ctx context.Context) (EinoGraphDef, error) {
	// 创建图，并设置本地状态生成函数
	// 输入类型改为 map[string]any，对应 chatTemplate 的占位符
	graph := compose.NewGraph[map[string]any, []*schema.Message](
		compose.WithGenLocalState(func(ctx context.Context) *graphState {
			return &graphState{
				history: make([]*schema.Message, 0),
			}
		}),
	)

	chatTemplateNode := s.newChatTemplate(ctx)

	// 创建chatModel节点
	chatModel := s.clientState.LLMProvider

	// 创建llmSentence节点
	llmSentenceNode := compose.TransformableLambda(stream_sentence.HandleLLMWithContextAndTools)

	// 创建tts节点
	ttsNode := compose.TransformableLambda(s.createTtsTransform())

	// tts2client 节点：直接使用 s.ttsManager.EinoTtsComponents
	tts2ClientNode := compose.CollectableLambda(s.ttsManager.EinoTtsComponents)

	// 获取工具并转换为两种格式：Eino ToolInfo 和 BaseTool
	einoTools, toolsList, err := s.getEinoTools(ctx)
	if err != nil {
		log.Errorf("获取工具失败: %v", err)
		// 继续执行，但不绑定工具
		toolsList = make([]tool.BaseTool, 0)
	} else if len(einoTools) > 0 {
		// 使用 chatModel 的 WithTools 绑定工具
		chatModel, err = chatModel.WithTools(einoTools)
		if err != nil {
			log.Errorf("绑定工具到 chatModel 失败: %v", err)
			return nil, err
		}
		log.Infof("成功绑定 %d 个工具到 chatModel", len(einoTools))
	}

	// 创建 ToolsNode
	toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{
		Tools: toolsList,
	})
	if err != nil {
		log.Errorf("创建 ToolsNode 失败: %v", err)
		return nil, err
	}

	// 创建 toolCallResult 节点
	toolCallResultNode := compose.InvokableLambda(s.toolCallResultHandler)

	// 添加节点到图
	_ = graph.AddChatTemplateNode(eino.NodeChatTemplate, chatTemplateNode, compose.WithNodeName(eino.NodeChatTemplate))
	// ChatModel 节点：添加 StatePreHandler 和 StatePostHandler 来维护历史消息
	_ = graph.AddChatModelNode(
		eino.NodeLLM,
		chatModel,
		compose.WithNodeName(eino.NodeLLM),
		compose.WithStatePreHandler(func(ctx context.Context, in []*schema.Message, state *graphState) ([]*schema.Message, error) {
			// 将输入消息添加到历史记录
			state.history = append(state.history, in...)
			// 使用历史消息作为输入，这样 LLM 可以看到完整的对话历史
			return state.history, nil
		}),
		compose.WithStreamStatePostHandler(func(ctx context.Context, out *schema.StreamReader[*schema.Message], state *graphState) (*schema.StreamReader[*schema.Message], error) {
			outputReader, outputWriter := schema.Pipe[*schema.Message](10)
			defer outputWriter.Close()
			var finalMsg schema.Message
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
				msg, err := out.Recv()
				if err != nil {
					if err == io.EOF {
						// 流正常结束，将最终消息添加到历史记录
						if finalMsg.Content != "" || (finalMsg.ToolCalls != nil && len(finalMsg.ToolCalls) > 0) {
							finalMsg.Role = schema.Assistant
							state.history = append(state.history, &finalMsg)
						}
						break
					}
					return nil, err
				}
				if msg.Role == schema.Assistant {
					if msg.Content != "" {
						finalMsg.Content += msg.Content
					}
					if msg.ToolCalls != nil {
						finalMsg.ToolCalls = append(finalMsg.ToolCalls, msg.ToolCalls...)
					}
				}
				outputWriter.Send(msg, nil)
			}
			return outputReader, nil
		}),
	)
	_ = graph.AddLambdaNode(eino.NodeLLMSentence, llmSentenceNode, compose.WithNodeName(eino.NodeLLMSentence))
	_ = graph.AddLambdaNode(eino.NodeTTS, ttsNode, compose.WithNodeName(eino.NodeTTS))
	_ = graph.AddLambdaNode(eino.NodeTTS2Client, tts2ClientNode, compose.WithNodeName(eino.NodeTTS2Client))
	// ToolsNode 节点
	_ = graph.AddToolsNode(
		eino.NodeToolCall,
		toolsNode,
		compose.WithNodeName(eino.NodeToolCall),
		/*compose.WithStatePostHandler(func(ctx context.Context, output []*schema.Message, state *graphState) ([]*schema.Message, error) {
			// 将输出消息添加到历史记录
			state.history = append(state.history, output...)
			return output, nil
		}),*/
	)
	_ = graph.AddLambdaNode(eino.NodeToolCallResult, toolCallResultNode, compose.WithNodeName(eino.NodeToolCallResult))

	// 创建 llm_sentence 收集节点：将流式输出转换为非流式数组
	llmSentenceCollectNode := compose.CollectableLambda(s.llmSentenceCollectHandler)
	_ = graph.AddLambdaNode(eino.NodeLLMSentenceCollect, llmSentenceCollectNode, compose.WithNodeName(eino.NodeLLMSentenceCollect))

	_ = graph.AddPassthroughNode(eino.NodePassThrough2)

	// 创建分支节点（使用 NewGraphBranch 因为输入是非流式的 []*schema.Message）
	// 注意：branch 节点会根据条件动态路由到目标节点（包括 compose.END）
	// 因此不需要显式添加 Edge 到 compose.END，branch 会自动处理路由
	afterToolCallBranch := compose.NewGraphBranch(s.afterToolCallBranchCondition, map[string]bool{
		eino.NodeLLM: true,
		compose.END:  true,
	})

	afterLlmSentenceBranch := compose.NewStreamGraphBranch(s.afterLlmSentenceBranchCondition, map[string]bool{
		eino.NodeToolCall:           true,
		eino.NodeLLMSentenceCollect: true,
	})

	// 构建边关系
	_ = graph.AddEdge(compose.START, eino.NodeChatTemplate)
	// prompt template(输入非流式, 输出非流式) => llm
	_ = graph.AddEdge(eino.NodeChatTemplate, eino.NodeLLM)
	// llm(输入非流式，输出流式) => llm_sentence, 由分散的流式消息合并为 流式输出完整句子
	_ = graph.AddEdge(eino.NodeLLM, eino.NodeLLMSentence)

	//LLMSentence节点是LLM的下游有两个 1. tts => tts2client  2. tool call => tool_call_result => node merge , 其中2和3的输出需要路由到Merge节点
	//llm_sentence => tts => tts2client
	_ = graph.AddEdge(eino.NodeLLMSentence, eino.NodeTTS)
	_ = graph.AddEdge(eino.NodeTTS, eino.NodeTTS2Client)

	//llm_sentence => node tool call => result => merge
	//_ = graph.AddEdge(eino.NodeLLMSentence, eino.NodeToolCall)
	_ = graph.AddEdge(eino.NodeToolCall, eino.NodeToolCallResult)

	// llm_sentence => llm_sentence_collect => pass_through_2 => END
	_ = graph.AddEdge(eino.NodeLLMSentenceCollect, eino.NodePassThrough2)
	_ = graph.AddEdge(eino.NodePassThrough2, compose.END)

	// merge 节点接收来自 TTS2Client 和 ToolCallResult 的输出，然后连接到 Branch
	_ = graph.AddBranch(eino.NodeToolCallResult, afterToolCallBranch)
	_ = graph.AddBranch(eino.NodeLLMSentence, afterLlmSentenceBranch)

	// 编译图
	r, err := graph.Compile(ctx)
	if err != nil {
		log.Errorf("编译EinoGraph失败: %v", err)
		return nil, err
	}

	return r, nil
}

func (s *ChatSession) newChatTemplate(ctx context.Context) prompt.ChatTemplate {
	return s.llmManager.GetMessagesTemplates(ctx)
}

// toolCallResultTransform 处理工具调用结果，将工具调用结果转换为消息
// 输入：[]*schema.Message（来自 ToolsNode 的输出）
// 输出：*schema.StreamReader[*schema.Message]（流式消息）
func (s *ChatSession) toolCallResultHandler(ctx context.Context, input []*schema.Message) ([]*schema.Message, error) {
	var messages []*schema.Message

	// 获取工具列表，用于通过 ToolCallID 查找工具信息
	mcpTools, err := mcp.GetToolsByDeviceId(s.clientState.DeviceID, s.clientState.AgentID)
	if err != nil {
		log.Errorf("获取设备 %s 的工具失败: %v", s.clientState.DeviceID, err)
		mcpTools = make(map[string]tool.InvokableTool)
	}

	// 用于存储工具调用历史，以便通过 ToolCallID 查找对应的工具调用
	toolCallMap := make(map[string]*schema.ToolCall)

	// 遍历输入消息列表
	for _, msg := range input {
		if msg == nil {
			continue
		}

		// 如果是工具结果消息（Role == schema.Tool），处理工具调用结果
		if msg.Role == schema.Tool && msg.ToolCallID != "" {
			// 查找对应的工具调用
			toolCall, ok := toolCallMap[msg.ToolCallID]
			if !ok {
				log.Warnf("未找到 ToolCallID %s 对应的工具调用", msg.ToolCallID)
				continue
			}

			// 获取工具对象
			toolName := toolCall.Function.Name
			toolObj, ok := mcpTools[toolName]
			if !ok || toolObj == nil {
				log.Warnf("未找到工具: %s", toolName)
				continue
			}

			// 处理工具调用结果
			var wg sync.WaitGroup
			processedResult, shouldStop := s.llmManager.processToolCallResult(ctx, toolName, msg.Content, toolObj, &wg)

			// 创建处理后的消息
			processedMsg := &schema.Message{
				Role:       schema.Tool,
				ToolCallID: msg.ToolCallID,
				Content:    processedResult,
			}

			// 等待异步处理完成（如音频播放）
			wg.Wait()

			// 如果应该停止处理，在消息 Content 中添加特殊标记
			// 这样 branchCondition 可以识别并直接结束流程
			if shouldStop {
				// 在 Content 前面添加特殊标记，标识这是一个需要停止后续处理的消息
				// 使用特殊前缀来标记，branchCondition 会检查这个标记
				processedMsg.Content = "[STOP]" + processedMsg.Content
				log.Debugf("工具 %s 的执行结果需要停止后续处理，已标记消息", toolName)
			}

			// 发送处理后的消息
			messages = append(messages, processedMsg)
		} else {
			// 其他消息直接透传
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

// llmSentenceCollectHandler 收集 llm_sentence 的流式输出，转换为非流式数组
// 输入：*schema.StreamReader[*schema.Message]（流式消息）
// 输出：[]*schema.Message（消息列表）
func (s *ChatSession) llmSentenceCollectHandler(ctx context.Context, input *schema.StreamReader[*schema.Message]) ([]*schema.Message, error) {
	var messages []*schema.Message

	for {
		select {
		case <-ctx.Done():
			return messages, ctx.Err()
		default:
		}

		msg, err := input.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Errorf("llmSentenceCollectHandler 读取输入失败: %v", err)
			return messages, err
		}

		if msg != nil {
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

// mergeCollect 合并 llmsentence 和 tool_call 的输出
// 输入：*schema.StreamReader[*schema.Message]（流式消息）
// 输出：[]*schema.Message（消息列表，供 LLM 节点使用）
func (s *ChatSession) mergeTransform(ctx context.Context, input *schema.StreamReader[*schema.Message]) ([]*schema.Message, error) {
	// 这个节点接收来自 tts2client 或 tool_call 的输出
	// 在 eino graph 中，如果多个边指向同一个节点，该节点会接收所有上游节点的输出
	// 收集所有流式消息到列表中
	var messages []*schema.Message

	for {
		select {
		case <-ctx.Done():
			return messages, ctx.Err()
		default:
		}

		msg, err := input.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Errorf("mergeTransform 读取输入失败: %v", err)
			return messages, err
		}

		if msg != nil {
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

// afterLlmSentenceBranchCondition 判断是否有工具调用，决定 passthrough 节点还是结束
// 输入：streamReader[*schema.Message]
// 输出：目标节点名称（string）
func (s *ChatSession) afterToolCallBranchCondition(ctx context.Context, input []*schema.Message) (string, error) {
	// 一次遍历完成所有判断
	hasToolCall := false
	hasToolResult := false

	for _, msg := range input {
		if msg == nil {
			continue
		}

		// 优先检查停止标志（优先级最高）
		if msg.Role == schema.Tool && len(msg.Content) >= 6 && msg.Content[:6] == "[STOP]" {
			log.Debugf("检测到需要停止处理的工具结果（音频/资源链接），直接结束流程")
			return compose.END, nil
		}

		// 检查是否有工具调用请求
		if msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
			hasToolCall = true
		}

		// 检查是否有工具结果（Role 为 Tool，且没有停止标志）
		if msg.Role == schema.Tool {
			hasToolResult = true
		}
	}

	// 根据检查结果决定返回哪个节点
	if hasToolCall {
		log.Debugf("检测到工具调用，返回到 llm 节点")
		return eino.NodeLLM, nil
	}

	if hasToolResult {
		log.Debugf("检测到工具结果，返回到 llm 节点继续处理")
		return eino.NodeLLM, nil
	}

	log.Debugf("未检测到工具调用或工具结果，直接结束")
	return compose.END, nil
}

// afterLlmSentenceBranchCondition 判断是否有工具调用，决定路由到 tool_call 还是 tool_call_result
// 输入：*schema.StreamReader[*schema.Message]（来自 llm_sentence 的流式消息）
// 输出：目标节点名称（string）
func (s *ChatSession) afterLlmSentenceBranchCondition(ctx context.Context, input *schema.StreamReader[*schema.Message]) (string, error) {
	// 收集所有消息，查找是否有工具调用
	var hasToolCall bool
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		msg, err := input.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		if msg != nil && msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
			hasToolCall = true
		}
	}

	// 根据检查结果决定路由
	if hasToolCall {
		log.Debugf("检测到工具调用，路由到 tool_call 节点")
		return eino.NodeToolCall, nil
	}

	// 没有工具调用，直接路由到收集节点（跳过 tool_call 和 tool_call_result）
	log.Debugf("没有工具调用，直接路由到收集节点")
	return eino.NodeLLMSentenceCollect, nil
}

// createTtsTransform 创建 TTS 转换函数
func (s *ChatSession) createTtsTransform() func(context.Context, *schema.StreamReader[*schema.Message]) (*schema.StreamReader[*schema.StreamReader[tts_types.TtsChunk]], error) {
	return func(ctx context.Context, input *schema.StreamReader[*schema.Message]) (*schema.StreamReader[*schema.StreamReader[tts_types.TtsChunk]], error) {
		return s.clientState.TTSProvider.Transform(
			ctx,
			input,
			tts_types.WithSampleRate(s.clientState.OutputAudioFormat.SampleRate),
			tts_types.WithChannel(s.clientState.OutputAudioFormat.Channels),
			tts_types.WithFrameDuration(s.clientState.OutputAudioFormat.FrameDuration),
		)
	}
}

func (s *ChatSession) GetEinoCallbackHandle() callbacks.Handler {
	sendTtsStart := func(ctx context.Context) context.Context {
		if startValue := ctx.Value("tts_start"); startValue == nil {
			// TTS 节点开始时，发送 TTS 开始信号
			err := s.serverTransport.SendTtsStart()
			if err != nil {
				log.Errorf("TTS节点开始: 发送TTS开始信号失败: %v", err)
			} else {
				// 在 context 中设置标记，避免重复发送
				ctx = context.WithValue(ctx, "tts_start", true)
			}
		}
		return ctx
	}
	sendTtsStop := func(ctx context.Context) context.Context {
		//isTtsStart, ok := ctx.Value("tts_start").(bool)
		// Graph 自身结束时，发送 TTS 结束信号
		err := s.serverTransport.SendTtsStop()
		if err != nil {
			log.Errorf("Graph结束: 发送TTS结束信号失败: %v", err)
		}
		return ctx
	}
	// 创建 callback handler 来监听组件生命周期
	log.Debugf("开始构建 callback handler")
	handler := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			// 立即打印日志，确保函数被调用
			log.Infof("✅ OnStartFn 被调用: info.Name=%s, info.Component=%v, info.Type=%v", info.Name, info.Component, info.Type)
			// 判断是否是 Graph 级别的 callback
			if info.Component == compose.ComponentOfGraph {
				//log.Infof("   → Graph 级别 OnStart 被触发")
			} else {
				//log.Debugf("   → 节点 '%s' 开始执行", info.Name)
				// 判断是否是 TTS 节点
				if info.Name == eino.NodeTTS {
					return sendTtsStart(ctx)
				}
			}
			return ctx
		}).
		OnStartWithStreamInputFn(func(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
			// 立即打印日志，确保函数被调用
			log.Infof("✅ OnStartWithStreamInputFn 被调用: info.Name=%s, info.Component=%v, info.Type=%v", info.Name, info.Component, info.Type)
			// 判断是否是 Graph 级别的 callback
			if info.Component == compose.ComponentOfGraph {
				log.Infof("   → Graph 级别 OnStartWithStreamInput 被触发")
			} else {
				if info.Name == eino.NodeTTS {
					return sendTtsStart(ctx)
				}
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			// 立即打印日志，确保函数被调用
			log.Infof("✅ OnEndFn 被调用: info.Name=%s, info.Component=%v, info.Type=%v", info.Name, info.Component, info.Type)
			// 判断是否是 Graph 级别的 callback
			if info.Component == compose.ComponentOfGraph {
				return sendTtsStop(ctx)
			} else {
				log.Debugf("   → 节点 '%s' 执行完成", info.Name)
			}
			return ctx
		}).
		OnEndWithStreamOutputFn(func(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
			// 立即打印日志，确保函数被调用
			log.Infof("✅ OnEndWithStreamOutputFn 被调用: info.Name=%s, info.Component=%v, info.Type=%v", info.Name, info.Component, info.Type)
			// 判断是否是 Graph 级别的 callback
			if info.Component == compose.ComponentOfGraph {
				return sendTtsStop(ctx)
			}
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			// 立即打印日志，确保函数被调用
			log.Infof("✅ OnErrorFn 被调用: info.Name=%s, info.Component=%v, err=%v", info.Name, info.Component, err)
			// Graph 执行出错时，也发送 TTS 结束信号
			if info.Component == compose.ComponentOfGraph {
				return sendTtsStop(ctx)
			} else {
				log.Errorf("   → 节点 '%s' 执行出错: %v", info.Name, err)
			}
			return ctx
		}).
		Build()

	return handler
}

// getEinoTools 获取工具并转换为两种格式：Eino ToolInfo 和 BaseTool
// 返回：[]*schema.ToolInfo（用于绑定到 chatModel）、[]tool.BaseTool（用于创建 ToolsNode）、error
func (s *ChatSession) getEinoTools(ctx context.Context) ([]*schema.ToolInfo, []tool.BaseTool, error) {
	// 获取 MCP 工具列表（只获取一次）
	mcpTools, err := mcp.GetToolsByDeviceId(s.clientState.DeviceID, s.clientState.AgentID)
	if err != nil {
		log.Errorf("获取设备 %s 的工具失败: %v", s.clientState.DeviceID, err)
		return nil, nil, err
	}

	// 将 MCP 工具转换为接口格式以便传递给转换函数
	mcpToolsInterface := make(map[string]interface{})
	for name, tool := range mcpTools {
		mcpToolsInterface[name] = tool
	}

	// 转换 MCP 工具为 Eino ToolInfo 格式
	einoTools, err := llm.ConvertMCPToolsToEinoTools(ctx, mcpToolsInterface)
	if err != nil {
		log.Errorf("转换 MCP 工具失败: %v", err)
		return nil, nil, err
	}

	// 将 MCP 工具转换为 BaseTool 列表
	toolsList := make([]tool.BaseTool, 0, len(mcpTools))
	for _, t := range mcpTools {
		toolsList = append(toolsList, t)
	}

	log.Infof("成功获取 %d 个工具", len(einoTools))
	return einoTools, toolsList, nil
}
