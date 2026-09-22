package conversion

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/num"
	"github.com/drainage/desilting/internal/shared/option"
	"github.com/drainage/desilting/internal/shared/refx"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// Service 换算规则业务逻辑。
type Service struct {
	repo *Repository
}

// baselineDate 系统基线规则的生效日期，与 database.baselineEffectiveFrom 一致。
var baselineDate = date.MustParse("2000-01-01")

// NewService 构造服务。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Engine 加载当前全部换算规则，返回换算引擎快照。
func (s *Service) Engine(ctx context.Context) (*sludge.Engine, error) {
	factors, err := s.repo.ListFactorRules(ctx)
	if err != nil {
		return nil, httpx.WrapInternal("加载换算规则失败", err)
	}
	return sludge.NewEngine(factors), nil
}

// StandardT 实现 cleaningrecord.SludgeGateway：单值折算到干重（干污泥 t）。
func (s *Service) StandardT(ctx context.Context, amount float64, caliber string, cleanedAt date.Date) (float64, error) {
	engine, err := s.Engine(ctx)
	if err != nil {
		return 0, err
	}
	return engine.StandardT(amount, caliber, cleanedAt), nil
}

// Preview 录入表单单值折算预览。
func (s *Service) Preview(ctx context.Context, req PreviewRequest) (*PreviewResponse, error) {
	if !sludge.HasCaliber(strings.TrimSpace(req.Caliber)) {
		return nil, httpx.Validation(fmt.Sprintf("计量口径只能是：%s", option.Labels(sludge.CaliberOptions())))
	}
	if req.Amount <= 0 || req.Amount > 1000000 {
		return nil, httpx.Validation("清淤量需大于 0 且不超过 1000000")
	}
	if req.CleanedAt.IsZero() {
		return nil, httpx.Validation("清淤日期不能为空")
	}
	engine, err := s.Engine(ctx)
	if err != nil {
		return nil, err
	}
	resp := &PreviewResponse{
		Amount:       req.Amount,
		Caliber:      strings.TrimSpace(req.Caliber),
		CleanedAt:    req.CleanedAt.String(),
		StandardT:    engine.StandardT(req.Amount, req.Caliber, req.CleanedAt),
		DensityT_M3:  engine.FactorAt(sludge.PairM3ToWet, req.CleanedAt),
		WetToDry:     engine.FactorAt(sludge.PairWetToDry, req.CleanedAt),
		FactorSource: "按清淤日期当时生效的换算规则折算",
	}
	return resp, nil
}

// List 规则列表，并标注每个版本是否最新 / 是否被引用 / 可删除。
func (s *Service) List(ctx context.Context) ([]RuleItem, error) {
	rules, err := s.repo.List(ctx)
	if err != nil {
		return nil, httpx.WrapInternal("查询换算规则失败", err)
	}
	latest := make(map[string]uint, 2)
	for i := range rules {
		latest[rules[i].Pair] = rules[i].ID // 列表按生效日期升序，最后一个即最新
	}
	items := make([]RuleItem, 0, len(rules))
	for i := range rules {
		rule := rules[i]
		from, to, _ := sludge.PairFromTo(rule.Pair)
		referenced, err := refx.IsRuleReferenced(ctx, s.repo.DB(), rule.Pair, rule.EffectiveFrom)
		if err != nil {
			return nil, httpx.WrapInternal("统计规则引用情况失败", err)
		}
		isLatest := latest[rule.Pair] == rule.ID
		item := RuleItem{
			Rule:           rule,
			FromCaliber:    from,
			ToCaliber:      to,
			Latest:         isLatest,
			Referenced:     referenced,
			Deletable:      isLatest && !referenced,
			AffectedRecord: 0,
		}
		if referenced {
			// 仅在需要时回查受影响条数，避免 N 次无谓计数。
			count, _, _, err := refx.CountRecordsAffectedByPair(ctx, s.repo.DB(), rule.Pair, rule.EffectiveFrom)
			if err != nil {
				return nil, httpx.WrapInternal("统计规则影响范围失败", err)
			}
			item.AffectedRecord = count
		}
		items = append(items, item)
	}
	return items, nil
}

