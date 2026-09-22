// Package overdue 超期预警模块：把任务超期判断从单一截止日期扩展为分阶段预警。
//
// 三个阶段分别对应任务生命周期的关键节点：
//
//	start（未按期开工）  计划开始日期已过，任务仍停留在待开工
//	finish（未按期报验） 计划完成日期已过，任务仍未完工报验
//	accept（验收超期）   完工报验之后长期没有验收结论
//
// 每个阶段的触发阈值与提示级别由 overdue_rules 配置，规则带生效日期，
// 判定始终使用「判定日当天已生效的最新规则」；预警一旦生成就固化当时的
// 阈值与级别快照，后续调整规则只影响之后的新判定，不回溯历史预警。
//
// 同一条任务在任意时刻最多落入一个阶段（判定互斥），且同一任务同一阶段
// 只生成一次预警（task_id + stage 唯一约束），不会被反复提醒。
// 已取消与已验收的任务不参与超期统计。
package overdue

import (
	"time"

	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/option"
)

// 超期预警阶段。
const (
	StageStart  = "start"  // 未按期开工
	StageFinish = "finish" // 未按期报验
	StageAccept = "accept" // 验收超期
)

// 预警提示级别。
const (
	LevelNotice   = "notice"   // 提醒
	LevelWarning  = "warning"  // 警告
	LevelCritical = "critical" // 严重
)

// 预警处理状态。
const (
	WarningActive   = "active"   // 待处理
	WarningResolved = "resolved" // 已解除
)

// OverdueRule 超期预警规则：某个阶段的触发阈值与提示级别，自带生效日期。
//
// 同一阶段可以存在多条规则，判定取「生效日期不晚于判定日」中最新的一条；
// 生效日期相同的重复配置不允许存在（stage + effective_from 唯一）。
type OverdueRule struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Stage         string    `gorm:"size:16;uniqueIndex:uk_overdue_rule_stage_effective;not null" json:"stage"`
	ThresholdDays int       `gorm:"not null" json:"thresholdDays"`
	Level         string    `gorm:"size:16;not null" json:"level"`
	EffectiveFrom date.Date `gorm:"type:date;uniqueIndex:uk_overdue_rule_stage_effective;not null" json:"effectiveFrom"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TableName 指定表名。
func (OverdueRule) TableName() string {
	return "overdue_rules"
}

// OverdueWarning 超期预警记录。
//
// 预警在扫描时生成并固化当时的规则快照（级别、阈值、首次超期天数），
// 之后规则调整不会改写已生成的预警；任务状态推进或关闭后预警被解除。
type OverdueWarning struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	TaskID        uint       `gorm:"uniqueIndex:uk_overdue_warning_task_stage;not null" json:"taskId"`
	Stage         string     `gorm:"size:16;uniqueIndex:uk_overdue_warning_task_stage;not null" json:"stage"`
	Level         string     `gorm:"size:16;not null" json:"level"`
	ThresholdDays int        `gorm:"not null" json:"thresholdDays"`
	OverdueDays   int        `gorm:"not null" json:"overdueDays"`
	Status        string     `gorm:"size:16;index;not null;default:active" json:"status"`
	ResolveReason string     `gorm:"size:64" json:"resolveReason"`
	ResolvedAt    *time.Time `json:"resolvedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// TableName 指定表名。
func (OverdueWarning) TableName() string {
	return "overdue_warnings"
}

// StageOptions 预警阶段选项。
func StageOptions() []option.Option {
	return option.List(
		StageStart, "未按期开工",
		StageFinish, "未按期报验",
		StageAccept, "验收超期",
	)
}

// LevelOptions 提示级别选项。
func LevelOptions() []option.Option {
	return option.List(
		LevelNotice, "提醒",
		LevelWarning, "警告",
		LevelCritical, "严重",
	)
}

// WarningStatusOptions 预警状态选项。
func WarningStatusOptions() []option.Option {
	return option.List(
		WarningActive, "待处理",
		WarningResolved, "已解除",
	)
}

// StageLabel 返回阶段中文名。
func StageLabel(stage string) string {
	return option.Label(StageOptions(), stage)
}
