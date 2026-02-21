package buildinfo

import (
	"fmt"
	"time"
)

var (
	// Version 由构建参数注入，例如 v1.2.3
	Version = "dev"
	// Commit 由构建参数注入，例如 git commit 短哈希
	Commit = "none"
	// BuildTime 由构建参数注入，建议使用 UTC RFC3339 格式
	BuildTime = "unknown"
)

var startedAt = time.Now()

// StartedAtRFC3339 返回进程启动时间（RFC3339）
func StartedAtRFC3339() string {
	return startedAt.Format(time.RFC3339)
}

// UptimeSeconds 返回进程运行时长（秒）
func UptimeSeconds() int64 {
	return int64(time.Since(startedAt).Seconds())
}

// UptimeText 将秒数格式化为“x天 x小时 x分钟”
func UptimeText(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%d天 %d小时 %d分钟", days, hours, minutes)
}
