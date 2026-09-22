package conversion

import (
	"github.com/drainage/desilting/internal/shared/date"
)

// SaveRequest 新增换算规则版本的请求体（规则只增不改）。
type SaveRequest struct {
	Name           string    `json:"name" label:"规则名称" validate:"required,max=64"`
	EffectiveFrom  date.Date `json:"effectiveFrom" label:"生效日期"`
	WetToDryFactor float64   `json:"wetToDryFactor" label:"湿重折算干重系数" validate:"gt=0,lte=1"`
	Remark         string    `json:"remark" label:"备注" validate:"max=1000"`
}

// Window 一条规则的生效区间：[From, To]，To 为空表示开放区间（至今）。
type Window struct {
	From date.Date  `json:"from"`
	To   *date.Date `json:"to"`
}

// Impact 一次规则调整对既有清淤统计的影响范围。
type Impact struct {
	// EffectiveFrom 新规则的生效日期。
	EffectiveFrom date.Date `json:"effectiveFrom"`
	// WindowTo 受影响区间截止日（下一条规则生效日前一天）；为空表示至今。
	WindowTo *date.Date `json:"windowTo"`
	// AffectedRecords 区间内会被重新折算的记录条数。
	AffectedRecords int64 `json:"affectedRecords"`
	// AffectedTasks 涉及的任务数。
	AffectedTasks int64 `json:"affectedTasks"`
	// AffectedSegments 涉及的管段数。
	AffectedSegments int64 `json:"affectedSegments"`
	// AffectedDistricts 涉及的片区数。
	AffectedDistricts int64 `json:"affectedDistricts"`
	// DryBefore 折算前区间干重合计（6 位精度）。
	DryBefore float64 `json:"dryBefore"`
	// DryAfter 折算后区间干重合计（6 位精度）。
	DryAfter float64 `json:"dryAfter"`
	// Delta 折算后 - 折算前（6 位精度），正数表示口径上调。
	Delta float64 `json:"delta"`
}

// Caliber 统一口径说明，供看板/报表展示。
type Caliber struct {
	Basis       string    `json:"basis"`
	BasisLabel  string    `json:"basisLabel"`
	Unit        string    `json:"unit"`
	CurrentRule *RuleView `json:"currentRule"`
}

// RuleView 规则视图：规则本体 + 其生效区间上界。
type RuleView struct {
	ConversionRule
	// EffectiveTo 该规则生效区间的截止日；为空表示仍是当前规则。
	EffectiveTo *date.Date `json:"effectiveTo"`
}
