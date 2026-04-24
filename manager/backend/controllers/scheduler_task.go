package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
)

type schedulerTaskPayload struct {
	Name         string                 `json:"name" binding:"required"`
	Enabled      *bool                  `json:"enabled"`
	ScheduleType string                 `json:"schedule_type"`
	RunAt        *time.Time             `json:"run_at"`
	CronExpr     string                 `json:"cron_expr"`
	IntervalSec  int64                  `json:"interval_sec"`
	Timezone     string                 `json:"timezone"`
	TaskMode     string                 `json:"task_mode"`
	TaskText     string                 `json:"task_text"`
	ToolName     string                 `json:"tool_name"`
	Arguments    map[string]interface{} `json:"arguments"`
}

type schedulerTaskResponse struct {
	ID           uint                   `json:"id"`
	Name         string                 `json:"name"`
	Enabled      bool                   `json:"enabled"`
	ScheduleType string                 `json:"schedule_type"`
	RunAt        *time.Time             `json:"run_at,omitempty"`
	CronExpr     string                 `json:"cron_expr,omitempty"`
	IntervalSec  int64                  `json:"interval_sec,omitempty"`
	Timezone     string                 `json:"timezone,omitempty"`
	TaskMode     string                 `json:"task_mode"`
	TaskText     string                 `json:"task_text,omitempty"`
	ToolName     string                 `json:"tool_name,omitempty"`
	Arguments    map[string]interface{} `json:"arguments,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

func (ac *AdminController) GetSchedulerTasks(c *gin.Context) {
	var tasks []models.SchedulerTask
	if err := ac.DB.Order("created_at DESC").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取定时任务失败: " + err.Error()})
		return
	}

	ret := make([]schedulerTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		ret = append(ret, toSchedulerTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": ret})
}

func (ac *AdminController) CreateSchedulerTask(c *gin.Context) {
	var req schedulerTaskPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	task, err := normalizeSchedulerTaskPayload(req, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ac.DB.Create(task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建定时任务失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toSchedulerTaskResponse(*task)})
}

func (ac *AdminController) UpdateSchedulerTask(c *gin.Context) {
	var task models.SchedulerTask
	if err := ac.DB.First(&task, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "定时任务不存在"})
		return
	}

	var req schedulerTaskPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	updatedTask, err := normalizeSchedulerTaskPayload(req, &task)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := ac.DB.Save(updatedTask).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新定时任务失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toSchedulerTaskResponse(*updatedTask)})
}

func (ac *AdminController) DeleteSchedulerTask(c *gin.Context) {
	if err := ac.DB.Delete(&models.SchedulerTask{}, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除定时任务失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

func normalizeSchedulerTaskPayload(req schedulerTaskPayload, existing *models.SchedulerTask) (*models.SchedulerTask, error) {
	task := &models.SchedulerTask{}
	if existing != nil {
		*task = *existing
	}

	task.Name = strings.TrimSpace(req.Name)
	if task.Name == "" {
		return nil, fmt.Errorf("name 不能为空")
	}

	scheduleType := strings.ToLower(strings.TrimSpace(req.ScheduleType))
	if scheduleType == "" {
		scheduleType = "once"
	}
	if scheduleType != "once" && scheduleType != "interval" && scheduleType != "cron" {
		return nil, fmt.Errorf("schedule_type 仅支持 once/interval/cron")
	}
	task.ScheduleType = scheduleType
	task.RunAt = req.RunAt
	task.CronExpr = strings.TrimSpace(req.CronExpr)
	task.IntervalSec = req.IntervalSec
	task.Timezone = strings.TrimSpace(req.Timezone)
	if task.Timezone == "" {
		task.Timezone = "Asia/Shanghai"
	}

	taskMode := strings.ToLower(strings.TrimSpace(req.TaskMode))
	if taskMode == "" {
		taskMode = "inject_llm"
	}
	if taskMode != "inject_llm" && taskMode != "mcp_call" {
		return nil, fmt.Errorf("task_mode 仅支持 inject_llm/mcp_call")
	}
	task.TaskMode = taskMode
	task.TaskText = strings.TrimSpace(req.TaskText)
	task.ToolName = strings.TrimSpace(req.ToolName)
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	} else if existing == nil {
		task.Enabled = true
	}

	if req.Arguments != nil {
		raw, err := json.Marshal(req.Arguments)
		if err != nil {
			return nil, err
		}
		task.ArgumentsJSON = string(raw)
	}
	if task.TaskMode == "mcp_call" && task.ToolName == "" {
		return nil, fmt.Errorf("task_mode=mcp_call 时 tool_name 不能为空")
	}
	if task.TaskMode == "inject_llm" && task.TaskText == "" {
		return nil, fmt.Errorf("task_mode=inject_llm 时 task_text 不能为空")
	}
	return task, nil
}

func toSchedulerTaskResponse(task models.SchedulerTask) schedulerTaskResponse {
	resp := schedulerTaskResponse{
		ID:           task.ID,
		Name:         task.Name,
		Enabled:      task.Enabled,
		ScheduleType: task.ScheduleType,
		RunAt:        task.RunAt,
		CronExpr:     task.CronExpr,
		IntervalSec:  task.IntervalSec,
		Timezone:     task.Timezone,
		TaskMode:     task.TaskMode,
		TaskText:     task.TaskText,
		ToolName:     task.ToolName,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
	}
	if strings.TrimSpace(task.ArgumentsJSON) != "" {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(task.ArgumentsJSON), &args); err == nil {
			resp.Arguments = args
		}
	}
	return resp
}
