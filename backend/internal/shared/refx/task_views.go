package refx

import (
	"context"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/num"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// RawAmount 某原始口径下的数值合计（不做任何换算，仅用于界面并列展示）。
type RawAmount struct {
	Caliber string  `json:"caliber"`
	Unit    string  `json:"unit"`
	Amount  float64 `json:"amount"`
}

// RecordTotals 某个任务的清淤记录汇总。
//
// StandardSludgeT 是折算到统一口径（干重 t）后的合计，各层级合计都以它为准；
// RawAmounts 保留各原始口径的数值合计，方便对照原始台账，但不参与跨口径汇总。
type RecordTotals struct {
	RecordCount     int64       `json:"recordCount"`
	StandardSludgeT float64     `json:"standardSludgeT"`
	RawAmounts      []RawAmount `gorm:"-" json:"rawAmounts"`
	CleanedLengthM  float64     `json:"cleanedLengthM"`
	LatestCleanedAt date.Date   `json:"latestCleanedAt"`
}

// AcceptanceBrief 某个任务的最新验收结论。
type AcceptanceBrief struct {
	ID              uint      `json:"id"`
	Code            string    `json:"code"`
	Result          string    `json:"result"`
	AcceptedAt      date.Date `json:"acceptedAt"`
	InspectorName   string    `json:"inspectorName"`
	InspectorOrg    string    `json:"inspectorOrg"`
	Score           int       `json:"score"`
	Issues          string    `json:"issues"`
	RectifyDeadline date.Date `json:"rectifyDeadline"`
	RectifiedAt     date.Date `json:"rectifiedAt"`
}

// scanRecordTotals 在已构造好的查询上执行扫描并组装汇总。
// baseQuery 已包含 FROM / JOIN / WHERE（WHERE 参数由 Where 子句自行注册，
// GORM 会按 SELECT → WHERE 的 SQL 出现顺序绑定占位符）。
func scanRecordTotals(tx *gorm.DB, standardExpr string, standardArgs []any) (RecordTotals, error) {
	type row struct {
		RecordCount     int64
		StandardSludgeT float64
		CleanedLengthM  float64
		LatestCleanedAt *date.Date
	}
	var r row
	selectClause := "COUNT(*) AS record_count, " +
		standardExpr + " AS standard_sludge_t, " +
		"COALESCE(SUM(length_m), 0) AS cleaned_length_m, " +
		"MAX(cleaned_at) AS latest_cleaned_at"
	if err := tx.Select(selectClause, standardArgs...).Scan(&r).Error; err != nil {
		return RecordTotals{}, err
	}
	totals := RecordTotals{
		RecordCount:     r.RecordCount,
		StandardSludgeT: num.Round2(r.StandardSludgeT),
		CleanedLengthM:  num.Round2(r.CleanedLengthM),
	}
	if r.LatestCleanedAt != nil {
		totals.LatestCleanedAt = *r.LatestCleanedAt
	}
	return totals, nil
}

// rawAmountsFor 汇总指定任务集合在各原始口径下的数值合计。
// taskIDs 为空时返回空切片。
func rawAmountsFor(ctx context.Context, db *gorm.DB, taskIDColumn string, taskIDs []any) ([]RawAmount, error) {
	type rawRow struct {
		Caliber string
		Amount  float64
	}
	rows := make([]rawRow, 0)
	err := db.WithContext(ctx).Table(TableCleaningRecords).
		Select(colCaliber+" AS caliber, COALESCE(SUM("+colAmount+"), 0) AS amount").
		Where(taskIDColumn+" IN ?", taskIDs).
		Group(colCaliber).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]RawAmount, 0, len(rows))
	for _, r := range rows {
		out = append(out, RawAmount{
			Caliber: r.Caliber,
			Unit:    sludge.Unit(r.Caliber),
			Amount:  num.Round2(r.Amount),
		})
	}
	return out, nil
}

// TotalsByTaskID 汇总单个任务的清淤记录。
func TotalsByTaskID(ctx context.Context, db *gorm.DB, taskID uint) (RecordTotals, error) {
	standardExpr, standardArgs := StandardSumExpr(aliasRecords)
	tx := db.WithContext(ctx).Table(TableCleaningRecords+" AS "+aliasRecords).
		Where(aliasRecords+".task_id = ?", taskID)
	totals, err := scanRecordTotals(tx, standardExpr, standardArgs)
	if err != nil {
		return RecordTotals{}, err
	}
	raw, err := rawAmountsFor(ctx, db, "task_id", []any{taskID})
	if err != nil {
		return RecordTotals{}, err
	}
	totals.RawAmounts = raw
	return totals, nil
}

// TotalsByTaskIDs 批量汇总多个任务的清淤记录，避免列表接口 N+1 查询。
func TotalsByTaskIDs(ctx context.Context, db *gorm.DB, taskIDs []uint) (map[uint]RecordTotals, error) {
	result := make(map[uint]RecordTotals, len(taskIDs))
	if len(taskIDs) == 0 {
		return result, nil
	}

	// 折算表达式依赖规则表的相关子查询，按任务分组一次查出。
	type aggRow struct {
		TaskID          uint
		RecordCount     int64
		StandardSludgeT float64
		CleanedLengthM  float64
		LatestCleanedAt *date.Date
	}
	standardExpr, standardArgs := StandardSumExpr(aliasRecords)
	aggRows := make([]aggRow, 0)
	err := db.WithContext(ctx).Table(TableCleaningRecords+" AS "+aliasRecords).
		Select(aliasRecords+".task_id AS task_id, COUNT(*) AS record_count, "+
			standardExpr+" AS standard_sludge_t, "+
			"COALESCE(SUM("+aliasRecords+".length_m), 0) AS cleaned_length_m, "+
			"MAX("+aliasRecords+".cleaned_at) AS latest_cleaned_at", standardArgs...).
		Where(aliasRecords+".task_id IN ?", taskIDs).
		Group(aliasRecords + ".task_id").
		Scan(&aggRows).Error
	if err != nil {
		return nil, err
	}

	rawByTask, err := rawAmountsGrouped(ctx, db, taskIDs)
	if err != nil {
		return nil, err
	}

	for _, r := range aggRows {
		totals := RecordTotals{
			RecordCount:     r.RecordCount,
			StandardSludgeT: num.Round2(r.StandardSludgeT),
			CleanedLengthM:  num.Round2(r.CleanedLengthM),
			RawAmounts:      rawByTask[r.TaskID],
		}
		if r.LatestCleanedAt != nil {
			totals.LatestCleanedAt = *r.LatestCleanedAt
		}
		if totals.RawAmounts == nil {
			totals.RawAmounts = []RawAmount{}
		}
		result[r.TaskID] = totals
	}
	return result, nil
}

// LatestAcceptanceForTask 查询任务最近一次验收记录。
func LatestAcceptanceForTask(ctx context.Context, db *gorm.DB, taskID uint) (*AcceptanceBrief, error) {
	var brief AcceptanceBrief
	err := db.WithContext(ctx).Table(TableAcceptanceRecords).
		Select(`id, code, result, accepted_at, inspector_name, inspector_org,
			score, issues, rectify_deadline, rectified_at`).
		Where("task_id = ?", taskID).
		Order("id DESC").
		Limit(1).
		Scan(&brief).Error
	if err != nil {
		return nil, err
	}
	if brief.ID == 0 {
		return nil, nil
	}
	return &brief, nil
}
