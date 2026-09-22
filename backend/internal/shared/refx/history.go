package refx

import (
	"context"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/num"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// HistoryItem 管段的清淤履历：一次任务串起"计划 -> 清淤记录 -> 验收结论"。
type HistoryItem struct {
	TaskID           uint        `json:"taskId"`
	TaskCode         string      `json:"taskCode"`
	Title            string      `json:"title"`
	Status           string      `json:"status"`
	Priority         string      `json:"priority"`
	TeamName         string      `json:"teamName"`
	PlanStartDate    date.Date   `json:"planStartDate"`
	PlanEndDate      date.Date   `json:"planEndDate"`
	RecordCount      int64       `json:"recordCount"`
	StandardSludgeT  float64     `json:"standardSludgeT"`
	RawAmounts       []RawAmount `gorm:"-" json:"rawAmounts"`
	CleanedLengthM   float64     `json:"cleanedLengthM"`
	AcceptanceResult string      `json:"acceptanceResult"`
	AcceptedAt       date.Date   `json:"acceptedAt"`
}

// HistoryForSegment 查询某管段下全部任务的清淤履历（按计划开始日期倒序）。
//
// 清淤量直接 JOIN 记录后按任务 GROUP BY，折算表达式只出现在外层 SELECT，
// 与任务 / 片区 / 看板共用同一段折算 SQL，保证各级合计一致。
func HistoryForSegment(ctx context.Context, db *gorm.DB, segmentID uint) ([]HistoryItem, error) {
	standardSum, sumArgs := StandardSumExpr("r")
	items := make([]HistoryItem, 0)
	err := db.WithContext(ctx).Table(TableCleaningTasks+" AS t").
		Select(`t.id AS task_id, t.code AS task_code, t.title, t.status, t.priority, t.team_name,
			t.plan_start_date, t.plan_end_date,
			COUNT(r.id) AS record_count,
			`+standardSum+` AS standard_sludge_t,
			COALESCE(SUM(r.length_m), 0) AS cleaned_length_m,
			COALESCE(ac.result, '') AS acceptance_result,
			ac.accepted_at`, sumArgs...).
		Joins("LEFT JOIN "+TableCleaningRecords+" AS r ON r.task_id = t.id").
		Joins(`LEFT JOIN (
			SELECT a.task_id, a.result, a.accepted_at
			FROM `+TableAcceptanceRecords+` AS a
			INNER JOIN (
				SELECT task_id, MAX(id) AS max_id FROM `+TableAcceptanceRecords+` GROUP BY task_id
			) AS latest ON latest.max_id = a.id
		) AS ac ON ac.task_id = t.id`).
		Where("t.pipe_segment_id = ?", segmentID).
		Group("t.id, ac.result, ac.accepted_at").
		Order("t.plan_start_date DESC, t.id DESC").
		Scan(&items).Error
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return items, nil
	}
	taskIDs := make([]uint, 0, len(items))
	for _, item := range items {
		taskIDs = append(taskIDs, item.TaskID)
	}
	rawByTask, err := rawAmountsGrouped(ctx, db, taskIDs)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].StandardSludgeT = num.Round2(items[i].StandardSludgeT)
		items[i].CleanedLengthM = num.Round2(items[i].CleanedLengthM)
		items[i].RawAmounts = rawByTask[items[i].TaskID]
	}
	return items, nil
}

// SludgeTotals 全局清淤量统计（用于管段台账列表的展示与统计口径统一）。
type SludgeTotals struct {
	RecordCount     int64       `json:"recordCount"`
	StandardSludgeT float64     `json:"standardSludgeT"`
	RawAmounts      []RawAmount `gorm:"-" json:"rawAmounts"`
	CleanedLengthM  float64     `json:"cleanedLengthM"`
}

// SludgeTotalsForSegment 统计某管段累计清淤量（折算干重 + 原始口径并列）。
func SludgeTotalsForSegment(ctx context.Context, db *gorm.DB, segmentID uint) (SludgeTotals, error) {
	standardSum, sumArgs := StandardSumExpr("r")
	var totals SludgeTotals
	err := db.WithContext(ctx).Table(TableCleaningRecords+" AS r").
		Select(`COUNT(r.id) AS record_count,
			`+standardSum+` AS standard_sludge_t,
			COALESCE(SUM(r.length_m), 0) AS cleaned_length_m`, sumArgs...).
		Joins("INNER JOIN "+TableCleaningTasks+" AS t ON t.id = r.task_id").
		Where("t.pipe_segment_id = ?", segmentID).
		Scan(&totals).Error
	if err != nil {
		return SludgeTotals{}, err
	}

	type rawRow struct {
		Caliber string
		Amount  float64
	}
	rawRows := make([]rawRow, 0)
	if err := db.WithContext(ctx).Table(TableCleaningRecords+" AS r").
		Select("r."+colCaliber+" AS caliber, COALESCE(SUM(r."+colAmount+"), 0) AS amount").
		Joins("INNER JOIN "+TableCleaningTasks+" AS t ON t.id = r.task_id").
		Where("t.pipe_segment_id = ?", segmentID).
		Group("r." + colCaliber).
		Scan(&rawRows).Error; err != nil {
		return SludgeTotals{}, err
	}
	for _, r := range rawRows {
		totals.RawAmounts = append(totals.RawAmounts, RawAmount{
			Caliber: r.Caliber,
			Amount:  num.Round2(r.Amount),
		})
	}
	totals.StandardSludgeT = num.Round2(totals.StandardSludgeT)
	totals.CleanedLengthM = num.Round2(totals.CleanedLengthM)
	for i := range totals.RawAmounts {
		totals.RawAmounts[i].Unit = sludge.Unit(totals.RawAmounts[i].Caliber)
	}
	return totals, nil
}
