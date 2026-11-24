package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	data_client "xiaozhi-esp32-server-golang/internal/data/client"
	config_types "xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"

	"github.com/cloudwego/eino/schema"
	mcp_go "github.com/mark3labs/mcp-go/mcp"
)

func TestHandleToolResultAcceptsPlainText(t *testing.T) {
	manager := &LLMManager{}

	result, ok := manager.handleToolResult("普通文本返回")
	if !ok {
		t.Fatal("expected plain text tool result to be accepted")
	}

	if result.IsError {
		t.Fatal("expected plain text tool result not to be marked as error")
	}

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(mcp_go.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}

	if textContent.Text != "普通文本返回" {
		t.Fatalf("expected original text to be preserved, got %q", textContent.Text)
	}
}

func TestHandleToolResultAcceptsMCPJSON(t *testing.T) {
	manager := &LLMManager{}

	result, ok := manager.handleToolResult(`{"content":[{"type":"text","text":"json返回"}],"isError":false}`)
	if !ok {
		t.Fatal("expected MCP JSON tool result to be accepted")
	}

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(mcp_go.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}

	if textContent.Text != "json返回" {
		t.Fatalf("expected parsed text content, got %q", textContent.Text)
	}
}

func TestGetMessagesUsesToolRoundMessagesInNoneMode(t *testing.T) {
	manager := newTestLLMManager(data_client.MemoryModeNone)
	user := schema.UserMessage("帮我查一下上海天气")
	assistant := schema.AssistantMessage("", []schema.ToolCall{
		{
			ID:   "call_weather_1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "get_weather",
				Arguments: `{"city":"Shanghai"}`,
			},
		},
	})
	toolMsg := schema.ToolMessage("上海今天多云，22度", "call_weather_1")

	ctx := appendToolRoundMessagesToContext(context.Background(), []*schema.Message{user, assistant, toolMsg})
	messages := manager.GetMessages(ctx, nil, 10, nil)

	if len(messages) != 4 {
		t.Fatalf("expected system + 3 tool-round messages, got %d", len(messages))
	}
	if messages[1].Role != schema.User || messages[1].Content != user.Content {
		t.Fatalf("expected tool-round user message to be forwarded, got role=%s content=%q", messages[1].Role, messages[1].Content)
	}
	if messages[2].Role != schema.Assistant || len(messages[2].ToolCalls) != 1 || messages[2].ToolCalls[0].ID != "call_weather_1" {
		t.Fatalf("expected assistant tool call message to be forwarded, got %+v", messages[2])
	}
	if messages[3].Role != schema.Tool || messages[3].ToolCallID != "call_weather_1" || messages[3].Content != toolMsg.Content {
		t.Fatalf("expected tool result message to be forwarded, got %+v", messages[3])
	}
}

func TestAppendToolRoundMessagesAccumulatesInOrder(t *testing.T) {
	user := schema.UserMessage("帮我订闹钟")
	assistant := schema.AssistantMessage("", []schema.ToolCall{
		{
			ID:   "call_alarm_1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "set_alarm",
				Arguments: `{"time":"07:30"}`,
			},
		},
	})
	toolMsg := schema.ToolMessage("闹钟已设置为早上7点半", "call_alarm_1")

	ctx := context.Background()
	ctx = appendToolRoundMessagesToContext(ctx, []*schema.Message{user})
	ctx = appendToolRoundMessagesToContext(ctx, []*schema.Message{assistant, toolMsg})

	messages := toolRoundMessagesFromContext(ctx)
	if len(messages) != 3 {
		t.Fatalf("expected 3 accumulated tool-round messages, got %d", len(messages))
	}
	if messages[0].Role != schema.User || messages[1].Role != schema.Assistant || messages[2].Role != schema.Tool {
		t.Fatalf("expected user -> assistant -> tool order, got roles %s -> %s -> %s", messages[0].Role, messages[1].Role, messages[2].Role)
	}
}

