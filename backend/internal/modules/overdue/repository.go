package overdue

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/refx"
)

// Repository 超期预警数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ---------- 规则 ----------

// CreateRule 新增规则。同一阶段同一生效日期只允许一条，冲突时返回 gorm.ErrDuplicatedKey。
func (r *Repository) CreateRule(ctx context.Context, rule *OverdueRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

// ListRules 按生效日期倒序列出全部规则（含历史版本）。
func (r *Repository) ListRules(ctx context.Context) ([]OverdueRule, error) {
	rules := make([]OverdueRule, 0)
	err := r.db.WithContext(ctx).
		Order("stage ASC, effective_from DESC, id DESC").
		Find(&rules).Error
	return rules, err
}

// EffectiveRules 取判定日当天每个阶段已生效的最新规则。
func (r *Repository) EffectiveRules(ctx context.Context, today date.Date) (map[string]OverdueRule, error) {
	rules := make([]OverdueRule, 0)
	err := r.db.WithContext(ctx).
		Where("effective_from <= ?", today.Time).
		Order("stage ASC, effective_from DESC, id DESC").
		Find(&rules).Error
	if err != nil {
		return nil, err
	}
	effective := make(map[string]OverdueRule, len(rules))
	for _, rule := range rules {
		if _, exists := effective[rule.Stage]; !exists {
			effective[rule.Stage] = rule
		}
	}
	return effective, nil
}

// CountRules 统计规则条数，用于判断是否需要写入默认规则。
func (r *Repository) CountRules(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&OverdueRule{}).Count(&total).Error
	return total, err
}

// ---------- 预警 ----------

// CreateWarning 写入预警；同一任务同一阶段已存在记录（含已解除）时静默跳过，
// 保证同一任务同一阶段不会被反复提醒。
func (r *Repository) CreateWarning(ctx context.Context, warning *OverdueWarning) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(warning).Error
}

// ActiveWarnings 取出全部待处理预警，按 任务 -> 阶段 归组。
func (r *Repository) ActiveWarnings(ctx context.Context) (map[uint]map[string]*OverdueWarning, error) {
	warnings := make([]OverdueWarning, 0)
	err := r.db.WithContext(ctx).
		Where("status = ?", WarningActive).
		Find(&warnings).Error
	if err != nil {
		return nil, err
	}
	grouped := make(map[uint]map[string]*OverdueWarning, len(warnings))
	for i := range warnings {
		warning := warnings[i]
		if grouped[warning.TaskID] == nil {
			grouped[warning.TaskID] = make(map[string]*OverdueWarning)
		}
		grouped[warning.TaskID][warning.Stage] = &warning
	}
	return grouped, nil
}

// ResolveWarning 解除指定预警。
func (r *Repository) ResolveWarning(ctx context.Context, id uint, reason string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&OverdueWarning{}).
		Where("id = ? AND status = ?", id, WarningActive).
		Updates(map[string]any{
			"status":         WarningResolved,
			"resolve_reason": reason,
			"resolved_at":    now,
			"updated_at":     now,
		}).Error
}

// ResolveForStatus 解除指定状态任务上仍待处理的全部预警（用于已验收、已取消的任务）。
func (r *Repository) ResolveForStatus(ctx context.Context, taskStatus, reason string) error {
	now := time.Now()
	closed := r.db.Table(refx.TableCleaningTasks).
		Select("id").
		Where("status = ?", taskStatus)
	return r.db.WithContext(ctx).Model(&OverdueWarning{}).
		Where("status = ? AND task_id IN (?)", WarningActive, closed).
		Updates(map[string]any{
			"status":         WarningResolved,
			"resolve_reason": reason,
			"resolved_at":    now,
			"updated_at":     now,
		}).Error
}

// warningRow 预警列表的联表查询结果。
type warningRow struct {
	OverdueWarning
	TaskCode      string
	TaskTitle     string
	TaskStatus    string
	PlanStartDate date.Date
	PlanEndDate   date.Date
	FinishedAt    *time.Time
	SegmentCode   string
	SegmentName   string
	District      string
}

// ListWarnings 分页联表查询预警，附带任务与管段信息。
func (r *Repository) ListWarnings(ctx context.Context, query WarningListQuery) ([]warningRow, int64, error) {
	query.Page.Normalize()

	var total int64
	if err := r.filteredWarnings(ctx, query).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := make([]warningRow, 0)
	err := r.filteredWarnings(ctx, query).
		Select(`w.*, t.code AS task_code, t.title AS task_title, t.status AS task_status,
			t.plan_start_date, t.plan_end_date, t.finished_at,
			COALESCE(s.code, '') AS segment_code,
			COALESCE(s.name, '') AS segment_name,
			COALESCE(s.district, '') AS district`).
		Order("w.id DESC").
		Offset(query.Page.Offset()).
		Limit(query.Page.PageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// filteredWarnings 构造预警列表的过滤查询（不含 Select，供计数与取数分别复用）。
func (r *Repository) filteredWarnings(ctx context.Context, query WarningListQuery) *gorm.DB {
	tx := r.db.WithContext(ctx).Table(refx.TableOverdueWarnings + " AS w").
		Joins("INNER JOIN " + refx.TableCleaningTasks + " AS t ON t.id = w.task_id").
		Joins("LEFT JOIN " + refx.TablePipeSegments + " AS s ON s.id = t.pipe_segment_id")
	if query.Stage != "" {
		tx = tx.Where("w.stage = ?", query.Stage)
	}
	if query.Level != "" {
		tx = tx.Where("w.level = ?", query.Level)
	}
	if query.Status != "" {
		tx = tx.Where("w.status = ?", query.Status)
	}
	if query.Keyword != "" {
		like := "%" + query.Keyword + "%"
		tx = tx.Where("LOWER(t.code) LIKE ? OR LOWER(t.title) LIKE ?", like, like)
	}
	return tx
}

// CountActive 按阶段与级别统计待处理预警数量。
func (r *Repository) CountActive(ctx context.Context) (map[string]int64, map[string]int64, error) {
	type row struct {
		Stage string
		Level string
		Total int64
	}
	rows := make([]row, 0)
	err := r.db.WithContext(ctx).Model(&OverdueWarning{}).
		Select("stage, level, COUNT(*) AS total").
		Where("status = ?", WarningActive).
		Group("stage, level").
		Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	byStage := make(map[string]int64, len(rows))
	byLevel := make(map[string]int64, len(rows))
	for _, item := range rows {
		byStage[item.Stage] += item.Total
		byLevel[item.Level] += item.Total
	}
	return byStage, byLevel, nil
}

// ActiveForTasks 查询指定任务集合上的待处理预警，附带计算超期天数所需的任务日期。
func (r *Repository) ActiveForTasks(ctx context.Context, taskIDs []uint) ([]warningRow, error) {
	rows := make([]warningRow, 0)
	if len(taskIDs) == 0 {
		return rows, nil
	}
	err := r.db.WithContext(ctx).Table(refx.TableOverdueWarnings+" AS w").
		Select(`w.*, t.code AS task_code, t.title AS task_title, t.status AS task_status,
			t.plan_start_date, t.plan_end_date, t.finished_at`).
		Joins("INNER JOIN "+refx.TableCleaningTasks+" AS t ON t.id = w.task_id").
		Where("w.status = ? AND w.task_id IN ?", WarningActive, taskIDs).
		Scan(&rows).Error
	return rows, err
}
