// Package cleaningrecord 清淤记录录入模块：记录每次实际清淤的作业数据。
package cleaningrecord

import (
	"time"

	"github.com/drainage/desilting/internal/shared/date"
)

// CleaningRecord 清淤记录。
type CleaningRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Code           string    `gorm:"size:32;uniqueIndex;not null" json:"code"`
	TaskID         uint      `gorm:"index;not null" json:"taskId"`
	CleanedAt      date.Date `gorm:"type:date;index;not null" json:"cleanedAt"`
	LengthM        float64   `gorm:"not null" json:"lengthM"`
	SludgeVolumeM3 float64   `json:"sludgeVolumeM3"`
	// RawWeightT 现场计量的原始清淤量（吨），按录入值原样保存，任何折算都不会改写它。
	RawWeightT float64 `gorm:"not null;default:0" json:"rawWeightT"`
	// WeightBasis 原始计量口径：wet 湿重 / dry 干重。
	WeightBasis string `gorm:"size:8;not null;default:wet" json:"weightBasis"`
	// ConversionRuleID 折算命中的规则版本；原始口径为干重时为 0。
	ConversionRuleID uint `gorm:"index;not null;default:0" json:"conversionRuleId"`
	// ConversionRuleCode 命中规则的版本编号快照，便于界面直接展示折算依据。
	ConversionRuleCode string `gorm:"size:32" json:"conversionRuleCode"`
	// ConversionFactor 实际使用的折算系数（干重口径恒为 1），随记录固化。
	ConversionFactor float64 `gorm:"not null;default:1" json:"conversionFactor"`
	// ConvertedDryT 折算到统一口径（干重吨）的结果，按 6 位精度固化，
	// 是任务/管段/片区/看板四级合计唯一汇总的叶子列。
	ConvertedDryT      float64   `gorm:"not null;default:0" json:"convertedDryT"`
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
}

// TableName 指定表名。
func (CleaningRecord) TableName() string {
	return "cleaning_records"
}
