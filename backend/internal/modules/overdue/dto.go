package overdue

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/shared/date"
)

// SaveRuleRequest 新增阈值配置版本的请求体。
type SaveRuleRequest struct {
	Stage         string    `json:"stage" label:"预警阶段" validate:"required"`
	ThresholdDays int       `json:"thresholdDays" label:"预警阈值"`
	EffectiveFrom date.Date `json:"effectiveFrom" label:"生效日期"`
}

// ListQuery 预警列表查询条件。
type ListQuery struct {
	Stage string
	Page  httpx.PageQuery
}

// ParseListQuery 解析预警列表查询条件。
func ParseListQuery(c *fiber.Ctx) (ListQuery, error) {
	stage := httpx.TrimmedQuery(c, "stage")
	if stage != "" && !HasStage(stage) {
		return ListQuery{}, httpx.BadRequest("预警阶段只能是：not_started / not_reported / not_accepted")
	}
	return ListQuery{Stage: stage, Page: httpx.ParsePage(c)}, nil
}

// warningRow 预警列表的查询行。
type warningRow struct {
	ID            uint
	Code          string
	Title         string
	Status        string
	Priority      string
	TeamName      string
	PlanStartDate date.Date
	PlanEndDate   date.Date
	FinishedAt    *time.Time
	OverdueStage  string
	SegmentCode   string
	SegmentName   string
	District      string
}

// snapshot 提取判定所需的任务快照。
func (r warningRow) snapshot() Snapshot {
	return Snapshot{
		Status:        r.Status,
		PlanStartDate: r.PlanStartDate,
		PlanEndDate:   r.PlanEndDate,
		FinishedAt:    r.FinishedAt,
	}
}

// WarningItem 预警列表项。
type WarningItem struct {
	TaskID        uint       `json:"taskId"`
	Code          string     `json:"code"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	TeamName      string     `json:"teamName"`
	SegmentCode   string     `json:"segmentCode"`
	SegmentName   string     `json:"segmentName"`
	District      string     `json:"district"`
	Stage         string     `json:"stage"`
	Level         string     `json:"level"`
	OverdueDays   int        `json:"overdueDays"`
	PlanStartDate date.Date  `json:"planStartDate"`
	PlanEndDate   date.Date  `json:"planEndDate"`
	FinishedAt    *time.Time `json:"finishedAt"`
}

// Summary 分阶段预警汇总。
type Summary struct {
	Total   int64            `json:"total"`
	ByStage map[string]int64 `json:"byStage"`
}

// CurrentRule 某阶段当前生效的阈值（生效日期为空表示使用系统默认值）。
type CurrentRule struct {
	Stage         string    `json:"stage"`
	Level         string    `json:"level"`
	ThresholdDays int       `json:"thresholdDays"`
	EffectiveFrom date.Date `json:"effectiveFrom"`
}

// RulesOverview 阈值配置总览。
type RulesOverview struct {
	Current  []CurrentRule `json:"current"`
	Versions []OverdueRule `json:"versions"`
}
