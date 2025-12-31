package eino

// Eino Graph 节点名称常量
const (
	// NodeChatTemplate ChatTemplate 节点名称
	NodeChatTemplate = "chat_template"

	// NodeLLM LLM 节点名称
	NodeLLM = "llm"

	// NodeLLMSentence LLM 句子处理节点名称
	NodeLLMSentence = "llm_sentence"

	// NodeTTS TTS 节点名称
	NodeTTS = "tts"

	// NodeTTS2Client TTS 到客户端节点名称
	NodeTTS2Client = "tts2client"

	// NodeToolCall 工具调用节点名称
	NodeToolCall = "tool_call"

	// NodeToolCallResult 工具调用结果节点名称
	NodeToolCallResult = "tool_call_result"

	// NodeMerge 合并节点名称
	NodeMerge = "merge"

	// NodeLLMSentenceCollect LLM句子收集节点名称（将流式输出转换为非流式数组）
	NodeLLMSentenceCollect = "llm_sentence_collect"

	// NodePassThrough2 透传节点2名称
	NodePassThrough2 = "pass_through_2"

	// NodeVAD VAD 节点名称
	NodeVAD = "vad"

	// NodeASR ASR 节点名称
	NodeASR = "asr"
)
