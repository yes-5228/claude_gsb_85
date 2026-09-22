package overdue

import (
	"fmt"
	"time"

	"github.com/drainage/desilting/internal/shared/date"
)

// Snapshot 判定任务预警所需的任务快照。
type Snapshot struct {
	Status        string
	PlanStartDate date.Date
	PlanEndDate   date.Date
	FinishedAt    *time.Time
}

// StageCaseSQL 生成判定任务预警阶段的 SQL CASE 表达式，
// 是预警列表、任务列表与看板共用的唯一判定口径。
//
// prefix 为任务表别名（如 "t."），单表查询传空串。
// CASE 分支按阶段严重程度从高到低排列，保证同一任务同一时刻只会落入一个阶段；
// 已取消、已验收的任务不匹配任何分支，自然不参与超期统计。
func StageCaseSQL(prefix string, th Thresholds, today date.Date) (string, []any) {
	notAcceptedBefore := today.AddDays(-th.NotAcceptedDays)
	notReportedBefore := today.AddDays(-th.NotReportedDays)
	notStartedBefore := today.AddDays(-th.NotStartedDays)
	expr := fmt.Sprintf(`CASE
		WHEN %[1]sstatus = '%[2]s' AND %[1]sfinished_at IS NOT NULL AND %[1]sfinished_at < ? THEN '%[3]s'
		WHEN %[1]sstatus IN ('%[4]s', '%[5]s') AND %[1]splan_end_date < ? THEN '%[6]s'
		WHEN %[1]sstatus = '%[4]s' AND %[1]splan_start_date < ? THEN '%[7]s'
		ELSE '' END`,
		prefix,
		taskStatusCompleted, StageNotAccepted,
		taskStatusPending, taskStatusInProgress, StageNotReported,
		StageNotStarted)
	return expr, []any{notAcceptedBefore.Time, notReportedBefore.Time, notStartedBefore.Time}
}

// OverdueDays 计算任务命中阶段后的超期天数（相对该阶段的基准日期），未命中返回 0。
func OverdueDays(stage string, snap Snapshot, today date.Date) int {
	var base date.Date
	switch stage {
	case StageNotStarted:
		base = snap.PlanStartDate
	case StageNotReported:
		base = snap.PlanEndDate
	case StageNotAccepted:
		if snap.FinishedAt == nil {
			return 0
		}
		base = date.New(*snap.FinishedAt)
	default:
		return 0
	}
	if base.IsZero() || !today.After(base) {
		return 0
	}
	return int(today.Time.Sub(base.Time).Hours() / 24)
}
