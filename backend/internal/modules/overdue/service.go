package overdue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/option"
	"github.com/drainage/desilting/internal/shared/refx"
)

// 默认规则：首次启动（规则表为空）时写入，生效日期取一个足够早的日期，
// 保证写入后立即参与判定。
var defaultRules = []OverdueRule{
	{Stage: StageStart, ThresholdDays: 3, Level: LevelNotice, EffectiveFrom: date.MustParse("2020-01-01")},
	{Stage: StageFinish, ThresholdDays: 0, Level: LevelWarning, EffectiveFrom: date.MustParse("2020-01-01")},
	{Stage: StageAccept, ThresholdDays: 7, Level: LevelCritical, EffectiveFrom: date.MustParse("2020-01-01")},
}

// Service 超期预警业务逻辑。
type Service struct {
	repo *Repository
}

// NewService 构造服务。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// EnsureDefaultRules 在规则表为空时写入默认的分阶段阈值。
//
// 规则一旦存在（包括被管理员全部调整过）就不再覆盖，保证配置只由显式操作变更。
func EnsureDefaultRules(ctx context.Context, db *gorm.DB) error {
	repo := NewRepository(db)
	total, err := repo.CountRules(ctx)
	if err != nil {
		return fmt.Errorf("统计预警规则失败: %w", err)
	}
	if total > 0 {
		return nil
	}
	for i := range defaultRules {
		if err := repo.CreateRule(ctx, &defaultRules[i]); err != nil {
			return fmt.Errorf("写入默认预警规则失败: %w", err)
		}
	}
	return nil
}

// Sync 扫描全部未结任务，按当前生效规则生成缺失的预警、解除不再成立的预警。
//
// 该方法是幂等的：同一任务同一阶段只生成一次预警（唯一约束兜底），
// 已解除的预警不会复活。所有读取预警数据的入口都先调用 Sync，
// 从而保证预警列表、任务列表与看板的超期口径一致。
func (s *Service) Sync(ctx context.Context) error {
	today := date.Today()
	rules, err := s.repo.EffectiveRules(ctx, today)
	if err != nil {
		return httpx.WrapInternal("读取预警规则失败", err)
	}

	tasks := make([]cleaningtask.CleaningTask, 0)
	err = s.repo.db.WithContext(ctx).Table(refx.TableCleaningTasks).
		Where("status IN ?", []string{
			cleaningtask.StatusPending,
			cleaningtask.StatusInProgress,
			cleaningtask.StatusCompleted,
		}).
		Find(&tasks).Error
	if err != nil {
		return httpx.WrapInternal("扫描清淤任务失败", err)
	}

	active, err := s.repo.ActiveWarnings(ctx)
	if err != nil {
		return httpx.WrapInternal("读取待处理预警失败", err)
	}

	for i := range tasks {
		task := &tasks[i]
		stage, days, overdue := Evaluate(task, rules, today)
		if overdue {
			if _, exists := active[task.ID][stage]; !exists {
				rule := rules[stage]
				warning := &OverdueWarning{
					TaskID:        task.ID,
					Stage:         stage,
					Level:         rule.Level,
					ThresholdDays: rule.ThresholdDays,
					OverdueDays:   days,
					Status:        WarningActive,
				}
				if err := s.repo.CreateWarning(ctx, warning); err != nil {
					return httpx.WrapInternal("生成超期预警失败", err)
				}
			}
		}
		// 解除该任务上不再成立的预警（状态推进、阶段迁移都会走到这里）。
		for warningStage, warning := range active[task.ID] {
			if !overdue || warningStage != stage {
				if err := s.repo.ResolveWarning(ctx, warning.ID, resolveReason(task.Status)); err != nil {
					return httpx.WrapInternal("解除超期预警失败", err)
				}
			}
		}
	}

	// 已验收、已取消的任务不参与超期统计，兜底解除其上仍待处理的预警。
	closedReasons := map[string]string{
		cleaningtask.StatusAccepted:  "任务已验收",
		cleaningtask.StatusCancelled: "任务已取消",
	}
	for status, reason := range closedReasons {
		if err := s.repo.ResolveForStatus(ctx, status, reason); err != nil {
			return httpx.WrapInternal("解除已关闭任务的预警失败", err)
		}
	}
	return nil
}

