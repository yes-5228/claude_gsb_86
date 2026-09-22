package refx

import (
	"fmt"

	"github.com/drainage/desilting/internal/shared/sludge"
)

// TableSludgeRules 换算规则表名，必须与 conversion.Rule.TableName() 一致。
const TableSludgeRules = "sludge_conversion_rules"

// 清淤记录表的原始清淤量列（原始计量口径 + 原始数值）。
const (
	colCaliber = "sludge_caliber"
	colAmount  = "sludge_amount"
)

// aliasRecords 聚合清淤记录时统一使用的表别名。
const aliasRecords = "r"

// RawAmountColumn 返回清淤量原始数值列名（供外模块拼 SELECT 使用）。
func RawAmountColumn() string { return colAmount }

// RawCaliberColumn 返回清淤量原始口径列名（供外模块拼 SELECT 使用）。
func RawCaliberColumn() string { return colCaliber }

// factorExpr 返回「指定方向在某清淤日期生效的换算系数」SQL 表达式。
//
// 规则解析逻辑与 sludge.Engine.FactorAt 完全一致：取 effective_from <= 清淤日期
// 的最新版本；若清淤日期早于首个版本，COALESCE 回退到最早版本。换算规则全表
// 数量很少（每个方向只有寥寥几个版本），故用相关标量子查询实现，SQLite /
// PostgreSQL 通用。子查询别名固定为 fr / fe，同一 SELECT 中可被多次引用
// （每个括号子查询拥有独立的 FROM 作用域）。
//
// pair 全部来自 sludge 包常量，不接受外部输入，不存在注入面。
func factorExpr(pair, cleanedAtCol string) string {
	latest := `(SELECT fr.factor FROM ` + TableSludgeRules + ` AS fr
		WHERE fr.pair = ? AND fr.effective_from <= ` + cleanedAtCol + `
		ORDER BY fr.effective_from DESC, fr.id DESC LIMIT 1)`
	earliest := `(SELECT fe.factor FROM ` + TableSludgeRules + ` AS fe
		WHERE fe.pair = ?
		ORDER BY fe.effective_from ASC, fe.id ASC LIMIT 1)`
	return fmt.Sprintf("COALESCE(%s, %s)", latest, earliest)
}

// factorArgs 与 factorExpr 中的两个占位符一一对应（同一方向出现两次）。
func factorArgs(pair string) []any {
	return []any{pair, pair}
}

// StandardPerRecordExpr 返回「单条清淤记录折算到干重（干污泥 t）」的 SQL 片段。
//
// 体积 ->(密度)-> 湿重 ->(干湿系数)-> 干重；湿重只乘干湿系数；干重原值直接使用。
// ROUND 在记录层完成，是折算链路唯一的取整点。alias 为清淤记录表别名。
//
// 外层用 CAST(... AS NUMERIC) 包裹后再 ROUND：PostgreSQL 没有
// round(double precision, integer) 重载，只有 round(numeric, integer)；
// 标准 SQL 的 CAST AS NUMERIC 在 SQLite 与 PostgreSQL 上都可用。
//
// 占位符在 SQL 中的出现顺序与返回的 args 顺序严格对应：
//
//	WHEN caliber = m3 THEN amount * m3Factor * wetDryFactor
//	WHEN caliber = wet_t THEN amount * wetDryFactor
//	ELSE amount
func StandardPerRecordExpr(alias string) (string, []any) {
	amount := alias + "." + colAmount
	caliber := alias + "." + colCaliber
	cleanedAt := alias + ".cleaned_at"

	m3Factor := factorExpr(sludge.PairM3ToWet, cleanedAt)
	wetDryFactor := factorExpr(sludge.PairWetToDry, cleanedAt)

	expr := fmt.Sprintf(`ROUND(CAST(CASE
		WHEN %s = ? THEN %s * %s * %s
		WHEN %s = ? THEN %s * %s
		ELSE %s END AS NUMERIC), 2)`,
		caliber, amount, m3Factor, wetDryFactor,
		caliber, amount, wetDryFactor, amount,
	)

	args := make([]any, 0, 8)
	args = append(args, sludge.CaliberM3)
	args = append(args, factorArgs(sludge.PairM3ToWet)...)
	args = append(args, factorArgs(sludge.PairWetToDry)...)
	args = append(args, sludge.CaliberWetT)
	args = append(args, factorArgs(sludge.PairWetToDry)...)
	return expr, args
}

// StandardSumExpr 返回「对一组记录的干重折算值求和」的 SQL 片段。
//
// 必须先在记录层 ROUND 再 SUM，绝不能写成 SUM(原始值 * 系数) 后再取整；
// 任务 / 管段 / 片区 / 看板四个层级都用同一个片段汇总，保证合计完全一致。
func StandardSumExpr(recordAlias string) (string, []any) {
	perRecord, args := StandardPerRecordExpr(recordAlias)
	return "COALESCE(SUM(" + perRecord + "), 0)", args
}
