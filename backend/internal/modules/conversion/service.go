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
	"github.com/drainage/desilting/internal/shared/refx"
)

// Service 换算规则与统一口径折算。
type Service struct {
	repo *Repository
	db   *gorm.DB
}

// NewService 构造服务。
func NewService(repo *Repository) *Service {
	return &Service{repo: repo, db: repo.DB()}
}

// EnsureSeed 确保系统至少存在一条规则（默认规则），供初始化与测试调用。
// 已有任意规则时不做改动。返回最终生效的基线规则。
func (s *Service) EnsureSeed(ctx context.Context, fallback ConversionRule) (*ConversionRule, error) {
	latest, err := s.repo.LatestRule(ctx)
	if err != nil {
		return nil, err
	}
	if latest != nil {
		return latest, nil
	}
	if fallback.EffectiveFrom.IsZero() {
		return nil, httpx.Validation("默认规则生效日期不能为空")
	}
	if !ValidFactor(fallback.WetToDryFactor) {
		return nil, httpx.Validation("默认规则湿重折算干重系数需在 0.0001 ~ 1 之间")
	}
	if strings.TrimSpace(fallback.Code) == "" {
		fallback.Code = ruleCode(fallback.EffectiveFrom)
	}
	if strings.TrimSpace(fallback.Name) == "" {
		fallback.Name = "默认湿重折算规则"
	}
	if err := s.repo.CreateRule(ctx, &fallback); err != nil {
		return nil, err
	}
	return &fallback, nil
}

// ResolveAt 返回某业务日期应使用的换算规则快照（供清淤记录录入/修改时调用）。
// 干重口径不调用本方法；湿重口径必须命中一条规则，否则报错提示先配置规则。
func (s *Service) ResolveAt(ctx context.Context, day date.Date) (Snapshot, error) {
	if day.IsZero() {
		return Snapshot{}, httpx.Validation("清淤日期为空，无法确定折算规则")
	}
	rule, err := s.repo.EffectiveAt(ctx, day)
	if err != nil {
		return Snapshot{}, httpx.WrapInternal("查询折算规则失败", err)
	}
	if rule == nil {
		return Snapshot{}, httpx.InvalidState(fmt.Sprintf(
			"清淤日期 %s 尚未配置生效的湿重折算规则，请先在换算规则中登记一条不晚于该日期的规则", day.String()))
	}
	snap := Convert(1, BasisWet, rule.WetToDryFactor)
	snap.RuleID = rule.ID
	snap.RuleCode = rule.Code
	snap.Factor = rule.WetToDryFactor
	snap.DryT = 0 // 系数快照本身不携带干重结果，由记录侧用原始重量另行折算。
	return snap, nil
}

// ResolveForRecord 是给清淤记录模块用的便捷方法：按口径与业务日期折算原始重量。
//
// basis 为干重时系数恒为 1、不查规则；为湿重时解析当天生效规则并折算。
// 返回的 Snapshot 已包含 RuleID / RuleCode / Factor / DryT，可直接固化到记录上。
func (s *Service) ResolveForRecord(ctx context.Context, rawWeightT float64, basis string, day date.Date) (Snapshot, error) {
	if !HasBasis(basis) {
		return Snapshot{}, httpx.Validation("重量口径只能是湿重或干重")
	}
	if basis == BasisDry {
		snap := Convert(rawWeightT, BasisDry, 1)
		return snap, nil
	}
	rule, err := s.repo.EffectiveAt(ctx, day)
	if err != nil {
		return Snapshot{}, httpx.WrapInternal("查询折算规则失败", err)
	}
	if rule == nil {
		return Snapshot{}, httpx.InvalidState(fmt.Sprintf(
			"清淤日期 %s 尚未配置生效的湿重折算规则，请先登记一条不晚于该日期的规则", day.String()))
	}
	snap := Convert(rawWeightT, BasisWet, rule.WetToDryFactor)
	snap.RuleID = rule.ID
	snap.RuleCode = rule.Code
	return snap, nil
}

