package overdue

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/refx"
)

// Service 超期预警业务逻辑。
type Service struct {
	repo *Repository
}

// NewService 构造服务。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// CurrentThresholds 当前生效的三阶段阈值：取生效时间不晚于今天的最新版本，缺失阶段回退默认值。
func (s *Service) CurrentThresholds(ctx context.Context) (Thresholds, error) {
	rules, err := s.repo.EffectiveRules(ctx, date.Today())
	if err != nil {
		return Thresholds{}, httpx.WrapInternal("查询超期预警阈值失败", err)
	}
	return resolveThresholds(rules), nil
}

// resolveThresholds 从生效配置版本中解析三阶段阈值。
func resolveThresholds(rules []OverdueRule) Thresholds {
	thresholds := DefaultThresholds
	latest := latestByStage(rules)
	if rule, ok := latest[StageNotStarted]; ok {
		thresholds.NotStartedDays = rule.ThresholdDays
	}
	if rule, ok := latest[StageNotReported]; ok {
		thresholds.NotReportedDays = rule.ThresholdDays
	}
	if rule, ok := latest[StageNotAccepted]; ok {
		thresholds.NotAcceptedDays = rule.ThresholdDays
	}
	return thresholds
}

// latestByStage 从生效版本中挑出每个阶段的最新一条（生效日期优先，其次主键）。
func latestByStage(rules []OverdueRule) map[string]OverdueRule {
	latest := make(map[string]OverdueRule, len(stageLevels))
	for _, rule := range rules {
		current, ok := latest[rule.Stage]
		if !ok || rule.EffectiveFrom.After(current.EffectiveFrom) ||
			(rule.EffectiveFrom.Time.Equal(current.EffectiveFrom.Time) && rule.ID > current.ID) {
			latest[rule.Stage] = rule
		}
	}
	return latest
}

// SaveRule 新增一个阈值配置版本。
//
// 生效日期不允许早于今天，保证调整只影响之后的判定；
// 同一阶段同一生效日期重复保存时覆盖阈值，避免产生歧义版本。
func (s *Service) SaveRule(ctx context.Context, req SaveRuleRequest) (*OverdueRule, error) {
	stage := strings.TrimSpace(req.Stage)
	if !HasStage(stage) {
		return nil, httpx.Validation("预警阶段不合法，只能是：未开工 / 未报验 / 未验收")
	}
	if req.ThresholdDays < 0 || req.ThresholdDays > 365 {
		return nil, httpx.Validation("预警阈值需在 0-365 天之间")
	}
	if req.EffectiveFrom.IsZero() {
		return nil, httpx.Validation("生效日期不能为空")
	}
	if req.EffectiveFrom.Before(date.Today()) {
		return nil, httpx.Validation("生效日期不能早于今天，阈值调整只影响之后的判定")
	}

	existing, err := s.repo.FindByStageAndEffectiveFrom(ctx, stage, req.EffectiveFrom)
	if err != nil {
		return nil, httpx.WrapInternal("查询阈值配置失败", err)
	}
	if existing != nil {
		existing.ThresholdDays = req.ThresholdDays
		if err := s.repo.Save(ctx, existing); err != nil {
			return nil, httpx.WrapInternal("更新阈值配置失败", err)
		}
		return existing, nil
	}

	rule := &OverdueRule{
		Stage:         stage,
		ThresholdDays: req.ThresholdDays,
		EffectiveFrom: req.EffectiveFrom,
	}
	if err := s.repo.Create(ctx, rule); err != nil {
		return nil, httpx.WrapInternal("保存阈值配置失败", err)
	}
	return rule, nil
}

// EnsureDefaults 在没有任何配置时写入默认阈值（生效日期足够早，保证立即生效）。
func (s *Service) EnsureDefaults(ctx context.Context) error {
	total, err := s.repo.Count(ctx)
	if err != nil {
		return err
	}
	if total > 0 {
		return nil
	}
	epoch := date.MustParse("2024-01-01")
	rules := []OverdueRule{
		{Stage: StageNotStarted, ThresholdDays: DefaultThresholds.NotStartedDays, EffectiveFrom: epoch},
		{Stage: StageNotReported, ThresholdDays: DefaultThresholds.NotReportedDays, EffectiveFrom: epoch},
		{Stage: StageNotAccepted, ThresholdDays: DefaultThresholds.NotAcceptedDays, EffectiveFrom: epoch},
	}
	for i := range rules {
		if err := s.repo.Create(ctx, &rules[i]); err != nil {
			return err
		}
	}
	return nil
}

