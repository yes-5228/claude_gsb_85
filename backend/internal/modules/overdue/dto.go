package overdue

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/option"
)

// RuleSaveRequest 新增预警规则的请求体。
type RuleSaveRequest struct {
	Stage         string    `json:"stage" label:"预警阶段" validate:"required"`
	ThresholdDays int       `json:"thresholdDays" label:"触发阈值（天）" validate:"gte=0,lte=365"`
	Level         string    `json:"level" label:"提示级别" validate:"required"`
	EffectiveFrom date.Date `json:"effectiveFrom" label:"生效日期"`
}

// WarningListQuery 预警列表查询条件。
type WarningListQuery struct {
	Stage   string
	Level   string
	Status  string
	Keyword string
	Page    httpx.PageQuery
}

// ParseWarningListQuery 解析预警列表查询条件，状态默认只看待处理。
func ParseWarningListQuery(c *fiber.Ctx) WarningListQuery {
	query := WarningListQuery{
		Stage:   httpx.TrimmedQuery(c, "stage"),
		Level:   httpx.TrimmedQuery(c, "level"),
		Status:  httpx.TrimmedQuery(c, "status"),
		Keyword: strings.ToLower(httpx.TrimmedQuery(c, "keyword")),
		Page:    httpx.ParsePage(c),
	}
	if query.Status == "" {
		query.Status = WarningActive
	}
	return query
}

// WarningItem 预警列表项：预警本体 + 任务与管段信息 + 当前超期天数。
type WarningItem struct {
	ID            uint       `json:"id"`
	TaskID        uint       `json:"taskId"`
	TaskCode      string     `json:"taskCode"`
	TaskTitle     string     `json:"taskTitle"`
	TaskStatus    string     `json:"taskStatus"`
	SegmentCode   string     `json:"segmentCode"`
	SegmentName   string     `json:"segmentName"`
	District      string     `json:"district"`
	Stage         string     `json:"stage"`
	Level         string     `json:"level"`
	ThresholdDays int        `json:"thresholdDays"`
	OverdueDays   int        `json:"overdueDays"`
	Status        string     `json:"status"`
	ResolveReason string     `json:"resolveReason"`
	CreatedAt     time.Time  `json:"createdAt"`
	ResolvedAt    *time.Time `json:"resolvedAt"`
}

// Summary 待处理预警的分阶段统计，看板、预警列表与任务列表共用同一口径。
type Summary struct {
	Total   int64            `json:"total"`
	ByStage map[string]int64 `json:"byStage"`
	ByLevel map[string]int64 `json:"byLevel"`
}

// toItem 把联表行转换为输出结构，并按当前日期重算超期天数。
func (row warningRow) toItem(today date.Date) WarningItem {
	return WarningItem{
		ID:            row.ID,
		TaskID:        row.TaskID,
		TaskCode:      row.TaskCode,
		TaskTitle:     row.TaskTitle,
		TaskStatus:    row.TaskStatus,
		SegmentCode:   row.SegmentCode,
		SegmentName:   row.SegmentName,
		District:      row.District,
		Stage:         row.Stage,
		Level:         row.Level,
		ThresholdDays: row.ThresholdDays,
		OverdueDays:   row.currentOverdueDays(today),
		Status:        row.Status,
		ResolveReason: row.ResolveReason,
		CreatedAt:     row.CreatedAt,
		ResolvedAt:    row.ResolvedAt,
	}
}

// currentOverdueDays 按预警生成时固化的阈值与任务日期，重算当前超期天数。
func (row warningRow) currentOverdueDays(today date.Date) int {
	base := row.stageBaseDate()
	if base.IsZero() {
		return row.OverdueDays
	}
	return DaysOver(base, row.ThresholdDays, today)
}

// stageBaseDate 取阶段对应的基准日期。
func (row warningRow) stageBaseDate() date.Date {
	switch row.Stage {
	case StageStart:
		return row.PlanStartDate
	case StageFinish:
		return row.PlanEndDate
	case StageAccept:
		if row.FinishedAt != nil {
			return date.New(*row.FinishedAt)
		}
	}
	return date.Date{}
}

// validateStage 校验阶段取值合法。
func validateStage(stage string) error {
	if !option.Has(StageOptions(), stage) {
		return httpx.Validation(fmt.Sprintf("预警阶段只能是：%s", option.Labels(StageOptions())))
	}
	return nil
}