// ListRules 返回规则版本列表，并为每条规则标注生效区间上界。
func (s *Service) ListRules(ctx context.Context) ([]RuleView, error) {
	rules, err := s.repo.ListRules(ctx)
	if err != nil {
		return nil, httpx.WrapInternal("查询折算规则失败", err)
	}
	// rules 已按生效日期倒序；对每条规则，其上一条（更晚生效）规则的生效日前一天即区间上界。
	views := make([]RuleView, 0, len(rules))
	for i := range rules {
		view := RuleView{ConversionRule: rules[i]}
		if i > 0 {
			to := rules[i-1].EffectiveFrom.AddDays(-1)
			view.EffectiveTo = &to
		}
		views = append(views, view)
	}
	return views, nil
}

// ListLogs 返回规则变更日志。
func (s *Service) ListLogs(ctx context.Context, limit int) ([]ConversionLog, error) {
	logs, err := s.repo.ListLogs(ctx, limit)
	if err != nil {
		return nil, httpx.WrapInternal("查询折算规则变更日志失败", err)
	}
	return logs, nil
}

// Caliber 返回当前统一口径说明与当前生效规则，供看板/报表标注口径。
func (s *Service) Caliber(ctx context.Context) (*Caliber, error) {
	rules, err := s.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	caliber := newCaliber()
	if len(rules) > 0 {
		current := rules[0] // 倒序第一条为当前规则，EffectiveTo 为空。
		caliber.CurrentRule = &current
	}
	return caliber, nil
}

func newCaliber() *Caliber {
	return &Caliber{
		Basis:      UnifiedBasis,
		BasisLabel: BasisLabel(UnifiedBasis),
		Unit:       UnifiedUnit,
	}
}

// PreviewImpact 预览新增一条规则版本对既有统计的影响，不写任何数据。
func (s *Service) PreviewImpact(ctx context.Context, req SaveRequest) (*Impact, error) {
	if err := validateRule(req); err != nil {
		return nil, err
	}
	from := req.EffectiveFrom
	exists, err := s.repo.ExistsEffectiveFrom(ctx, from, 0)
	if err != nil {
		return nil, httpx.WrapInternal("校验规则生效日期失败", err)
	}
	if exists {
		return nil, httpx.Conflict(fmt.Sprintf("生效日期 %s 已存在规则版本，同一天只能有一个版本", from.String()))
	}
	next, err := s.repo.NextRule(ctx, from)
	if err != nil {
		return nil, httpx.WrapInternal("查询规则区间失败", err)
	}

	var to *date.Date
	if next != nil {
		t := next.EffectiveFrom.AddDays(-1)
		to = &t
	}
	return s.computeImpact(ctx, from, to, req.WetToDryFactor)
}