// Create 新增规则版本（append-only）。返回新版本与生效后的影响范围。
func (s *Service) Create(ctx context.Context, req CreateRuleRequest) (*Rule, *ImpactSummary, error) {
	if err := validateRule(req.Pair, req.Factor, req.EffectiveFrom); err != nil {
		return nil, nil, err
	}
	pair := strings.TrimSpace(req.Pair)

	// 同一生效日期不允许重复版本；新版本生效日期必须晚于现有最新版本。
	dup, err := s.repo.FindVersion(ctx, pair, req.EffectiveFrom)
	if err != nil {
		return nil, nil, httpx.WrapInternal("校验换算规则失败", err)
	}
	if dup != nil {
		return nil, nil, httpx.Conflict(fmt.Sprintf("%s 方向在 %s 已存在生效版本，规则只能新增版本不能覆盖",
			option.Label(sludge.PairOptions(), pair), req.EffectiveFrom))
	}
	latest, err := s.repo.LatestByPair(ctx, pair)
	if err != nil {
		return nil, nil, httpx.WrapInternal("校验最新换算规则失败", err)
	}
	if latest != nil && !req.EffectiveFrom.After(latest.EffectiveFrom) {
		return nil, nil, httpx.InvalidState(fmt.Sprintf(
			"新版本生效日期必须晚于当前最新版本（%s）；调整既有规则请新增一个更晚生效的版本",
			latest.EffectiveFrom))
	}

	// 影响范围必须在落库前基于「现状规则 vs 假设新版本」试算，
	// 否则新版本入库后 before/after 会相同。
	impact, err := s.impactSummary(ctx, ImpactRequest{
		Pair: pair, Factor: req.Factor, EffectiveFrom: req.EffectiveFrom,
	})
	if err != nil {
		return nil, nil, err
	}

	rule := &Rule{
		Pair:          pair,
		Factor:        req.Factor,
		EffectiveFrom: req.EffectiveFrom,
		Remark:        strings.TrimSpace(req.Remark),
	}
	if err := s.repo.Create(ctx, rule); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, nil, httpx.Conflict("该方向在此生效日期已存在规则版本")
		}
		return nil, nil, httpx.WrapInternal("新增换算规则失败", err)
	}
	return rule, impact, nil
}

// Impact 规则影响预览（不写库）。
func (s *Service) Impact(ctx context.Context, req ImpactRequest) (*ImpactSummary, error) {
	if err := validateRule(req.Pair, req.Factor, req.EffectiveFrom); err != nil {
		return nil, err
	}
	return s.impactSummary(ctx, req)
}

// impactSummary 按「假设该版本生效」在 Go 侧重算影响范围与总量变化。
func (s *Service) impactSummary(ctx context.Context, req ImpactRequest) (*ImpactSummary, error) {
	engine, err := s.Engine(ctx)
	if err != nil {
		return nil, err
	}
	allRows, err := refx.ListAllRecordsForImpact(ctx, s.repo.DB())
	if err != nil {
		return nil, httpx.WrapInternal("读取清淤记录失败", err)
	}

	// 试算引擎 = 当前规则集，用新版本替换/追加该方向在生效日及以后的解析结果。
	rules, err := s.repo.List(ctx)
	if err != nil {
		return nil, httpx.WrapInternal("读取换算规则失败", err)
	}
	factors := make([]sludge.FactorRule, 0, len(rules)+1)
	for _, rule := range rules {
		if req.RuleID != nil && rule.ID == *req.RuleID {
			continue
		}
		factors = append(factors, sludge.FactorRule{
			Pair: rule.Pair, Factor: rule.Factor, EffectiveFrom: rule.EffectiveFrom,
		})
	}
	factors = append(factors, sludge.FactorRule{
		Pair: req.Pair, Factor: req.Factor, EffectiveFrom: req.EffectiveFrom,
	})
	afterEngine := sludge.NewEngine(factors)

	summary := &ImpactSummary{
		Pair:              req.Pair,
		Factor:            req.Factor,
		EffectiveFrom:     req.EffectiveFrom.String(),
		AffectedDistricts: []string{},
		Samples:           []AffectedRecordSample{},
	}

	districtSet := map[string]struct{}{}
	taskSet := map[uint]struct{}{}
	sampleCodes := map[uint]bool{}

	var affectedBefore, affectedAfter float64
	for _, row := range allRows {
		recordCaliber := row.Caliber
		if !caliberTouchedByPair(req.Pair, recordCaliber) {
			continue
		}
		if row.CleanedAt.Before(req.EffectiveFrom) {
			continue
		}
		before := engine.StandardT(row.Amount, row.Caliber, row.CleanedAt)
		after := afterEngine.StandardT(row.Amount, row.Caliber, row.CleanedAt)
		summary.AffectedRecord++
		taskSet[row.TaskID] = struct{}{}
		if row.District != "" {
			districtSet[row.District] = struct{}{}
		}
		affectedBefore += before
		affectedAfter += after
	}

	// 全局总量（新规则生效前 / 生效后）：所有记录各自折算后求和，只对最终结果收敛浮点尾差。
	var grandBefore, grandAfter float64
	for _, row := range allRows {
		grandBefore += engine.StandardT(row.Amount, row.Caliber, row.CleanedAt)
		grandAfter += afterEngine.StandardT(row.Amount, row.Caliber, row.CleanedAt)
	}
	summary.BeforeStandardT = num.Round2(affectedBefore)
	summary.AfterStandardT = num.Round2(affectedAfter)
	summary.GrandTotalBeforeT = num.Round2(grandBefore)
	summary.GrandTotalAfterT = num.Round2(grandAfter)
	summary.DeltaStandardT = num.Round2(affectedAfter - affectedBefore)
	summary.AffectedTask = int64(len(taskSet))
	for district := range districtSet {
		summary.AffectedDistricts = append(summary.AffectedDistricts, district)
	}
	sortStrings(summary.AffectedDistricts)

	// 样例：受影响记录最新 10 条（含任务编号由调用方补充 code 已在查询中带出）。
	samples, err := refx.RecordsAffectedByPair(ctx, s.repo.DB(), req.Pair, req.EffectiveFrom, 10)
	if err != nil {
		return nil, httpx.WrapInternal("读取受影响记录样例失败", err)
	}
	for _, row := range samples {
		if sampleCodes[row.ID] {
			continue
		}
		sampleCodes[row.ID] = true
		before := engine.StandardT(row.Amount, row.Caliber, row.CleanedAt)
		after := afterEngine.StandardT(row.Amount, row.Caliber, row.CleanedAt)
		summary.Samples = append(summary.Samples, AffectedRecordSample{
			RecordID:  row.ID,
			Code:      row.Code,
			TaskID:    row.TaskID,
			CleanedAt: row.CleanedAt.String(),
			District:  row.District,
			Caliber:   row.Caliber,
			RawAmount: num.Round2(row.Amount),
			BeforeT:   before,
			AfterT:    after,
			DeltaT:    num.Round2(after - before),
		})
	}
	return summary, nil
}

