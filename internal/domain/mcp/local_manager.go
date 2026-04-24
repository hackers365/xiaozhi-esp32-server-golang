package mcp

import (
	"fmt"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/getkin/kin-openapi/openapi3"

	mcp_protocol "github.com/ThinkInAIXYZ/go-mcp/protocol"
)

// LocalMCPManager 本地MCP工具管理器
type LocalMCPManager struct {
	tools map[string]*McpTool // 工具名称 -> 工具定义
	mu    sync.RWMutex        // 读写锁保护并发访问
}

var (
	localManager *LocalMCPManager
	localOnce    sync.Once
)

// GetLocalMCPManager 获取本地MCP管理器单例
func GetLocalMCPManager() *LocalMCPManager {
	localOnce.Do(func() {
		localManager = &LocalMCPManager{
			tools: make(map[string]*McpTool),
		}
		// 初始化默认的本地工具
		localManager.initDefaultTools()
	})
	return localManager
}

// initDefaultTools 初始化默认的本地工具
func (l *LocalMCPManager) initDefaultTools() {
	if err := l.registerSchedulerTools(); err != nil {
		log.Errorf("注册 scheduler 本地工具失败: %v", err)
	}

	log.Info("本地MCP管理器默认工具初始化完成")
}

// RegisterTool 注册本地工具
func (l *LocalMCPManager) RegisterTool(tool *McpTool) error {
	if tool == nil {
		return fmt.Errorf("工具不能为空")
	}

	if tool.info.Name == "" {
		return fmt.Errorf("工具名称不能为空")
	}

	if !tool.isLocal || tool.localHandler == nil {
		return fmt.Errorf("工具处理函数不能为空")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查工具是否已存在
	if _, exists := l.tools[tool.info.Name]; exists {
		log.Warnf("本地工具 %s 已存在，将被覆盖", tool.info.Name)
	}

	l.tools[tool.info.Name] = tool
	log.Infof("成功注册本地工具: %s - %s", tool.info.Name, tool.info.Desc)
	return nil
}

func (l *LocalMCPManager) convertStructToOpenaipi3Schema(inputParams any) (*openapi3.Schema, error) {
	//使用github.com/ThinkInAIXYZ/go-mcp 通过struct生成 tool, 然后转换成openapi3.Schema
	toolInstance, err := mcp_protocol.NewTool("get_system_info", "获取系统基本信息", inputParams)
	if err != nil {
		return nil, err
	}

	marshaledInputSchema, err := sonic.Marshal(toolInstance.InputSchema)
	if err != nil {
		return nil, err
	}

	inputSchema := &openapi3.Schema{}
	err = sonic.Unmarshal(marshaledInputSchema, inputSchema)
	if err != nil {
		return nil, err
	}
	return inputSchema, nil
}

// RegisterToolFunc 注册工具函数（简化版本）
func (l *LocalMCPManager) RegisterToolFunc(name, description string, inputParams any, handler LocalToolHandler) error {
	inputSchema, err := l.convertStructToOpenaipi3Schema(inputParams)
	if err != nil {
		log.Errorf("Failed to convert struct to openapi3 schema: %v", err)
		return err
	}
	tool := &McpTool{
		info: &schema.ToolInfo{
			Name:        name,
			Desc:        description,
			ParamsOneOf: schema.NewParamsOneOfByOpenAPIV3(inputSchema),
		},
		isLocal:      true,
		localHandler: handler,
	}
	return l.RegisterTool(tool)
}

// UnregisterTool 注销工具
func (l *LocalMCPManager) UnregisterTool(name string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, exists := l.tools[name]; !exists {
		return fmt.Errorf("工具 %s 不存在", name)
	}

	delete(l.tools, name)
	log.Infof("成功注销本地工具: %s", name)
	return nil
}

// GetAllTools 获取所有本地工具，返回Eino工具接口格式
func (l *LocalMCPManager) GetAllTools() map[string]tool.InvokableTool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make(map[string]tool.InvokableTool)
	for name, mcpTool := range l.tools {
		result[name] = mcpTool
	}
	return result
}

// GetToolByName 根据名称获取工具
func (l *LocalMCPManager) GetToolByName(name string) (tool.InvokableTool, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	mcpTool, exists := l.tools[name]
	if !exists {
		return nil, false
	}

	return mcpTool, true
}

// GetToolNames 获取所有工具名称列表
func (l *LocalMCPManager) GetToolNames() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	names := make([]string, 0, len(l.tools))
	for name := range l.tools {
		names = append(names, name)
	}
	return names
}

// GetToolCount 获取工具数量
func (l *LocalMCPManager) GetToolCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.tools)
}

// Start 启动本地管理器（预留接口）
func (l *LocalMCPManager) Start() error {
	log.Info("本地MCP管理器已启动")
	return nil
}

// Stop 停止本地管理器（预留接口）
func (l *LocalMCPManager) Stop() error {
	// 注意：我们不清空工具，因为本地管理器的工具应该在整个应用程序生命周期内保持可用
	// 如果需要清空工具，应该显式调用UnregisterTool方法
	log.Info("本地MCP管理器已停止")
	return nil
}
