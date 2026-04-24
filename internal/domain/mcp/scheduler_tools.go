package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type schedulerMutationInput struct {
	Op      string              `json:"op" jsonschema:"description=操作类型:create/update/delete/enable/disable/run_now,enum=create,enum=update,enum=delete,enum=enable,enum=disable,enum=run_now"`
	JobID   string              `json:"job_id,omitempty" jsonschema:"description=任务ID，update/delete/enable/disable/run_now时必填"`
	Payload schedulerJobPayload `json:"payload,omitempty" jsonschema:"description=任务内容，create/update时必填"`
}

type schedulerJobPayload struct {
	Name         string                 `json:"name,omitempty" jsonschema:"description=任务名称"`
	ScheduleType string                 `json:"schedule_type,omitempty" jsonschema:"description=调度类型:once/interval/cron"`
	RunAt        string                 `json:"run_at,omitempty" jsonschema:"description=一次性任务执行时间, RFC3339"`
	CronExpr     string                 `json:"cron_expr,omitempty" jsonschema:"description=cron表达式"`
	IntervalSec  int64                  `json:"interval_sec,omitempty" jsonschema:"description=周期秒数"`
	Timezone     string                 `json:"timezone,omitempty" jsonschema:"description=时区"`
	TaskMode     string                 `json:"task_mode,omitempty" jsonschema:"description=执行模式 inject_llm/mcp_call"`
	TaskText     string                 `json:"task_text,omitempty" jsonschema:"description=注入到LLM的文本"`
	ToolName     string                 `json:"tool_name,omitempty" jsonschema:"description=MCP工具名"`
	Arguments    map[string]interface{} `json:"arguments,omitempty" jsonschema:"description=MCP工具参数"`
}

type schedulerQueryInput struct{}

type schedulerJob struct {
	JobID        string                 `json:"job_id"`
	Name         string                 `json:"name"`
	Enabled      bool                   `json:"enabled"`
	ScheduleType string                 `json:"schedule_type"`
	RunAt        string                 `json:"run_at,omitempty"`
	CronExpr     string                 `json:"cron_expr,omitempty"`
	IntervalSec  int64                  `json:"interval_sec,omitempty"`
	Timezone     string                 `json:"timezone,omitempty"`
	TaskMode     string                 `json:"task_mode,omitempty"`
	TaskText     string                 `json:"task_text,omitempty"`
	ToolName     string                 `json:"tool_name,omitempty"`
	Arguments    map[string]interface{} `json:"arguments,omitempty"`
	UpdatedAt    string                 `json:"updated_at"`
	LastRunAt    string                 `json:"last_run_at,omitempty"`
}

type schedulerToolStore struct {
	mu   sync.RWMutex
	jobs map[string]schedulerJob
}

var defaultSchedulerStore = &schedulerToolStore{
	jobs: make(map[string]schedulerJob),
}

func (l *LocalMCPManager) registerSchedulerTools() error {
	if err := l.RegisterToolFunc(
		"scheduler_query",
		"查询当前已创建的调度任务数量（无需参数）",
		schedulerQueryInput{},
		handleSchedulerQueryTool,
	); err != nil {
		return err
	}

	if err := l.RegisterToolFunc(
		"scheduler_mutation",
		"统一处理调度任务的新增/更新/删除/启停/立即执行",
		schedulerMutationInput{},
		handleSchedulerMutationTool,
	); err != nil {
		return err
	}
	return nil
}

func handleSchedulerQueryTool(_ context.Context, _ string) (string, error) {
	defaultSchedulerStore.mu.RLock()
	total := len(defaultSchedulerStore.jobs)
	defaultSchedulerStore.mu.RUnlock()

	return marshalToolResult(map[string]interface{}{
		"total":   total,
		"message": fmt.Sprintf("当前共 %d 个调度任务", total),
	}), nil
}

func handleSchedulerMutationTool(_ context.Context, argumentsInJSON string) (string, error) {
	var req schedulerMutationInput
	if strings.TrimSpace(argumentsInJSON) != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &req); err != nil {
			return "", fmt.Errorf("解析参数失败: %w", err)
		}
	}

	req.Op = strings.ToLower(strings.TrimSpace(req.Op))
	switch req.Op {
	case "create":
		return handleSchedulerCreate(req)
	case "update":
		return handleSchedulerUpdate(req)
	case "delete":
		return handleSchedulerDelete(req)
	case "enable":
		return handleSchedulerEnableDisable(req, true)
	case "disable":
		return handleSchedulerEnableDisable(req, false)
	case "run_now":
		return handleSchedulerRunNow(req)
	default:
		return "", fmt.Errorf("不支持的 op: %s", req.Op)
	}
}

