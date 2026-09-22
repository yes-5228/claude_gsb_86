package conversion

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/date"
)

// ErrNotFound 换算规则不存在。
var ErrNotFound = errors.New("换算规则不存在")

// Repository 换算规则与变更日志的数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB 暴露底层连接，供 service 在事务内重算记录折算值。
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// CreateRule 新增一条规则版本。
func (r *Repository) CreateRule(ctx context.Context, rule *ConversionRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

// FindRuleByID 按主键查询规则。
func (r *Repository) FindRuleByID(ctx context.Context, id uint) (*ConversionRule, error) {
	var rule ConversionRule
	err := r.db.WithContext(ctx).First(&rule, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// ExistsEffectiveFrom 判断某生效日期是否已存在规则（同一天只允许一个版本）。
func (r *Repository) ExistsEffectiveFrom(ctx context.Context, day date.Date, excludeID uint) (bool, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&ConversionRule{}).Where("effective_from = ?", day.QueryValue())
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListRules 按生效日期倒序返回全部规则版本（最新生效的排最前）。
func (r *Repository) ListRules(ctx context.Context) ([]ConversionRule, error) {
	rules := make([]ConversionRule, 0)
	err := r.db.WithContext(ctx).
		Order("effective_from DESC, id DESC").
		Find(&rules).Error
	return rules, err
}

// EffectiveAt 返回某业务日期当天生效的规则：effective_from <= day 的最新一条。
// 没有任何规则（含该日期）时返回 nil, nil，由调用方决定如何处理。
func (r *Repository) EffectiveAt(ctx context.Context, day date.Date) (*ConversionRule, error) {
	var rule ConversionRule
	err := r.db.WithContext(ctx).
		Where("effective_from <= ?", day.QueryValue()).
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

// LatestRule 返回当前最新的规则版本（生效日期最大），无规则时返回 nil, nil。
func (r *Repository) LatestRule(ctx context.Context) (*ConversionRule, error) {
	var rule ConversionRule
	err := r.db.WithContext(ctx).
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

// NextRule 返回生效日期严格晚于 day 的最早一条规则；不存在时返回 nil, nil。
// 用于确定一条新规则的影响区间上界（下一条规则生效日的前一天）。
func (r *Repository) NextRule(ctx context.Context, day date.Date) (*ConversionRule, error) {
	var rule ConversionRule
	err := r.db.WithContext(ctx).
		Where("effective_from > ?", day.QueryValue()).
		Order("effective_from ASC, id ASC").
		First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// CreateLog 写入一条规则变更日志。
func (r *Repository) CreateLog(ctx context.Context, log *ConversionLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// ListLogs 按创建时间倒序返回规则变更日志。
func (r *Repository) ListLogs(ctx context.Context, limit int) ([]ConversionLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	logs := make([]ConversionLog, 0, limit)
	err := r.db.WithContext(ctx).
		Order("id DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}
