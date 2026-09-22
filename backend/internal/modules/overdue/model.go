// Package overdue 超期预警模块：把任务超期判断从单一截止日期扩展为分阶段预警。
//
// 三个预警阶段（互斥，按严重程度从高到低匹配）：
//   - not_accepted 未验收：已完工报验，但超过阈值天数仍无验收结论（严重）
//   - not_reported 未报验：计划完成日期已过阈值天数，仍未完工报验（警告）
//   - not_started  未开工：计划开始日期已过阈值天数，仍未开工（提示）
//
// 已取消、已验收的任务不参与超期判定。
// 预警列表、任务列表与看板的超期口径统一由 StageCaseSQL 保证。
package overdue

import (
	"time"

	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/option"
)

// 预警阶段。
const (
	StageNotStarted  = "not_started"  // 计划开始时间已到却迟迟未开工
	StageNotReported = "not_reported" // 计划完成时间已过仍未报验
	StageNotAccepted = "not_accepted" // 报验之后长期没有验收结论
)

// 预警级别：随阶段固定，阶段越靠后级别越高。
const (
	LevelNotice   = "notice"   // 提示
	LevelWarning  = "warning"  // 警告
	LevelCritical = "critical" // 严重
)

// 任务状态取值，与 cleaningtask 模块的 StatusXxx 常量保持一致。
// 这里重复定义以避免模块间循环依赖（与 refx 表名的处理思路相同）。
const (
	taskStatusPending    = "pending"
	taskStatusInProgress = "in_progress"
	taskStatusCompleted  = "completed"
)

// stageLevels 阶段对应的预警级别。
var stageLevels = map[string]string{
	StageNotStarted:  LevelNotice,
	StageNotReported: LevelWarning,
	StageNotAccepted: LevelCritical,
}

// Thresholds 三个阶段的预警阈值（天）：超过阈值天数才判定为超期。
type Thresholds struct {
	NotStartedDays  int `json:"notStartedDays"`
	NotReportedDays int `json:"notReportedDays"`
	NotAcceptedDays int `json:"notAcceptedDays"`
}

// DefaultThresholds 没有任何配置时使用的默认阈值。
var DefaultThresholds = Thresholds{
	NotStartedDays:  3,
	NotReportedDays: 0,
	NotAcceptedDays: 7,
}

// OverdueRule 分阶段预警阈值配置（按生效时间版本化）。
//
// 同一阶段可以存在多条不同生效时间的配置，判定时取生效时间不晚于当天的最新一条；
// 新配置的生效时间不允许早于当天，因此调整只影响之后的判定。
type OverdueRule struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Stage         string    `gorm:"size:24;not null;uniqueIndex:uk_overdue_rules_stage_effective" json:"stage"`
	ThresholdDays int       `gorm:"not null" json:"thresholdDays"`
	EffectiveFrom date.Date `gorm:"type:date;not null;uniqueIndex:uk_overdue_rules_stage_effective" json:"effectiveFrom"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TableName 指定表名。
func (OverdueRule) TableName() string {
	return "overdue_rules"
}

// HasStage 判断是否为合法的预警阶段。
func HasStage(stage string) bool {
	_, ok := stageLevels[stage]
	return ok
}

// LevelOf 返回阶段对应的预警级别。
func LevelOf(stage string) string {
	return stageLevels[stage]
}

// StageOptions 预警阶段选项。
func StageOptions() []option.Option {
	return option.List(
		StageNotStarted, "未开工",
		StageNotReported, "未报验",
		StageNotAccepted, "未验收",
	)
}

// LevelOptions 预警级别选项。
func LevelOptions() []option.Option {
	return option.List(
		LevelNotice, "提示",
		LevelWarning, "警告",
		LevelCritical, "严重",
	)
}