// List 分页查询预警列表。
func (s *Service) List(ctx context.Context, query WarningListQuery) ([]WarningItem, int64, error) {
	if query.Stage != "" {
		if err := validateStage(query.Stage); err != nil {
			return nil, 0, err
		}
	}
	if query.Level != "" && !option.Has(LevelOptions(), query.Level) {
		return nil, 0, httpx.Validation(fmt.Sprintf("提示级别只能是：%s", option.Labels(LevelOptions())))
	}
	if query.Status != "" && !option.Has(WarningStatusOptions(), query.Status) {
		return nil, 0, httpx.Validation(fmt.Sprintf("预警状态只能是：%s", option.Labels(WarningStatusOptions())))
	}
	if err := s.Sync(ctx); err != nil {
		return nil, 0, err
	}
	rows, total, err := s.repo.ListWarnings(ctx, query)
	if err != nil {
		return nil, 0, httpx.WrapInternal("查询超期预警失败", err)
	}
	today := date.Today()
	items := make([]WarningItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.toItem(today))
	}
	return items, total, nil
}

// Summary 待处理预警的分阶段统计。
func (s *Service) Summary(ctx context.Context) (*Summary, error) {
	if err := s.Sync(ctx); err != nil {
		return nil, err
	}
	byStage, byLevel, err := s.repo.CountActive(ctx)
	if err != nil {
		return nil, httpx.WrapInternal("统计超期预警失败", err)
	}
	summary := &Summary{ByStage: byStage, ByLevel: byLevel}
	for _, count := range byStage {
		summary.Total += count
	}
	return summary, nil
}

// BriefsByTaskIDs 查询指定任务的待处理预警摘要，供任务列表与看板复用同一口径。
func (s *Service) BriefsByTaskIDs(ctx context.Context, taskIDs []uint) (map[uint]cleaningtask.OverdueBrief, error) {
	briefs := make(map[uint]cleaningtask.OverdueBrief, len(taskIDs))
	if len(taskIDs) == 0 {
		return briefs, nil
	}
	if err := s.Sync(ctx); err != nil {
		return nil, err
	}
	rows, err := s.repo.ActiveForTasks(ctx, taskIDs)
	if err != nil {
		return nil, httpx.WrapInternal("查询任务超期预警失败", err)
	}
	today := date.Today()
	for _, row := range rows {
		briefs[row.TaskID] = cleaningtask.OverdueBrief{
			Stage:       row.Stage,
			Level:       row.Level,
			OverdueDays: row.currentOverdueDays(today),
		}
	}
	return briefs, nil
}

// Stages 返回预警阶段选项，供任务模块校验筛选参数（实现 cleaningtask.OverdueGateway）。
func (s *Service) Stages() []option.Option {
	return StageOptions()
}

// ListRules 列出全部规则（含历史版本）。
func (s *Service) ListRules(ctx context.Context) ([]OverdueRule, error) {
	rules, err := s.repo.ListRules(ctx)
	if err != nil {
		return nil, httpx.WrapInternal("查询预警规则失败", err)
	}
	return rules, nil
}

// CreateRule 新增规则。规则自带生效日期，只影响生效之后的判定。
func (s *Service) CreateRule(ctx context.Context, req RuleSaveRequest) (*OverdueRule, error) {
	stage := strings.TrimSpace(req.Stage)
	if err := validateStage(stage); err != nil {
		return nil, err
	}
	level := strings.TrimSpace(req.Level)
	if !option.Has(LevelOptions(), level) {
		return nil, httpx.Validation(fmt.Sprintf("提示级别只能是：%s", option.Labels(LevelOptions())))
	}
	if req.EffectiveFrom.IsZero() {
		return nil, httpx.Validation("生效日期不能为空")
	}

	rule := &OverdueRule{
		Stage:         stage,
		ThresholdDays: req.ThresholdDays,
		Level:         level,
		EffectiveFrom: req.EffectiveFrom,
	}
	if err := s.repo.CreateRule(ctx, rule); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, httpx.Conflict(fmt.Sprintf(
				"「%s」阶段在 %s 已有生效规则，请直接调整该日期或换一个生效日期",
				StageLabel(stage), req.EffectiveFrom.String(),
			))
		}
		return nil, httpx.WrapInternal("保存预警规则失败", err)
	}
	return rule, nil
}

// resolveReason 按任务当前状态给出预警解除原因。
func resolveReason(status string) string {
	switch status {
	case cleaningtask.StatusInProgress:
		return "任务已开工"
	case cleaningtask.StatusCompleted:
		return "已完工报验"
	case cleaningtask.StatusAccepted:
		return "任务已验收"
	case cleaningtask.StatusCancelled:
		return "任务已取消"
	default:
		return "超期阶段已变化"
	}
}
