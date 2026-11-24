package client

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// AlignToolMessages 保证 role:tool 消息紧跟在对应的 role:assistant(tool_calls) 消息之后，
// 且每个保留的 tool_call 都有对应的 tool 响应，避免孤立 tool 消息、未完成 tool_calls
// 或多工具调用时重复追加 assistant 消息导致大模型 API 报错。
func AlignToolMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}

	alignedMessages := make([]*schema.Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg == nil {
			continue
		}

		// 孤立的 tool 消息（未紧跟在匹配的 assistant tool_calls 之后）直接丢弃
		if msg.Role == schema.Tool {
			continue
		}

		if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			// 收集紧随其后的连续 tool 响应消息
			j := i + 1
			toolResponsesByID := make(map[string]*schema.Message)
			for j < len(messages) {
				next := messages[j]
				if next == nil {
					j++
					continue
				}
				if next.Role != schema.Tool {
					break
				}
				callID := strings.TrimSpace(next.ToolCallID)
				if callID != "" {
					if _, exists := toolResponsesByID[callID]; !exists {
						toolResponsesByID[callID] = next
					}
				}
				j++
			}
			// 跳过已扫描的连续 tool 块
			i = j - 1

			matchedCalls := make([]schema.ToolCall, 0, len(msg.ToolCalls))
			matchedTools := make([]*schema.Message, 0, len(msg.ToolCalls))
			seenCallIDs := make(map[string]struct{}, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				callID := strings.TrimSpace(tc.ID)
				if callID == "" {
					continue
				}
				if _, dup := seenCallIDs[callID]; dup {
					continue
				}
				if toolResp, ok := toolResponsesByID[callID]; ok {
					seenCallIDs[callID] = struct{}{}
					matchedCalls = append(matchedCalls, tc)
					matchedTools = append(matchedTools, toolResp)
				}
			}

			if len(matchedCalls) == 0 {
				// 无匹配 tool 响应：若仍有可见文本则剥离 tool_calls 保留文本，否则丢弃
				if strings.TrimSpace(msg.Content) != "" {
					msgCopy := *msg
					msgCopy.ToolCalls = nil
					alignedMessages = append(alignedMessages, &msgCopy)
				}
				continue
			}

			if len(matchedCalls) == len(msg.ToolCalls) {
				alignedMessages = append(alignedMessages, msg)
			} else {
				msgCopy := *msg
				msgCopy.ToolCalls = matchedCalls
				alignedMessages = append(alignedMessages, &msgCopy)
			}
			alignedMessages = append(alignedMessages, matchedTools...)
			continue
		}

		alignedMessages = append(alignedMessages, msg)
	}

	return alignedMessages
}