// ListWarnings 分页查询预警列表。
//
// 判定由 StageCaseSQL 完成，同一任务同一时刻只会落入一个阶段，列表中不会重复出现。
func (s *Service) ListWarnings(ctx context.Context, query ListQuery) ([]WarningItem, int64, error) {
	query.Page.Normalize()
	thresholds, err := s.CurrentThresholds(ctx)
	if err != nil {
		return nil, 0, err
	}
	today := date.Today()

	var total int64
	if err := s.warningQuery(ctx, query, thresholds).Count(&total).Error; err != nil {
		return nil, 0, httpx.WrapInternal("统计超期预警失败", err)
	}

	rows := make([]warningRow, 0)
	err = s.warningQuery(ctx, query, thresholds).
		Select(`t.id, t.code, t.title, t.status, t.priority, t.team_name,
			t.plan_start_date, t.plan_end_date, t.finished_at, t.overdue_stage,
			COALESCE(s.code, '') AS segment_code,
			COALESCE(s.name, '') AS segment_name,
			COALESCE(s.district, '') AS district`).
		Joins("LEFT JOIN " + refx.TablePipeSegments + " AS s ON s.id = t.pipe_segment_id").
		Order("t.overdue_stage ASC, t.plan_end_date ASC, t.id ASC").
		Offset(query.Page.Offset()).
		Limit(query.Page.PageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, httpx.WrapInternal("查询超期预警失败", err)
	}

	items := make([]WarningItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, WarningItem{
			TaskID:        row.ID,
			Code:          row.Code,
			Title:         row.Title,
			Status:        row.Status,
			Priority:      row.Priority,
			TeamName:      row.TeamName,
			SegmentCode:   row.SegmentCode,
			SegmentName:   row.SegmentName,
			District:      row.District,
			Stage:         row.OverdueStage,
			Level:         LevelOf(row.OverdueStage),
			OverdueDays:   OverdueDays(row.OverdueStage, row.snapshot(), today),
			PlanStartDate: row.PlanStartDate,
			PlanEndDate:   row.PlanEndDate,
			FinishedAt:    row.FinishedAt,
		})
	}
	return items, total, nil
}

// warningQuery 构造预警任务的过滤查询（基于带 overdue_stage 列的子查询）。
func (s *Service) warningQuery(ctx context.Context, query ListQuery, thresholds Thresholds) *gorm.DB {
	expr, args := StageCaseSQL("", thresholds, date.Today())
	sub := s.repo.DB().WithContext(ctx).Table(refx.TableCleaningTasks).
		Select("*, "+expr+" AS overdue_stage", args...)
	tx := s.repo.DB().WithContext(ctx).Table("(?) AS t", sub).
		Where("t.overdue_stage <> ''")
	if query.Stage != "" {
		tx = tx.Where("t.overdue_stage = ?", query.Stage)
	}
	return tx
}

// CountByStage 统计处于各预警阶段的任务数量（看板与预警汇总共用，保证口径一致）。
func (s *Service) CountByStage(ctx context.Context) (int64, map[string]int64, error) {
	thresholds, err := s.CurrentThresholds(ctx)
	if err != nil {
		return 0, nil, err
	}
	expr, args := StageCaseSQL("", thresholds, date.Today())
	sub := s.repo.DB().WithContext(ctx).Table(refx.TableCleaningTasks).
		Select("*, "+expr+" AS overdue_stage", args...)

	type stageCount struct {
		Stage string
		Total int64
	}
	rows := make([]stageCount, 0)
	err = s.repo.DB().WithContext(ctx).Table("(?) AS t", sub).
		Select("overdue_stage AS stage, COUNT(*) AS total").
		Where("overdue_stage <> ''").
		Group("overdue_stage").
		Scan(&rows).Error
	if err != nil {
		return 0, nil, httpx.WrapInternal("统计超期预警失败", err)
	}

	byStage := map[string]int64{
		StageNotStarted:  0,
		StageNotReported: 0,
		StageNotAccepted: 0,
	}
	var total int64
	for _, row := range rows {
		byStage[row.Stage] = row.Total
		total += row.Total
	}
	return total, byStage, nil
}

// RulesOverview 阈值配置总览：三阶段当前生效值 + 全部历史版本。
func (s *Service) RulesOverview(ctx context.Context) (*RulesOverview, error) {
	effective, err := s.repo.EffectiveRules(ctx, date.Today())
	if err != nil {
		return nil, httpx.WrapInternal("查询阈值配置失败", err)
	}
	latest := latestByStage(effective)

	defaults := map[string]int{
		StageNotStarted:  DefaultThresholds.NotStartedDays,
		StageNotReported: DefaultThresholds.NotReportedDays,
		StageNotAccepted: DefaultThresholds.NotAcceptedDays,
	}
	current := make([]CurrentRule, 0, len(stageLevels))
	for _, stage := range []string{StageNotStarted, StageNotReported, StageNotAccepted} {
		item := CurrentRule{
			Stage:         stage,
			Level:         LevelOf(stage),
			ThresholdDays: defaults[stage],
		}
		if rule, ok := latest[stage]; ok {
			item.ThresholdDays = rule.ThresholdDays
			item.EffectiveFrom = rule.EffectiveFrom
		}
		current = append(current, item)
	}

	versions, err := s.repo.AllVersions(ctx)
	if err != nil {
		return nil, httpx.WrapInternal("查询阈值配置历史失败", err)
	}
	return &RulesOverview{Current: current, Versions: versions}, nil
}
