package conversion

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// ErrNotFound 换算规则不存在。
var ErrNotFound = errors.New("换算规则不存在")

// Repository 换算规则数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB 暴露底层连接。
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// List 查询全部换算规则，按方向、生效日期升序返回（版本链从旧到新）。
func (r *Repository) List(ctx context.Context) ([]Rule, error) {
	rules := make([]Rule, 0)
	err := r.db.WithContext(ctx).
		Order("pair ASC, effective_from ASC, id ASC").
		Find(&rules).Error
	return rules, err
}

// ListFactorRules 以换算引擎所需的最小结构返回全部规则。
func (r *Repository) ListFactorRules(ctx context.Context) ([]sludge.FactorRule, error) {
	rules, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	factors := make([]sludge.FactorRule, 0, len(rules))
	for _, rule := range rules {
		factors = append(factors, sludge.FactorRule{
			Pair:          rule.Pair,
			Factor:        rule.Factor,
			EffectiveFrom: rule.EffectiveFrom,
		})
	}
	return factors, nil
}

// FindByID 按主键查询。
func (r *Repository) FindByID(ctx context.Context, id uint) (*Rule, error) {
	var rule Rule
	err := r.db.WithContext(ctx).First(&rule, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// FindVersion 查询某方向在指定生效日期的版本（唯一索引保护）。
func (r *Repository) FindVersion(ctx context.Context, pair string, effectiveFrom date.Date) (*Rule, error) {
	var rule Rule
	err := r.db.WithContext(ctx).
		Where("pair = ? AND effective_from = ?", pair, effectiveFrom.Time).
		First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// LatestByPair 查询某方向的最新版本；不存在时返回 nil。
func (r *Repository) LatestByPair(ctx context.Context, pair string) (*Rule, error) {
	var rule Rule
	err := r.db.WithContext(ctx).
		Where("pair = ?", pair).
		Order("effective_from DESC, id DESC").
		First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// Create 新增规则版本。
func (r *Repository) Create(ctx context.Context, rule *Rule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

// Delete 删除规则版本（仅允许删除未被引用的未来版本，由 service 把关）。
func (r *Repository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&Rule{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