func TestGetMessagesIgnoresToolRoundMessagesOutsideNoneMode(t *testing.T) {
	manager := newTestLLMManager(data_client.MemoryModeShort)
	historyUser := schema.UserMessage("历史消息")
	manager.clientState.AddMessage(historyUser)

	ctx := appendToolRoundMessagesToContext(context.Background(), []*schema.Message{
		schema.UserMessage("工具链临时消息"),
	})
	messages := manager.GetMessages(ctx, nil, 10, nil)

	if len(messages) != 2 {
		t.Fatalf("expected system + history message, got %d", len(messages))
	}
	if messages[1].Content != historyUser.Content {
		t.Fatalf("expected short memory mode to keep dialogue history, got %q", messages[1].Content)
	}
}

func TestTTSTurnTrackerWaitBlocksUntilAllDone(t *testing.T) {
	tracker := newTTSTurnTracker()
	done1 := tracker.Add()
	done2 := tracker.Add()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- tracker.Wait(ctx)
	}()

	select {
	case err := <-waitDone:
		t.Fatalf("wait returned too early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	done1(nil)

	select {
	case err := <-waitDone:
		t.Fatalf("wait returned before final TTS item completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	done2(nil)

	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("wait returned unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not return after all TTS items completed")
	}
}

func TestTTSTurnTrackerWaitHonorsContextCancellation(t *testing.T) {
	tracker := newTTSTurnTracker()
	_ = tracker.Add()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tracker.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestWaitForTTSTurnDrainIfRootSkipsNestedContexts(t *testing.T) {
	ctx := ensureTTSTurnTrackerInContext(context.Background())
	tracker := ttsTurnTrackerFromContext(ctx)
	if tracker == nil {
		t.Fatal("expected tracker to be stored in context")
	}

	done := tracker.Add()
	defer done(nil)

	nestedCtx := context.WithValue(ctx, "nest", 2)
	waitCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := waitForTTSTurnDrainIfRoot(nestedCtx); err != nil {
		t.Fatalf("expected nested context wait to be skipped, got %v", err)
	}

	rootWaitDone := make(chan error, 1)
	go func() {
		rootWaitDone <- waitForTTSTurnDrainIfRoot(context.WithValue(ctx, "nest", 1))
	}()

	select {
	case err := <-rootWaitDone:
		t.Fatalf("root wait returned before TTS item completion: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	done(nil)

	select {
	case err := <-rootWaitDone:
		if err != nil {
			t.Fatalf("root wait returned unexpected error: %v", err)
		}
	case <-waitCtx.Done():
		t.Fatal("root wait did not finish after TTS item completion")
	}
}

func TestAddMessageGeneratesUniqueIDsForBurstToolMessages(t *testing.T) {
	manager := newTestLLMManager(data_client.MemoryModeShort)
	manager.clientState.SessionID = "sess-burst-tool"
	manager.lastMessageID = make(map[string]string)

	idCh := make(chan string, 4)
	handler := func(evt *eventbus.AddMessageEvent) {
		if evt != nil && evt.ClientState == manager.clientState {
			idCh <- evt.MessageID
		}
	}
	if err := eventbus.Get().Subscribe(eventbus.TopicAddMessage, handler); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer func() {
		_ = eventbus.Get().Unsubscribe(eventbus.TopicAddMessage, handler)
	}()

	ctx := context.Background()
	if err := manager.AddMessage(ctx, schema.ToolMessage("result-1", "call_1")); err != nil {
		t.Fatalf("AddMessage tool 1 failed: %v", err)
	}
	if err := manager.AddMessage(ctx, schema.ToolMessage("result-2", "call_2")); err != nil {
		t.Fatalf("AddMessage tool 2 failed: %v", err)
	}

	var ids []string
	for len(ids) < 2 {
		select {
		case id := <-idCh:
			ids = append(ids, id)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for AddMessage events, got %d", len(ids))
		}
	}
	if ids[0] == "" || ids[1] == "" || ids[0] == ids[1] {
		t.Fatalf("expected distinct non-empty message IDs for burst tool messages, got %q and %q", ids[0], ids[1])
	}
}

func newTestLLMManager(memoryMode string) *LLMManager {
	return &LLMManager{
		clientState: &data_client.ClientState{
			Dialogue: &data_client.Dialogue{},
			DeviceConfig: config_types.UConfig{
				MemoryMode: memoryMode,
			},
			SystemPrompt: "你是一个测试助手",
		},
	}
}
