package sludge

import (
	"sort"
	"time"

	"github.com/drainage/desilting/internal/shared/date"
)

// FactorRule 一条换算系数（与 conversion 模块的表记录对应，
// 在这里用最小结构表达，避免共享包反向依赖业务模块）。
type FactorRule struct {
	Pair          string
	Factor        float64
	EffectiveFrom date.Date
}

// Engine 按生效日期解析换算系数的不可变快照。
//
// 换算系数按「方向 + 清淤日期」解析：取 effective_from <= 清淤日期 的最新版本；
// 若清淤日期早于该方向的首个版本，则回退到最早版本（系统基线规则，
// 实际由迁移阶段写入的 2000-01-01 基线版本兜底，正常不会触发）。
type Engine struct {
	versions map[string][]FactorRule
}

// NewEngine 由一组换算规则构造引擎。
func NewEngine(rules []FactorRule) *Engine {
	engine := &Engine{versions: make(map[string][]FactorRule, 2)}
	for _, rule := range rules {
		engine.versions[rule.Pair] = append(engine.versions[rule.Pair], rule)
	}
	for pair := range engine.versions {
		sort.Slice(engine.versions[pair], func(i, j int) bool {
			return engine.versions[pair][i].EffectiveFrom.Before(engine.versions[pair][j].EffectiveFrom)
		})
	}
	return engine
}

// FactorAt 返回指定方向在某清淤日期生效的换算系数。
func (e *Engine) FactorAt(pair string, day date.Date) float64 {
	versions := e.versions[pair]
	if len(versions) == 0 {
		return 0
	}
	// 版本已按生效日期升序；取最后一个 effective_from <= day 的版本。
	index := -1
	for i := range versions {
		if !day.Before(versions[i].EffectiveFrom) {
			index = i
			continue
		}
		break
	}
	if index < 0 {
		// 清淤日期早于首个版本：回退到最早版本。
		return versions[0].Factor
	}
	return versions[index].Factor
}

// StandardT 把原始清淤量折算为统一口径（干重，干污泥 t）。
//
// 体积 ->(密度)-> 湿重 ->(干湿系数)-> 干重；本身就是干重口径的原值直接使用。
// 结果在记录层保留 2 位小数，这是整个折算链路唯一的取整点。
func (e *Engine) StandardT(amount float64, caliber string, cleanedAt date.Date) float64 {
	standard := amount
	switch caliber {
	case CaliberM3:
		standard = amount * e.FactorAt(PairM3ToWet, cleanedAt) * e.FactorAt(PairWetToDry, cleanedAt)
	case CaliberWetT:
		standard = amount * e.FactorAt(PairWetToDry, cleanedAt)
	case CaliberDryT:
		// 已是统一口径，无需换算。
	}
	return Round2(standard)
}

// RawRecord 参与折算的最小记录结构。
type RawRecord struct {
	Amount    float64
	Caliber   string
	CleanedAt date.Date
}

// EvaluatedRecord 折算结果（原始值 + 折算值）。
type EvaluatedRecord struct {
	StandardT float64
}

// Evaluate 批量折算，返回顺序与入参一致。
func (e *Engine) Evaluate(records []RawRecord) []EvaluatedRecord {
	out := make([]EvaluatedRecord, len(records))
	for i := range records {
		out[i].StandardT = e.StandardT(records[i].Amount, records[i].Caliber, records[i].CleanedAt)
	}
	return out
}

// EffectiveSince 返回指定方向在给定日期生效的版本生效日期（用于影响分析）。
func (e *Engine) effectiveVersion(pair string, day date.Date) (FactorRule, bool) {
	versions := e.versions[pair]
	index := -1
	for i := range versions {
		if !day.Before(versions[i].EffectiveFrom) {
			index = i
			continue
		}
		break
	}
	if index < 0 {
		if len(versions) > 0 {
			return versions[0], true
		}
		return FactorRule{}, false
	}
	return versions[index], true
}

// LatestEffectiveFrom 返回指定方向最新版本的生效日期；没有任何版本时返回零值。
func (e *Engine) LatestEffectiveFrom(pair string) (time.Time, bool) {
	versions := e.versions[pair]
	if len(versions) == 0 {
		return time.Time{}, false
	}
	return versions[len(versions)-1].EffectiveFrom.Time, true
}
