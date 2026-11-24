package client

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestAlignToolMessages_MultiToolCallsNotDuplicated(t *testing.T) {
	user := schema.UserMessage("查上海和北京天气")
	assistantCall := schema.AssistantMessage("", []schema.ToolCall{
		{ID: "call_sh", Function: schema.FunctionCall{Name: "weather", Arguments: `{"city":"sh"}`}},
		{ID: "call_bj", Function: schema.FunctionCall{Name: "weather", Arguments: `{"city":"bj"}`}},
	})
	toolSH := schema.ToolMessage("上海22度", "call_sh")
	toolBJ := schema.ToolMessage("北京18度", "call_bj")
	finalReply := schema.AssistantMessage("上海22度，北京18度。", nil)

	aligned := AlignToolMessages([]*schema.Message{user, assistantCall, toolSH, toolBJ, finalReply})
	if len(aligned) != 5 {
		t.Fatalf("len(aligned) = %d, want 5", len(aligned))
	}
	if aligned[0].Role != schema.User ||
		aligned[1].Role != schema.Assistant || len(aligned[1].ToolCalls) != 2 ||
		aligned[2].Role != schema.Tool || aligned[2].ToolCallID != "call_sh" ||
		aligned[3].Role != schema.Tool || aligned[3].ToolCallID != "call_bj" ||
		aligned[4].Role != schema.Assistant || aligned[4].Content != "上海22度，北京18度。" {
		t.Fatalf("unexpected aligned order: %+v", aligned)
	}
}

func TestAlignToolMessages_DropsOrphanAndOutOfOrderToolMessages(t *testing.T) {
	orphanTool := schema.ToolMessage("stale result", "call_missing")
	user1 := schema.UserMessage("第一轮")
	assistantCall := schema.AssistantMessage("", []schema.ToolCall{
		{ID: "call_1", Function: schema.FunctionCall{Name: "get_time", Arguments: "{}"}},
	})
	user2 := schema.UserMessage("第二轮打断了")
	lateTool := schema.ToolMessage("12:00", "call_1")
	reply2 := schema.AssistantMessage("好的", nil)

	aligned := AlignToolMessages([]*schema.Message{orphanTool, user1, assistantCall, user2, lateTool, reply2})
	if len(aligned) != 3 {
		t.Fatalf("len(aligned) = %d, want 3 (user1, user2, reply2); got=%+v", len(aligned), aligned)
	}
	if aligned[0] != user1 || aligned[1] != user2 || aligned[2] != reply2 {
		t.Fatalf("unexpected aligned messages: %+v", aligned)
	}
}

func TestAlignToolMessages_PartialMatchAndTextFallback(t *testing.T) {
	user := schema.UserMessage("帮我查一下")
	assistantPartial := schema.AssistantMessage("", []schema.ToolCall{
		{ID: "call_1", Function: schema.FunctionCall{Name: "a", Arguments: "{}"}},
		{ID: "call_2", Function: schema.FunctionCall{Name: "b", Arguments: "{}"}},
	})
	tool1 := schema.ToolMessage("result_1", "call_1")
	assistantTextWithUnmatched := schema.AssistantMessage("正在为你查询", []schema.ToolCall{
		{ID: "call_3", Function: schema.FunctionCall{Name: "c", Arguments: "{}"}},
	})

	aligned := AlignToolMessages([]*schema.Message{user, assistantPartial, tool1, assistantTextWithUnmatched})
	if len(aligned) != 4 {
		t.Fatalf("len(aligned) = %d, want 4; got=%+v", len(aligned), aligned)
	}
	if len(aligned[1].ToolCalls) != 1 || aligned[1].ToolCalls[0].ID != "call_1" {
		t.Fatalf("expected assistantPartial to keep only call_1, got %+v", aligned[1].ToolCalls)
	}
	if len(assistantPartial.ToolCalls) != 2 {
		t.Fatalf("original assistantPartial message must not be mutated")
	}
	if aligned[2] != tool1 {
		t.Fatalf("expected tool1 at index 2")
	}
	if aligned[3].Role != schema.Assistant || aligned[3].Content != "正在为你查询" || len(aligned[3].ToolCalls) != 0 {
		t.Fatalf("expected assistantTextWithUnmatched to preserve text without tool_calls, got %+v", aligned[3])
	}
}