func handleSchedulerCreate(req schedulerMutationInput) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		jobID = fmt.Sprintf("job_%d", time.Now().UnixNano())
	}

	job := schedulerJob{
		JobID:        jobID,
		Name:         strings.TrimSpace(req.Payload.Name),
		Enabled:      true,
		ScheduleType: strings.TrimSpace(req.Payload.ScheduleType),
		RunAt:        strings.TrimSpace(req.Payload.RunAt),
		CronExpr:     strings.TrimSpace(req.Payload.CronExpr),
		IntervalSec:  req.Payload.IntervalSec,
		Timezone:     strings.TrimSpace(req.Payload.Timezone),
		TaskMode:     strings.TrimSpace(req.Payload.TaskMode),
		TaskText:     strings.TrimSpace(req.Payload.TaskText),
		ToolName:     strings.TrimSpace(req.Payload.ToolName),
		Arguments:    req.Payload.Arguments,
		UpdatedAt:    now,
	}

	defaultSchedulerStore.mu.Lock()
	defer defaultSchedulerStore.mu.Unlock()
	if _, exists := defaultSchedulerStore.jobs[jobID]; exists {
		return "", fmt.Errorf("任务已存在: %s", jobID)
	}
	defaultSchedulerStore.jobs[jobID] = job

	return marshalToolResult(map[string]interface{}{
		"ok":     true,
		"op":     "create",
		"job_id": jobID,
	}), nil
}

func handleSchedulerUpdate(req schedulerMutationInput) (string, error) {
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return "", fmt.Errorf("job_id 不能为空")
	}

	defaultSchedulerStore.mu.Lock()
	defer defaultSchedulerStore.mu.Unlock()
	job, exists := defaultSchedulerStore.jobs[jobID]
	if !exists {
		return "", fmt.Errorf("任务不存在: %s", jobID)
	}

	if name := strings.TrimSpace(req.Payload.Name); name != "" {
		job.Name = name
	}
	if scheduleType := strings.TrimSpace(req.Payload.ScheduleType); scheduleType != "" {
		job.ScheduleType = scheduleType
	}
	if runAt := strings.TrimSpace(req.Payload.RunAt); runAt != "" {
		job.RunAt = runAt
	}
	if cronExpr := strings.TrimSpace(req.Payload.CronExpr); cronExpr != "" {
		job.CronExpr = cronExpr
	}
	if req.Payload.IntervalSec > 0 {
		job.IntervalSec = req.Payload.IntervalSec
	}
	if timezone := strings.TrimSpace(req.Payload.Timezone); timezone != "" {
		job.Timezone = timezone
	}
	if taskMode := strings.TrimSpace(req.Payload.TaskMode); taskMode != "" {
		job.TaskMode = taskMode
	}
	if taskText := strings.TrimSpace(req.Payload.TaskText); taskText != "" {
		job.TaskText = taskText
	}
	if toolName := strings.TrimSpace(req.Payload.ToolName); toolName != "" {
		job.ToolName = toolName
	}
	if req.Payload.Arguments != nil {
		job.Arguments = req.Payload.Arguments
	}
	job.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	defaultSchedulerStore.jobs[jobID] = job

	return marshalToolResult(map[string]interface{}{
		"ok":     true,
		"op":     "update",
		"job_id": jobID,
	}), nil
}

func handleSchedulerDelete(req schedulerMutationInput) (string, error) {
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return "", fmt.Errorf("job_id 不能为空")
	}

	defaultSchedulerStore.mu.Lock()
	defer defaultSchedulerStore.mu.Unlock()
	if _, exists := defaultSchedulerStore.jobs[jobID]; !exists {
		return "", fmt.Errorf("任务不存在: %s", jobID)
	}
	delete(defaultSchedulerStore.jobs, jobID)

	return marshalToolResult(map[string]interface{}{
		"ok":     true,
		"op":     "delete",
		"job_id": jobID,
	}), nil
}

func handleSchedulerEnableDisable(req schedulerMutationInput, enabled bool) (string, error) {
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return "", fmt.Errorf("job_id 不能为空")
	}

	defaultSchedulerStore.mu.Lock()
	defer defaultSchedulerStore.mu.Unlock()
	job, exists := defaultSchedulerStore.jobs[jobID]
	if !exists {
		return "", fmt.Errorf("任务不存在: %s", jobID)
	}
	job.Enabled = enabled
	job.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	defaultSchedulerStore.jobs[jobID] = job

	op := "disable"
	if enabled {
		op = "enable"
	}

	return marshalToolResult(map[string]interface{}{
		"ok":      true,
		"op":      op,
		"job_id":  jobID,
		"enabled": enabled,
	}), nil
}

func handleSchedulerRunNow(req schedulerMutationInput) (string, error) {
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return "", fmt.Errorf("job_id 不能为空")
	}

	defaultSchedulerStore.mu.Lock()
	defer defaultSchedulerStore.mu.Unlock()
	job, exists := defaultSchedulerStore.jobs[jobID]
	if !exists {
		return "", fmt.Errorf("任务不存在: %s", jobID)
	}

	job.LastRunAt = time.Now().UTC().Format(time.RFC3339)
	job.UpdatedAt = job.LastRunAt
	defaultSchedulerStore.jobs[jobID] = job

	return marshalToolResult(map[string]interface{}{
		"ok":          true,
		"op":          "run_now",
		"job_id":      jobID,
		"last_run_at": job.LastRunAt,
	}), nil
}

func marshalToolResult(v map[string]interface{}) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
