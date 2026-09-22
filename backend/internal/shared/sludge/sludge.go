// Package sludge 统一维护清淤量的统计口径。
//
// 现场录入的清淤量原始值有三种计量口径：
//
//   - CaliberM3    体积口径，单位 m³（清淤车斗方量，历史数据沿用）
//   - CaliberWetT  湿重口径，单位 t（污泥脱水前称重）
//   - CaliberDryT  干重口径，单位 t（脱水后干污泥称重）
//
// 看板与报表的统一统计口径为「干重（干污泥吨）」，所有跨层级合计
// （任务 / 管段 / 片区 / 看板）都折算到该口径后再汇总。
package sludge

import (
	"math"

	"github.com/drainage/desilting/internal/shared/option"
)

// 计量口径取值。
const (
	CaliberM3   = "m3"
	CaliberWetT = "wet_t"
	CaliberDryT = "dry_t"
)

// StandardCaliber 统一统计口径：干重（干污泥吨）。
const StandardCaliber = CaliberDryT

// 换算规则支持的口径对方向：体积 -> 湿重（密度）、湿重 -> 干重（干湿系数）。
const (
	PairM3ToWet  = "m3->wet_t"
	PairWetToDry = "wet_t->dry_t"
)

// CaliberOptions 计量口径选项，随枚举字典下发给前端。
func CaliberOptions() []option.Option {
	return option.List(
		CaliberM3, "体积（m³）",
		CaliberWetT, "湿重（湿污泥 t）",
		CaliberDryT, "干重（干污泥 t）",
	)
}

// PairOptions 换算方向选项（仅用于换算规则维护页面）。
func PairOptions() []option.Option {
	return option.List(
		PairM3ToWet, "体积 → 湿重（湿污泥密度 t/m³）",
		PairWetToDry, "湿重 → 干重（干湿系数）",
	)
}

// HasCaliber 判断口径是否合法。
func HasCaliber(value string) bool {
	return option.Has(CaliberOptions(), value)
}

// CaliberLabel 返回口径的中文名称。
func CaliberLabel(value string) string {
	return option.Label(CaliberOptions(), value)
}

// PairFromTo 把换算方向拆成「源口径 / 目标口径」。
func PairFromTo(pair string) (from, to string, ok bool) {
	switch pair {
	case PairM3ToWet:
		return CaliberM3, CaliberWetT, true
	case PairWetToDry:
		return CaliberWetT, CaliberDryT, true
	default:
		return "", "", false
	}
}

// PairOf 返回由源 / 目标口径组成的方向标识，无法直接换算时返回空串。
func PairOf(from, to string) string {
	switch {
	case from == CaliberM3 && to == CaliberWetT:
		return PairM3ToWet
	case from == CaliberWetT && to == CaliberDryT:
		return PairWetToDry
	default:
		return ""
	}
}

// Unit 返回口径对应的展示单位。
func Unit(caliber string) string {
	switch caliber {
	case CaliberM3:
		return "m³"
	case CaliberWetT, CaliberDryT:
		return "t"
	default:
		return ""
	}
}

// Round2 记录级折算值统一保留 2 位小数。
//
// 折算过程只允许在记录层取整这一次；任务 / 管段 / 片区 / 看板各层级
// 都对记录级折算值求和，禁止逐层取整，避免各级四舍五入后总量对不上。
func Round2(value float64) float64 {
	return math.Round(value*100) / 100
}