// CreateRule 新增规则版本，并在同一事务内重算其生效区间内既有湿重记录的折算值，
// 同时写入一条变更日志。原始计量值不会被改动。
func (s *Service) CreateRule(ctx context.Context, req SaveRequest) (*ConversionRule, *Impact, error) {
	if err := validateRule(req); err != nil {
		return nil, nil, err
	}
	from := req.EffectiveFrom
	exists, err := s.repo.ExistsEffectiveFrom(ctx, from, 0)
	if err != nil {
		return nil, nil, httpx.WrapInternal("校验规则生效日期失败", err)
	}
	if exists {
		return nil, nil, httpx.Conflict(fmt.Sprintf("生效日期 %s 已存在规则版本，同一天只能有一个版本", from.String()))
	}

	rule := &ConversionRule{
		Code:           ruleCode(from),
		Name:           strings.TrimSpace(req.Name),
		EffectiveFrom:  from,
		WetToDryFactor: req.WetToDryFactor,
		Remark:         strings.TrimSpace(req.Remark),
	}

	// 影响区间上界（下一条规则生效日前一天）在落库前后一致，放在事务外查询，
	// 避免单连接数据库在事务内再发起查询造成自锁。
	next, err := s.repo.NextRule(ctx, from)
	if err != nil {
		return nil, nil, httpx.WrapInternal("查询规则区间失败", err)
	}
	var to *date.Date
	if next != nil {
		t := next.EffectiveFrom.AddDays(-1)
		to = &t
	}

	var impact *Impact
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rule).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return httpx.Conflict("规则版本编号或生效日期冲突，请刷新后重试")
			}
			return httpx.WrapInternal("新增折算规则失败", err)
		}

		impact, err = s.computeImpactTx(ctx, tx, from, to, rule)
		if err != nil {
			return err
		}

		log := &ConversionLog{
			RuleID:          rule.ID,
			RuleCode:        rule.Code,
			EffectiveFrom:   rule.EffectiveFrom,
			WetToDryFactor:  rule.WetToDryFactor,
			WindowTo:        to,
			AffectedRecords: impact.AffectedRecords,
			DryBefore:       impact.DryBefore,
			DryAfter:        impact.DryAfter,
			Remark:          rule.Name,
		}
		if err := tx.Create(log).Error; err != nil {
			return httpx.WrapInternal("写入折算规则变更日志失败", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return rule, impact, nil
}

// computeImpact / computeImpactTx 计算规则在 [from,to] 内的影响，并用新系数重算。
func (s *Service) computeImpact(ctx context.Context, from date.Date, to *date.Date, factor float64) (*Impact, error) {
	row, err := refx.ConversionImpact(ctx, s.db, from, to)
	if err != nil {
		return nil, httpx.WrapInternal("统计规则影响范围失败", err)
	}
	wetRows, err := refx.WetConversionsInWindow(ctx, s.db, from, to)
	if err != nil {
		return nil, httpx.WrapInternal("读取待折算记录失败", err)
	}
	dryAfter := sumDryAfter(wetRows, factor)
	return buildImpact(from, to, row, dryAfter), nil
}

func (s *Service) computeImpactTx(ctx context.Context, tx *gorm.DB, from date.Date, to *date.Date, rule *ConversionRule) (*Impact, error) {
	row, err := refx.ConversionImpact(ctx, tx, from, to)
	if err != nil {
		return nil, httpx.WrapInternal("统计规则影响范围失败", err)
	}
	wetRows, err := refx.WetConversionsInWindow(ctx, tx, from, to)
	if err != nil {
		return nil, httpx.WrapInternal("读取待折算记录失败", err)
	}

	// 用统一换算引擎逐行重算并落库，保证与录入路径完全同一套算法、同一精度。
	for _, item := range wetRows {
		snap := Convert(item.RawWeightT, BasisWet, rule.WetToDryFactor)
		if err := refx.ApplyConversion(ctx, tx, item.ID, rule.ID, rule.WetToDryFactor, snap.DryT); err != nil {
			return nil, httpx.WrapInternal("重新折算清淤量失败", err)
		}
	}
	dryAfter := sumDryAfter(wetRows, rule.WetToDryFactor)
	return buildImpact(from, to, row, dryAfter), nil
}

func sumDryAfter(rows []refx.WetConversionRow, factor float64) float64 {
	total := 0.0
	for _, item := range rows {
		total += Convert(item.RawWeightT, BasisWet, factor).DryT
	}
	return num.Round6(total)
}

func buildImpact(from date.Date, to *date.Date, row refx.ConversionImpactRow, dryAfter float64) *Impact {
	return &Impact{
		EffectiveFrom:     from,
		WindowTo:          to,
		AffectedRecords:   row.RecordCount,
		AffectedTasks:     row.TaskCount,
		AffectedSegments:  row.SegmentCount,
		AffectedDistricts: row.DistrictCount,
		DryBefore:         num.Round6(row.DryBefore),
		DryAfter:          dryAfter,
		Delta:             num.Round6(dryAfter - num.Round6(row.DryBefore)),
	}
}

func validateRule(req SaveRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return httpx.Validation("规则名称不能为空")
	}
	if req.EffectiveFrom.IsZero() {
		return httpx.Validation("生效日期不能为空")
	}
	if !ValidFactor(req.WetToDryFactor) {
		return httpx.Validation("湿重折算干重系数需在 0.0001 ~ 1 之间")
	}
	return nil
}

// ruleCode 生成形如 HG-20260101 的规则版本编号。
func ruleCode(from date.Date) string {
	return "HG-" + from.Format("20060102")
}
