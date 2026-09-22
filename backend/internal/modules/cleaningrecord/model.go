// Package cleaningrecord 清淤记录录入模块：记录每次实际清淤的作业数据。
package cleaningrecord

import (
	"time"

	"github.com/drainage/desilting/internal/shared/date"
)

// CleaningRecord 清淤记录。
//
// 清淤量只保存现场原始计量值 SludgeAmount 与原始口径 SludgeCaliber，
// 录入后（在记录仍可编辑的任务状态下）允许修正；一旦任务进入验收流程即冻结。
// 折算到统一口径（干重 t）的数值不落库，查询时按清淤日期当时生效的换算规则
// 动态计算，规则调整后历史统计可解释、可追溯。
type CleaningRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Code      string    `gorm:"size:32;uniqueIndex;not null" json:"code"`
	TaskID    uint      `gorm:"index;not null" json:"taskId"`
	CleanedAt date.Date `gorm:"type:date;index;not null" json:"cleanedAt"`
	LengthM   float64   `gorm:"not null" json:"lengthM"`
	// default 标签保证已有数据表可平滑加列（SQLite 不能加无默认值的 NOT NULL 列）；
	// 回填迁移 backfillLegacySludge 会立刻用 sludge_volume_m3 覆盖占位值。
	SludgeAmount       float64   `gorm:"not null;default:0" json:"sludgeAmount"`
	SludgeCaliber      string    `gorm:"size:16;not null;default:m3" json:"sludgeCaliber"`
	WaterVolumeM3      float64   `json:"waterVolumeM3"`
	PersonnelCount     int       `gorm:"not null" json:"personnelCount"`
	Method             string    `gorm:"size:24" json:"method"`
	Equipment          string    `gorm:"size:128" json:"equipment"`
	Weather            string    `gorm:"size:16" json:"weather"`
	SludgeDisposalSite string    `gorm:"size:128" json:"sludgeDisposalSite"`
	SafetyMeasures     string    `gorm:"type:text" json:"safetyMeasures"`
	ProblemFound       string    `gorm:"type:text" json:"problemFound"`
	RecorderName       string    `gorm:"size:32" json:"recorderName"`
	Remark             string    `gorm:"type:text" json:"remark"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`

	// StandardT 按清淤日期当时生效规则折算的干重（干污泥 t），仅查询时填充，不落库。
	StandardT float64 `gorm:"-" json:"standardT"`
}

// TableName 指定表名。
func (CleaningRecord) TableName() string {
	return "cleaning_records"
}
