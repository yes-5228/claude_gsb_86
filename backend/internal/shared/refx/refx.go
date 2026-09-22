// Package refx 集中处理跨模块的反向引用检查与跨表统计。
//
// 模块之间的依赖方向是单向的：清淤记录 -> 清淤任务 -> 管段台账。
// 如果让上游模块反过来 import 下游模块（例如管段模块要检查自己是否已被任务引用），
// 就会形成循环依赖。因此这类反向查询统一收敛到这里，改用表名直接查询。
//
// 注意：这里的表名必须与各模块 model 中的 TableName() 保持一致。
package refx

import (
	"context"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/shared/num"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// 各模块的表名。
const (
	TablePipeSegments      = "pipe_segments"
	TableCleaningTasks     = "cleaning_tasks"
	TableCleaningRecords   = "cleaning_records"
	TableAcceptanceRecords = "acceptance_records"
)

// TaskStats 某个管段下的清淤任务数量汇总。
type TaskStats struct {
	Total      int64 `json:"total"`
	Pending    int64 `json:"pending"`
	InProgress int64 `json:"inProgress"`
	Completed  int64 `json:"completed"`
	Accepted   int64 `json:"accepted"`
	Cancelled  int64 `json:"cancelled"`
}

// TaskRef 管段详情中展示的精简任务信息。
type TaskRef struct {
	ID              uint        `json:"id"`
	Code            string      `json:"code"`
	Title           string      `json:"title"`
	Status          string      `json:"status"`
	Priority        string      `json:"priority"`
	TeamName        string      `json:"teamName"`
	PlanStartDate   string      `json:"planStartDate"`
	PlanEndDate     string      `json:"planEndDate"`
	RecordCount     int64       `gorm:"column:record_count" json:"recordCount"`
	StandardSludgeT float64     `gorm:"column:standard_sludge_t" json:"standardSludgeT"`
	RawAmounts      []RawAmount `gorm:"-" json:"rawAmounts"`
}

// HasTasksForSegment 管段是否已经被清淤任务引用。
func HasTasksForSegment(ctx context.Context, db *gorm.DB, segmentID uint) (bool, error) {
	return exists(ctx, db, TableCleaningTasks, "pipe_segment_id = ?", segmentID)
}

// HasRecordsForTask 任务下是否已经录入清淤记录。
func HasRecordsForTask(ctx context.Context, db *gorm.DB, taskID uint) (bool, error) {
	return exists(ctx, db, TableCleaningRecords, "task_id = ?", taskID)
}

// HasAcceptanceForTask 任务下是否已经有验收记录。
func HasAcceptanceForTask(ctx context.Context, db *gorm.DB, taskID uint) (bool, error) {
	return exists(ctx, db, TableAcceptanceRecords, "task_id = ?", taskID)
}

// HasAcceptanceForRecord 清淤记录是否已经被验收记录引用。
func HasAcceptanceForRecord(ctx context.Context, db *gorm.DB, recordID uint) (bool, error) {
	return exists(ctx, db, TableAcceptanceRecords, "cleaning_record_id = ?", recordID)
}

// TaskStatsForSegment 汇总某管段下各状态的任务数量。
func TaskStatsForSegment(ctx context.Context, db *gorm.DB, segmentID uint) (TaskStats, error) {
	type row struct {
		Status string
		Total  int64
	}
	var rows []row
	err := db.WithContext(ctx).Table(TableCleaningTasks).
		Select("status, COUNT(*) AS total").
		Where("pipe_segment_id = ?", segmentID).
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return TaskStats{}, err
	}
	var stats TaskStats
	for _, item := range rows {
		stats.Total += item.Total
		switch item.Status {
		case "pending":
			stats.Pending = item.Total
		case "in_progress":
			stats.InProgress = item.Total
		case "completed":
			stats.Completed = item.Total
		case "accepted":
			stats.Accepted = item.Total
		case "cancelled":
			stats.Cancelled = item.Total
		}
	}
	return stats, nil
}

// RecentTasksForSegment 查询某管段最近的任务，并带上已录入的折算清淤量与记录条数。
//
// 直接 LEFT JOIN 清淤记录后按任务 GROUP BY，折算表达式只出现在外层 SELECT：
// 先在记录层 ROUND 再求和，与任务 / 片区 / 看板共用同一段折算 SQL。
func RecentTasksForSegment(ctx context.Context, db *gorm.DB, segmentID uint, limit int) ([]TaskRef, error) {
	if limit <= 0 {
		limit = 5
	}
	perRecordSum, sumArgs := StandardSumExpr("cr")
	refs := make([]TaskRef, 0, limit)
	err := db.WithContext(ctx).Table(TableCleaningTasks+" AS t").
		Select(`t.id, t.code, t.title, t.status, t.priority, t.team_name,
			t.plan_start_date, t.plan_end_date,
			COUNT(cr.id) AS record_count,
			`+perRecordSum+` AS standard_sludge_t`, sumArgs...).
		Joins("LEFT JOIN "+TableCleaningRecords+" AS cr ON cr.task_id = t.id").
		Where("t.pipe_segment_id = ?", segmentID).
		Group("t.id").
		Order("t.plan_start_date DESC, t.id DESC").
		Limit(limit).
		Scan(&refs).Error
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return refs, nil
	}
	taskIDs := make([]uint, 0, len(refs))
	for _, ref := range refs {
		taskIDs = append(taskIDs, ref.ID)
	}
	rawByTask, err := rawAmountsGrouped(ctx, db, taskIDs)
	if err != nil {
		return nil, err
	}
	for i := range refs {
		refs[i].StandardSludgeT = num.Round2(refs[i].StandardSludgeT)
		refs[i].RawAmounts = rawByTask[refs[i].ID]
	}
	return refs, nil
}

// rawAmountsGrouped 批量汇总多个任务在各原始口径下的数值合计。
func rawAmountsGrouped(ctx context.Context, db *gorm.DB, taskIDs []uint) (map[uint][]RawAmount, error) {
	type rawRow struct {
		TaskID  uint
		Caliber string
		Amount  float64
	}
	rawRows := make([]rawRow, 0)
	if err := db.WithContext(ctx).Table(TableCleaningRecords).
		Select("task_id, "+colCaliber+" AS caliber, COALESCE(SUM("+colAmount+"), 0) AS amount").
		Where("task_id IN ?", taskIDs).
		Group("task_id, " + colCaliber).
		Scan(&rawRows).Error; err != nil {
		return nil, err
	}
	result := make(map[uint][]RawAmount, len(taskIDs))
	for _, r := range rawRows {
		result[r.TaskID] = append(result[r.TaskID], RawAmount{
			Caliber: r.Caliber,
			Unit:    sludge.Unit(r.Caliber),
			Amount:  num.Round2(r.Amount),
		})
	}
	return result, nil
}

func exists(ctx context.Context, db *gorm.DB, table, where string, args ...any) (bool, error) {
	var count int64
	if err := db.WithContext(ctx).Table(table).Where(where, args...).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
