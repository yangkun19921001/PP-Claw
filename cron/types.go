package cron

// CronSchedule 调度定义 (对标 cron/types.py:CronSchedule)
type CronSchedule struct {
	Kind    string `json:"kind"`    // "at", "every", "cron"
	AtMs    int64  `json:"atMs"`    // 用于 "at": 时间戳 ms
	EveryMs int64  `json:"everyMs"` // 用于 "every": 间隔 ms
	Expr    string `json:"expr"`    // 用于 "cron": cron 表达式
	Tz      string `json:"tz"`      // 用于 "cron": 时区
}

// CronPayload 任务执行内容 (对标 cron/types.py:CronPayload)
type CronPayload struct {
	Kind    string `json:"kind"`    // "system_event" 或 "agent_turn"
	Message string `json:"message"` // 消息内容
	Deliver bool   `json:"deliver"` // 是否投递到渠道
	Channel string `json:"channel"` // 目标渠道
	To      string `json:"to"`      // 目标用户
}

// CronJobState 任务运行状态 (对标 cron/types.py:CronJobState)
type CronJobState struct {
	NextRunAtMs int64  `json:"nextRunAtMs"`
	LastRunAtMs int64  `json:"lastRunAtMs"`
	LastStatus  string `json:"lastStatus"` // "ok", "error", "skipped"
	LastError   string `json:"lastError"`
}

// CronJob 定时任务 (对标 cron/types.py:CronJob)
type CronJob struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Enabled        bool         `json:"enabled"`
	Schedule       CronSchedule `json:"schedule"`
	Payload        CronPayload  `json:"payload"`
	State          CronJobState `json:"state"`
	CreatedAtMs    int64        `json:"createdAtMs"`
	UpdatedAtMs    int64        `json:"updatedAtMs"`
	DeleteAfterRun bool         `json:"deleteAfterRun"`
}

// CronStore 持久化存储 (对标 cron/types.py:CronStore)
type CronStore struct {
	Version int       `json:"version"`
	Jobs    []CronJob `json:"jobs"`
}
