package overdue

import (
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/shared/date"
)

// Evaluate 判定单条任务在指定日期下落入哪个超期阶段。
//
// 判定互斥：一条任务最多命中一个阶段。待开工任务若同时越过开工与报验
// 阈值，按更靠后的「未按期报验」阶段提示。已取消、已验收的任务不参与
// 判定，直接返回 ok=false。某个阶段没有生效规则时该阶段不启用。
func Evaluate(task *cleaningtask.CleaningTask, rules map[string]OverdueRule, today date.Date) (stage string, days int, ok bool) {
	switch task.Status {
	case cleaningtask.StatusPending:
		if rule, enabled := rules[StageFinish]; enabled && exceeds(task.PlanEndDate, rule.ThresholdDays, today) {
			return StageFinish, DaysOver(task.PlanEndDate, rule.ThresholdDays, today), true
		}
		if rule, enabled := rules[StageStart]; enabled && exceeds(task.PlanStartDate, rule.ThresholdDays, today) {
			return StageStart, DaysOver(task.PlanStartDate, rule.ThresholdDays, today), true
		}
	case cleaningtask.StatusInProgress:
		if rule, enabled := rules[StageFinish]; enabled && exceeds(task.PlanEndDate, rule.ThresholdDays, today) {
			return StageFinish, DaysOver(task.PlanEndDate, rule.ThresholdDays, today), true
		}
	case cleaningtask.StatusCompleted:
		rule, enabled := rules[StageAccept]
		if enabled && task.FinishedAt != nil {
			finished := date.New(*task.FinishedAt)
			if exceeds(finished, rule.ThresholdDays, today) {
				return StageAccept, DaysOver(finished, rule.ThresholdDays, today), true
			}
		}
	}
	return "", 0, false
}

// DaysOver 计算相对 基准日期 + 阈值天数 已经超出的天数，未超期时返回 0。
func DaysOver(base date.Date, thresholdDays int, today date.Date) int {
	if base.IsZero() {
		return 0
	}
	days := int(today.Time.Sub(base.AddDays(thresholdDays).Time).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// exceeds 判断 基准日期 + 阈值天数 是否早于判定日（即已触发超期）。
func exceeds(base date.Date, thresholdDays int, today date.Date) bool {
	if base.IsZero() {
		return false
	}
	return base.AddDays(thresholdDays).Before(today)
}
