package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/yangkun19921001/go-nanobot/cron"
)

// CronTool 定时任务工具 (对标 agent/tools/cron.py:CronTool)
type CronTool struct {
	CronService *cron.Service
	channel     string
	chatID      string
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Schedule reminders and recurring tasks. Actions: add, list, remove."
}

func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []any{"add", "list", "remove"},
				"description": "Action to perform",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Reminder message (for add)",
			},
			"every_seconds": map[string]any{
				"type":        "integer",
				"description": "Interval in seconds (for recurring tasks)",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression like '0 9 * * *' (for scheduled tasks)",
			},
			"tz": map[string]any{
				"type":        "string",
				"description": "IANA timezone for cron expressions (e.g. 'America/Vancouver')",
			},
			"at": map[string]any{
				"type":        "string",
				"description": "ISO datetime for one-time execution (e.g. '2026-02-12T10:30:00')",
			},
			"job_id": map[string]any{
				"type":        "string",
				"description": "Job ID (for remove)",
			},
		},
		"required": []any{"action"},
	}
}

func (t *CronTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)
	message, _ := params["message"].(string)
	cronExpr, _ := params["cron_expr"].(string)
	tz, _ := params["tz"].(string)
	at, _ := params["at"].(string)
	jobID, _ := params["job_id"].(string)

	// every_seconds can be float64 from JSON
	var everySeconds int64
	if v, ok := params["every_seconds"].(float64); ok {
		everySeconds = int64(v)
	}

	switch action {
	case "add":
		return t.addJob(message, everySeconds, cronExpr, tz, at)
	case "list":
		return t.listJobs()
	case "remove":
		return t.removeJob(jobID)
	default:
		return fmt.Sprintf("Unknown action: %s", action), nil
	}
}

// addJob 添加任务 (对标 cron.py:_add_job)
func (t *CronTool) addJob(message string, everySeconds int64, cronExpr, tz, at string) (string, error) {
	if message == "" {
		return "Error: message is required for add", nil
	}
	if t.channel == "" || t.chatID == "" {
		return "Error: no session context (channel/chat_id)", nil
	}
	if tz != "" && cronExpr == "" {
		return "Error: tz can only be used with cron_expr", nil
	}

	if t.CronService == nil {
		return "Error: cron service not configured", nil
	}

	var schedule cron.CronSchedule
	deleteAfter := false

	switch {
	case everySeconds > 0:
		schedule = cron.CronSchedule{Kind: "every", EveryMs: everySeconds * 1000}
	case cronExpr != "":
		schedule = cron.CronSchedule{Kind: "cron", Expr: cronExpr, Tz: tz}
	case at != "":
		atMs, err := parseISOTime(at)
		if err != nil {
			return fmt.Sprintf("Error: invalid datetime '%s': %s", at, err.Error()), nil
		}
		schedule = cron.CronSchedule{Kind: "at", AtMs: atMs}
		deleteAfter = true
	default:
		return "Error: either every_seconds, cron_expr, or at is required", nil
	}

	name := message
	if len(name) > 30 {
		name = name[:30]
	}

	job := t.CronService.AddJob(name, schedule, message, true, t.channel, t.chatID, deleteAfter)
	return fmt.Sprintf("Created job '%s' (id: %s)", job.Name, job.ID), nil
}

// listJobs 列出任务 (对标 cron.py:_list_jobs)
func (t *CronTool) listJobs() (string, error) {
	if t.CronService == nil {
		return "Error: cron service not configured", nil
	}
	jobs := t.CronService.ListJobs(false)
	if len(jobs) == 0 {
		return "No scheduled jobs.", nil
	}
	result := "Scheduled jobs:\n"
	for _, j := range jobs {
		result += fmt.Sprintf("- %s (id: %s, %s)\n", j.Name, j.ID, j.Schedule.Kind)
	}
	return result, nil
}

// removeJob 删除任务 (对标 cron.py:_remove_job)
func (t *CronTool) removeJob(jobID string) (string, error) {
	if jobID == "" {
		return "Error: job_id is required for remove", nil
	}
	if t.CronService == nil {
		return "Error: cron service not configured", nil
	}
	if t.CronService.RemoveJob(jobID) {
		return fmt.Sprintf("Removed job %s", jobID), nil
	}
	return fmt.Sprintf("Job %s not found", jobID), nil
}

// SetContext 实现 ContextSetter 接口
func (t *CronTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

// parseISOTime 解析 ISO 时间字符串为毫秒时间戳
func parseISOTime(s string) (int64, error) {
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t.UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("cannot parse time: %s", s)
}

var _ Tool = (*CronTool)(nil)
