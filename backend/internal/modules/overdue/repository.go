package overdue

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/date"
)

// Repository 超期预警阈值配置的数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB 暴露底层连接，供 service 做跨表预警查询。
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// Create 新增一条阈值配置版本。
func (r *Repository) Create(ctx context.Context, rule *OverdueRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

// Save 保存配置全部字段。
func (r *Repository) Save(ctx context.Context, rule *OverdueRule) error {
	return r.db.WithContext(ctx).Save(rule).Error
}

// FindByStageAndEffectiveFrom 按阶段与生效日期查找配置版本，不存在时返回 nil。
func (r *Repository) FindByStageAndEffectiveFrom(ctx context.Context, stage string, effectiveFrom date.Date) (*OverdueRule, error) {
	var rule OverdueRule
	err := r.db.WithContext(ctx).
		Where("stage = ? AND effective_from = ?", stage, effectiveFrom.Time).
		First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// EffectiveRules 查询生效时间不晚于 today 的全部配置版本。
func (r *Repository) EffectiveRules(ctx context.Context, today date.Date) ([]OverdueRule, error) {
	rules := make([]OverdueRule, 0)
	err := r.db.WithContext(ctx).
		Where("effective_from <= ?", today.Time).
		Find(&rules).Error
	return rules, err
}

// AllVersions 查询全部配置版本（生效日期新的在前）。
func (r *Repository) AllVersions(ctx context.Context) ([]OverdueRule, error) {
	rules := make([]OverdueRule, 0)
	err := r.db.WithContext(ctx).
		Order("effective_from DESC, id DESC").
		Find(&rules).Error
	return rules, err
}

// Count 配置版本总数。
func (r *Repository) Count(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&OverdueRule{}).Count(&total).Error
	return total, err
}
