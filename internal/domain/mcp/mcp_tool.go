package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

var callRemoteMCPTool = func(ctx context.Context, cli *client.Client, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return cli.CallTool(ctx, request)
}

var reconnectGlobalMCPServer = func(serverName string) (*client.Client, error) {
	return GetGlobalMCPManager().reconnectServer(serverName)
}

// LocalToolHandler 本地工具处理函数类型
type LocalToolHandler func(ctx context.Context, argumentsInJSON string) (string, error)

// mcpTool MCP工具实现，支持远程和本地工具
type McpTool struct {
	info       *schema.ToolInfo
	originName string
	serverName string
	client     *client.Client
	requestMu  *sync.Mutex

	// 本地工具支持
	isLocal      bool
	localHandler LocalToolHandler
}

// Info 获取工具信息，实现BaseTool接口
func (t *McpTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *McpTool) callName() string {
	if t.originName != "" {
		return t.originName
	}
	if t.info != nil {
		return t.info.Name
	}
	return ""
}

func mcpToolMatchesName(invokable tool.InvokableTool, name string) bool {
	mcpTool, ok := invokable.(*McpTool)
	if !ok || mcpTool == nil {
		return false
	}
	if mcpTool.info != nil && mcpTool.info.Name == name {
		return true
	}
	return mcpTool.originName != "" && mcpTool.originName == name
}

func findInvokableToolByName(tools map[string]tool.InvokableTool, name string) (tool.InvokableTool, bool) {
	if invokable, ok := tools[name]; ok {
		return invokable, true
	}
	for _, invokable := range tools {
		if mcpToolMatchesName(invokable, name) {
			return invokable, true
		}
	}
	return nil, false
}

func remoteCallNameForTool(invokable tool.InvokableTool, fallback string) string {
	if mcpTool, ok := invokable.(*McpTool); ok && mcpTool != nil {
		if name := mcpTool.callName(); name != "" {
			return name
		}
	}
	return fallback
}

func (t *McpTool) InvokeableLocalRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	toolInfo := t.info
	if t.localHandler == nil {
		return "", fmt.Errorf("本地工具 %s 的处理函数未定义", toolInfo.Name)
	}

	log.Infof("执行本地工具: %s, 参数: %s", toolInfo.Name, argumentsInJSON)

	resultStr, err := t.localHandler(ctx, argumentsInJSON)
	if err != nil {
		log.Errorf("本地工具 %s 执行失败: %v", toolInfo.Name, err)
		return "", fmt.Errorf("本地工具执行失败: %v", err)
	}
	if len(resultStr) > 2048 {
		log.Infof("本地工具 %s 执行成功，结果长度: %d", toolInfo.Name, len(resultStr))
	} else {
		log.Infof("本地工具 %s 执行成功，结果: %+s", toolInfo.Name, resultStr)
	}

	return resultStr, nil
}

// InvokableRun 调用工具，实现InvokableTool接口
func (t *McpTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 如果是本地工具，直接调用本地处理函数
	if t.isLocal {
		return t.InvokeableLocalRun(ctx, argumentsInJSON, opts...)
	}

	retContent := ""

	// 远程MCP工具调用逻辑
	// 检查客户端是否可用
	if t.client == nil {
		return retContent, fmt.Errorf("调用MCP工具失败: MCP客户端未初始化")
	}

	// 解析参数
	var arguments map[string]interface{}
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &arguments); err != nil {
			return retContent, fmt.Errorf("解析工具参数失败: %v", err)
		}
	}

	// 准备调用请求
	toolName := t.callName()
	callRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	}

	result, err := t.callRemoteTool(ctx, callRequest)
	if err != nil {
		if !isRetryableRemoteCallError(err) {
			return retContent, fmt.Errorf("调用工具失败: %v", err)
		}

		log.Warnf("工具 %s 调用失败，准备重连服务器 %s 后重试: %v", t.info.Name, t.serverName, err)

		newClient, reconnectErr := reconnectGlobalMCPServer(t.serverName)
		if reconnectErr != nil {
			return retContent, fmt.Errorf("调用工具失败: %v，且重连服务器失败: %v", err, reconnectErr)
		}

		t.client = newClient
		result, err = t.callRemoteTool(ctx, callRequest)
		if err != nil {
			return retContent, fmt.Errorf("重连后调用仍然失败: %v", err)
		}
	}

	if err != nil {
		return retContent, fmt.Errorf("调用工具失败: %v", err)
	}

	resultStr, err := result.MarshalJSON()
	if err != nil {
		return retContent, fmt.Errorf("工具调用返回内容转换失败: %v", err)
	}

	return string(resultStr), nil
}

func (t *McpTool) callRemoteTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if t.requestMu != nil {
		t.requestMu.Lock()
		defer t.requestMu.Unlock()
	}
	if t.client == nil {
		return nil, fmt.Errorf("MCP客户端未初始化")
	}
	return callRemoteMCPTool(ctx, t.client, request)
}

func (t *McpTool) GetClient() *client.Client {
	return t.client
}

func (t *McpTool) GetServerName() string {
	return t.serverName
}
