package refx

import (
	"context"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/date"
)

// 换算规则相关表名，与 conversion 模块 model 的 TableName() 保持一致。
const (
	TableConversionRules = "sludge_conversion_rules"
	TableConversionLogs  = "sludge_conversion_logs"
)

// ConversionImpactRow 一次规则调整影响区间内、实际会被重新折算的湿重记录统计。
type ConversionImpactRow struct {
	RecordCount   int64
	TaskCount     int64
	SegmentCount  int64
	DistrictCount int64
	// DryBefore 这些湿重记录折算前的干重合计（即记录上已固化的 converted_dry_t 之和）。
	DryBefore float64
}

// wetWeightBasis 与 conversion.BasisWet 保持一致，这里用字面量避免 refx 反向依赖业务模块。
const wetWeightBasis = "wet"

// ConversionImpact 统计清淤日期落在 [from, to]（to 为 nil 表示开放区间）内、
// 且原始口径为湿重的记录范围与折算前干重合计。
//
// 干重记录折算系数恒为 1，调整湿重规则不会改变它们，因此不计入影响面。
// 这里只读取记录上已固化的 converted_dry_t（折算前），不做任何改写。
func ConversionImpact(ctx context.Context, db *gorm.DB, from date.Date, to *date.Date) (ConversionImpactRow, error) {
	var row ConversionImpactRow

	query := db.WithContext(ctx).Table(TableCleaningRecords+" AS r").
		Joins("INNER JOIN "+TableCleaningTasks+" AS t ON t.id = r.task_id").
		Joins("INNER JOIN "+TablePipeSegments+" AS s ON s.id = t.pipe_segment_id").
		Where("r.cleaned_at >= ?", from.QueryValue()).
		Where("r.weight_basis = ?", wetWeightBasis)
	if to != nil {
		query = query.Where("r.cleaned_at <= ?", to.QueryValue())
	}

	err := query.Select(`
			COUNT(*) AS record_count,
			COUNT(DISTINCT r.task_id) AS task_count,
			COUNT(DISTINCT t.pipe_segment_id) AS segment_count,
			COUNT(DISTINCT s.district) AS district_count,
			COALESCE(SUM(r.converted_dry_t), 0) AS dry_before`).
		Scan(&row).Error
	return row, err
}

// WetConversionRow 待重新折算的一条湿重记录的折算输入。
type WetConversionRow struct {
	ID         uint
	RawWeightT float64
}

// WetConversionsInWindow 读取区间内全部湿重记录的主键与原始重量，
// 供 conversion 服务在事务内用统一换算引擎逐行重算，避免数据库间 round 函数差异。
func WetConversionsInWindow(ctx context.Context, db *gorm.DB, from date.Date, to *date.Date) ([]WetConversionRow, error) {
	rows := make([]WetConversionRow, 0)
	query := db.WithContext(ctx).Table(TableCleaningRecords).
		Select("id, raw_weight_t").
		Where("cleaned_at >= ?", from.QueryValue()).
		Where("weight_basis = ?", wetWeightBasis)
	if to != nil {
		query = query.Where("cleaned_at <= ?", to.QueryValue())
	}
	err := query.Order("id ASC").Scan(&rows).Error
	return rows, err
}

// ApplyConversion 在给定事务内更新一条记录的折算三列，原始计量列不动。
func ApplyConversion(ctx context.Context, tx *gorm.DB, recordID uint, ruleID uint, factor, dryT float64) error {
	return tx.WithContext(ctx).Table(TableCleaningRecords).
		Where("id = ?", recordID).
		Updates(map[string]any{
			"conversion_rule_id": ruleID,
			"conversion_factor":  factor,
			"converted_dry_t":    dryT,
		}).Error
}