// Delete 删除规则版本：只允许删除未被引用的最新（未来）版本；基线规则受保护。
func (s *Service) Delete(ctx context.Context, id uint) error {
	rule, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return httpx.NotFound("换算规则版本不存在")
		}
		return httpx.WrapInternal("查询换算规则失败", err)
	}
	latest, err := s.repo.LatestByPair(ctx, rule.Pair)
	if err != nil {
		return httpx.WrapInternal("校验最新换算规则失败", err)
	}
	if latest == nil || latest.ID != rule.ID {
		return httpx.InvalidState("只能删除该方向的最新版本；历史版本用于保证既有统计可追溯，不能删除")
	}
	if !rule.EffectiveFrom.After(baselineDate) {
		return httpx.InvalidState("系统基线规则是全部历史数据折算的兜底版本，不能删除")
	}
	referenced, err := refx.IsRuleReferenced(ctx, s.repo.DB(), rule.Pair, rule.EffectiveFrom)
	if err != nil {
		return httpx.WrapInternal("校验规则引用失败", err)
	}
	if referenced {
		return httpx.Conflict("该版本已覆盖实际清淤记录，不能删除，只能新增更正版本")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return httpx.WrapInternal("删除换算规则失败", err)
	}
	return nil
}

// validateRule 校验规则入参。
func validateRule(pair string, factor float64, effectiveFrom date.Date) error {
	if _, _, ok := sludge.PairFromTo(strings.TrimSpace(pair)); !ok {
		return httpx.Validation(fmt.Sprintf("换算方向只能是：%s", option.Labels(sludge.PairOptions())))
	}
	if factor <= 0 || factor > 100 {
		return httpx.Validation("换算系数需大于 0 且不超过 100")
	}
	if effectiveFrom.IsZero() {
		return httpx.Validation("生效日期不能为空")
	}
	return nil
}

// caliberTouchedByPair 某口径的记录折算链是否经过指定方向。
func caliberTouchedByPair(pair, caliber string) bool {
	switch pair {
	case sludge.PairM3ToWet:
		return caliber == sludge.CaliberM3
	case sludge.PairWetToDry:
		return caliber == sludge.CaliberM3 || caliber == sludge.CaliberWetT
	default:
		return false
	}
}

// sortStrings 避免在文件顶部引入 sort 仅用于一处。
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}
