package refx

import (
	"context"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// AffectedRecordRow 影响分析中被某规则版本覆盖的原始记录。
type AffectedRecordRow struct {
	ID        uint      `gorm:"column:record_id"`
	Code      string    `gorm:"column:code"`
	TaskID    uint      `gorm:"column:task_id"`
	District  string    `gorm:"column:district"`
	CleanedAt date.Date `gorm:"column:cleaned_at"`
	Amount    float64   `gorm:"column:amount"`
	Caliber   string    `gorm:"column:caliber"`
}

// RecordsAffectedByPair 查询会被「指定方向 + 生效日期」新版本影响的原始记录。
//
// 体积->湿重 方向的规则会同时影响 m3 口径（链路含密度）与 wet_t 口径吗？
// 不会：wet_t -> 干重只经过干湿系数；因此：
//   - m3->wet_t 规则仅影响 caliber = m3 的记录；
//   - wet_t->dry_t 规则影响 caliber = m3（链路末端）与 wet_t 的记录。
//
// 生效日期 effectiveFrom 取「effective_from <= 清淤日期」的最新版本，
// 所以新版本只覆盖 cleaned_at >= 其生效日期的记录。
func RecordsAffectedByPair(ctx context.Context, db *gorm.DB, pair string, effectiveFrom date.Date, limit int) ([]AffectedRecordRow, error) {
	calibers := affectedCalibers(pair)
	if len(calibers) == 0 {
		return nil, nil
	}
	rows := make([]AffectedRecordRow, 0)
	q := db.WithContext(ctx).Table(TableCleaningRecords+" AS r").
		Select(`r.id AS record_id, r.code, r.task_id, r.cleaned_at,
			r.`+colAmount+` AS amount, r.`+colCaliber+` AS caliber,
			COALESCE(s.district, '') AS district`).
		Joins("INNER JOIN "+TableCleaningTasks+" AS t ON t.id = r.task_id").
		Joins("LEFT JOIN "+TablePipeSegments+" AS s ON s.id = t.pipe_segment_id").
		Where("r."+colCaliber+" IN ?", calibers).
		Where("r.cleaned_at >= ?", effectiveFrom.Time).
		Order("r.cleaned_at DESC, r.id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Scan(&rows).Error
	return rows, err
}

// CountRecordsAffectedByPair 统计受影响记录条数与去重任务数。
func CountRecordsAffectedByPair(ctx context.Context, db *gorm.DB, pair string, effectiveFrom date.Date) (recordCount, taskCount int64, districts []string, err error) {
	calibers := affectedCalibers(pair)
	if len(calibers) == 0 {
		return 0, 0, []string{}, nil
	}
	var counters struct {
		Records int64
		Tasks   int64
	}
	err = db.WithContext(ctx).Table(TableCleaningRecords+" AS r").
		Select("COUNT(r.id) AS records, COUNT(DISTINCT r.task_id) AS tasks").
		Joins("INNER JOIN "+TableCleaningTasks+" AS t ON t.id = r.task_id").
		Where("r."+colCaliber+" IN ?", calibers).
		Where("r.cleaned_at >= ?", effectiveFrom.Time).
		Scan(&counters).Error
	if err != nil {
		return 0, 0, nil, err
	}
	districtRows := make([]string, 0)
	err = db.WithContext(ctx).Table(TableCleaningRecords+" AS r").
		Distinct("COALESCE(s.district, '') AS district").
		Joins("INNER JOIN "+TableCleaningTasks+" AS t ON t.id = r.task_id").
		Joins("LEFT JOIN "+TablePipeSegments+" AS s ON s.id = t.pipe_segment_id").
		Where("r."+colCaliber+" IN ?", calibers).
		Where("r.cleaned_at >= ?", effectiveFrom.Time).
		Where("COALESCE(s.district, '') <> ''").
		Order("district ASC").
		Pluck("district", &districtRows).Error
	if err != nil {
		return 0, 0, nil, err
	}
	return counters.Records, counters.Tasks, districtRows, nil
}

// AllRecordRows 全部清淤记录原始值（影响分析中重算全局总量，记录数有限可全量读取）。
type AllRecordRows struct {
	ID        uint      `gorm:"column:record_id"`
	TaskID    uint      `gorm:"column:task_id"`
	District  string    `gorm:"column:district"`
	CleanedAt date.Date `gorm:"column:cleaned_at"`
	Amount    float64   `gorm:"column:amount"`
	Caliber   string    `gorm:"column:caliber"`
}

// ListAllRecordsForImpact 读取全部记录原始值，用于在 Go 侧试算规则前后总量。
func ListAllRecordsForImpact(ctx context.Context, db *gorm.DB) ([]AllRecordRows, error) {
	rows := make([]AllRecordRows, 0)
	err := db.WithContext(ctx).Table(TableCleaningRecords + " AS r").
		Select(`r.id AS record_id, r.task_id, r.cleaned_at,
			r.` + colAmount + ` AS amount, r.` + colCaliber + ` AS caliber,
			COALESCE(s.district, '') AS district`).
		Joins("INNER JOIN " + TableCleaningTasks + " AS t ON t.id = r.task_id").
		Joins("LEFT JOIN " + TablePipeSegments + " AS s ON s.id = t.pipe_segment_id").
		Order("r.id ASC").
		Scan(&rows).Error
	return rows, err
}

// IsRuleReferenced 判断规则版本是否被某条记录的折算实际引用
// （即存在 cleaned_at >= 生效日期 且口径经过该方向的记录）。
func IsRuleReferenced(ctx context.Context, db *gorm.DB, pair string, effectiveFrom date.Date) (bool, error) {
	count, _, _, err := CountRecordsAffectedByPair(ctx, db, pair, effectiveFrom)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// affectedCalibers 返回某方向规则参与折算的原始口径集合。
func affectedCalibers(pair string) []string {
	switch pair {
	case sludge.PairM3ToWet:
		return []string{sludge.CaliberM3}
	case sludge.PairWetToDry:
		// 体积记录的折算链末端也经过干湿系数。
		return []string{sludge.CaliberM3, sludge.CaliberWetT}
	default:
		return nil
	}
}
