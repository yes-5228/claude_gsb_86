// Package num 提供数值处理的公共函数。
package num

import "math"

// 业务展示精度：清淤量、管长等指标统一保留 2 位小数。
const decimals = 2

// 折算口径的存储/汇总精度。
//
// 统一口径（干重吨）在写入清淤记录时即按 6 位小数固化，之后任务、管段、
// 片区、看板四级合计全部直接 SUM 这一列、中间不做任何取整，只在接口边界
// 按 2 位小数展示。这样既避免浮点累加尾差，也保证各级底层合计严格相等，
// 不会出现「每一级先各自取整、加总后对不上」的情况。
const storageDecimals = 6

// Round2 把浮点数四舍五入到 2 位小数。
//
// 浮点数累加会产生 33.599999999999994 这类尾差，接口返回前统一收敛，
// 避免前端展示与统计口径出现"看着不一致"的数字。
func Round2(value float64) float64 {
	return round(value, decimals)
}

// Round6 把折算干重收敛到 6 位小数，作为统一口径的存储与汇总精度。
//
// 每条清淤记录折算后、每次 SUM 出的合计都先经过它，等价于以 10^-6 吨
// （即 0.001 千克）为最小单位的整数运算，从根上消除浮点尾差，
// 使「子项之和」与「合计」逐位相等。
func Round6(value float64) float64 {
	return round(value, storageDecimals)
}

func round(value float64, digits int) float64 {
	factor := math.Pow(10, float64(digits))
	return math.Round(value*factor) / factor
}
