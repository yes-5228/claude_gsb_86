package conversion

import "github.com/drainage/desilting/internal/shared/num"

// 折算系数的合法区间。湿重含水率不可能为负，也不可能折算后反而变重。
const (
	minFactor = 0.0001
	maxFactor = 1.0
)

// Snapshot 一条清淤记录命中的换算结果（写入记录、参与各级合计的依据）。
//
// 它是一个与 ORM 无关的值对象：清淤记录模块在录入/修改时拿到它并固化到
// 记录行上，之后即使换算规则再调整，历史记录仍保留当时命中的规则与系数，
// 原始计量值与折算结果都可以同时追溯。
type Snapshot struct {
	// RuleID 命中的换算规则版本；干重口径不依赖规则，为 0。
	RuleID uint
	// RuleCode 命中规则的版本编号，用于界面直接展示依据。
	RuleCode string
	// Factor 实际使用的折算系数（干重口径恒为 1）。
	Factor float64
	// DryT 折算后的统一口径干重（吨，已按存储精度 6 位收敛）。
	DryT float64
}

// Convert 按给定口径与湿重→干重系数，把原始重量折算为统一口径干重（吨）。
//
// basis 为干重时系数恒为 1、不使用 wetFactor；为湿重时使用 wetFactor。
// 返回的干重统一经过 num.Round6 收敛，保证后续各级 SUM 严格可对账。
// 调用方需保证 wetFactor 已通过 ValidFactor 校验、basis 已通过 HasBasis 校验。
func Convert(rawWeightT float64, basis string, wetFactor float64) Snapshot {
	factor := 1.0
	if basis == BasisWet {
		factor = wetFactor
	}
	return Snapshot{
		Factor: factor,
		DryT:   num.Round6(rawWeightT * factor),
	}
}

// ValidFactor 校验湿重→干重折算系数是否在合法区间内。
func ValidFactor(factor float64) bool {
	return factor >= minFactor && factor <= maxFactor
}
