// Package conversion 清淤量统一口径的换算规则模块。
//
// 业务背景：现场清淤量既可能按湿重计量，也可能按干重计量，单位不统一时
// 无法直接汇总。系统对外（看板、报表、各级合计）统一折算为「干重（吨）」：
//
//	干重（吨）= 原始重量（吨）× 折算系数
//	  - 原始口径为干重：系数恒为 1
//	  - 原始口径为湿重：系数取该清淤日期当天生效规则的湿重→干重折算率
//
// 规则按生效时间版本化（append-only）：某一业务日期始终命中
// effective_from <= 该日期 的最新一条规则。因此：
//   - 历史记录的折算不会因为后来调整规则而被静默改写（记录上保存命中规则快照）；
//   - 跨月补录按「清淤日期当时生效」的规则折算，而不是录入当天的规则；
//   - 新增生效规则只会影响其生效区间内（到下一条规则生效前）的记录，
//     影响范围在落库前可预览，落库后写入变更日志。
package conversion

import (
	"time"

	"github.com/drainage/desilting/internal/shared/date"
)

// 重量口径取值。
const (
	// BasisWet 湿重：现场含水土方实际称重，需要按规则折算为干重。
	BasisWet = "wet"
	// BasisDry 干重：已经是干重口径，折算系数恒为 1。
	BasisDry = "dry"
)

// UnifiedBasis 是看板与各级合计统一使用的口径名称。
const UnifiedBasis = "dry"

// UnifiedUnit 是统一口径的计量单位。
const UnifiedUnit = "吨（干重）"

// ConversionRule 清淤量湿重→干重换算规则（按生效时间版本化）。
type ConversionRule struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// Code 规则版本编号，便于在记录快照与日志中引用，例如 HG-2026-01。
	Code string `gorm:"size:32;uniqueIndex;not null" json:"code"`
	// Name 规则版本名称。
	Name string `gorm:"size:64;not null" json:"name"`
	// EffectiveFrom 生效日期（含当天）。同一天只允许存在一条规则。
	EffectiveFrom date.Date `gorm:"type:date;uniqueIndex;not null" json:"effectiveFrom"`
	// WetToDryFactor 湿重→干重折算系数（干重吨 = 湿重吨 × 系数），取值区间 (0,1]。
	WetToDryFactor float64 `gorm:"not null" json:"wetToDryFactor"`
	// Remark 编制依据 / 备注。
	Remark    string    `gorm:"type:text" json:"remark"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName 指定表名。
func (ConversionRule) TableName() string {
	return "sludge_conversion_rules"
}

// ConversionLog 换算规则变更日志：记录每次新增规则版本对既有统计的影响。
type ConversionLog struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// RuleID 本次变更对应的规则版本。
	RuleID uint `gorm:"index;not null" json:"ruleId"`
	// RuleCode 冗余规则编号，即使规则版本被引用也能直接展示。
	RuleCode string `gorm:"size:32;not null" json:"ruleCode"`
	// EffectiveFrom 冗余生效日期，便于日志独立呈现。
	EffectiveFrom date.Date `gorm:"type:date;not null" json:"effectiveFrom"`
	// WetToDryFactor 冗余本次使用的折算系数。
	WetToDryFactor float64 `gorm:"not null" json:"wetToDryFactor"`
	// WindowTo 受影响区间的截止日期（下一条规则生效日前一天）；无下一条规则时为空（开放区间）。
	WindowTo *date.Date `gorm:"type:date" json:"windowTo"`
	// AffectedRecords 受影响（被重新折算）的清淤记录条数。
	AffectedRecords int64 `gorm:"not null" json:"affectedRecords"`
	// DryBefore 受影响记录折算前的干重合计（6 位精度存储）。
	DryBefore float64 `gorm:"not null" json:"dryBefore"`
	// DryAfter 受影响记录折算后的干重合计（6 位精度存储）。
	DryAfter float64 `gorm:"not null" json:"dryAfter"`
	// Remark 变更说明。
	Remark    string    `gorm:"size:255" json:"remark"`
	CreatedAt time.Time `json:"createdAt"`
}

// TableName 指定表名。
func (ConversionLog) TableName() string {
	return "sludge_conversion_logs"
}
