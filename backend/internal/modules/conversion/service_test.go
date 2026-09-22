package conversion_test

import (
	"context"
	"testing"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningrecord"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/sludge"
	"github.com/drainage/desilting/internal/testsupport"
)

func cleaningRecordReq(taskID uint, day date.Date, amount float64, caliber string) cleaningrecord.SaveRequest {
	return cleaningrecord.SaveRequest{
		TaskID:         taskID,
		CleanedAt:      day,
		LengthM:        10,
		SludgeAmount:   amount,
		SludgeCaliber:  caliber,
		WaterVolumeM3:  1,
		PersonnelCount: 3,
		Method:         cleaningtask.MethodHighPressure,
		Weather:        cleaningrecord.WeatherSunny,
		RecorderName:   "测试",
	}
}

func TestBaselineRulesSeeded(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := testsupport.NewServices(db)
	ctx := context.Background()

	engine, err := svc.Conversion.Engine(ctx)
	if err != nil {
		t.Fatalf("加载引擎失败: %v", err)
	}
	if got := engine.FactorAt(sludge.PairM3ToWet, date.MustParse("2020-01-01")); got != 1.40 {
		t.Fatalf("基线密度期望 1.40，实际 %v", got)
	}
	if got := engine.FactorAt(sludge.PairWetToDry, date.MustParse("2020-01-01")); got != 0.40 {
		t.Fatalf("基线干湿系数期望 0.40，实际 %v", got)
	}
}

func TestCreateRuleVersionAppendOnly(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := testsupport.NewServices(db)
	ctx := context.Background()

	// 生效日期早于已有版本（基线 2000-01-01）应被拒绝。
	_, _, err := svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          sludge.PairWetToDry,
		Factor:        0.30,
		EffectiveFrom: date.MustParse("1999-01-01"),
	})
	testsupport.RequireAppError(t, err, httpx.CodeInvalidState)

	// 合法新版本。
	_, impact, err := svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          sludge.PairWetToDry,
		Factor:        0.50,
		EffectiveFrom: date.MustParse("2026-06-01"),
		Remark:        "年中调整",
	})
	if err != nil {
		t.Fatalf("新增版本失败: %v", err)
	}
	if impact == nil {
		t.Fatal("应返回影响范围")
	}

	// 同一生效日期重复版本冲突。
	_, _, err = svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          sludge.PairWetToDry,
		Factor:        0.55,
		EffectiveFrom: date.MustParse("2026-06-01"),
	})
	testsupport.RequireAppError(t, err, httpx.CodeConflict)

	// 新版本不能早于最新版本。
	_, _, err = svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          sludge.PairWetToDry,
		Factor:        0.55,
		EffectiveFrom: date.MustParse("2026-05-01"),
	})
	testsupport.RequireAppError(t, err, httpx.CodeInvalidState)

	// 非法口径方向 / 系数。
	_, _, err = svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          "m3->dry_t",
		Factor:        1,
		EffectiveFrom: date.MustParse("2027-01-01"),
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
	_, _, err = svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          sludge.PairWetToDry,
		Factor:        0,
		EffectiveFrom: date.MustParse("2027-01-01"),
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}

func TestRuleImpactScopedByEffectiveDateAndCaliber(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := testsupport.NewServices(db)
	ctx := context.Background()

	seg := svc.CreateSegment(t, "PS-I-001", "示范区")
	task := svc.CreateTask(t, seg.ID, "影响分析任务")

	// 调整日 2026-06-01 之前 1 条 wet_t（10 t）、之后 1 条 wet_t（10 t）；
	// 另有一条 m3 记录（5 m³）在生效日后，干湿系数调整同时影响它的折算链末端。
	before := date.MustParse("2026-05-31")
	after := date.MustParse("2026-06-02")
	f := svc
	_, err := f.Records.Create(ctx, cleaningRecordReq(task.ID, before, 10, sludge.CaliberWetT))
	if err != nil {
		t.Fatalf("录入失败: %v", err)
	}
	if _, err := f.Records.Create(ctx, cleaningRecordReq(task.ID, after, 10, sludge.CaliberWetT)); err != nil {
		t.Fatalf("录入失败: %v", err)
	}
	if _, err := f.Records.Create(ctx, cleaningRecordReq(task.ID, after, 5, sludge.CaliberM3)); err != nil {
		t.Fatalf("录入失败: %v", err)
	}

	impact, err := svc.Conversion.Impact(ctx, conversion.ImpactRequest{
		Pair:          sludge.PairWetToDry,
		Factor:        0.50,
		EffectiveFrom: date.MustParse("2026-06-01"),
	})
	if err != nil {
		t.Fatalf("影响预览失败: %v", err)
	}
	// 生效日及之后：10 wet_t + 5 m3 共 2 条受影响；5-31 的旧记录不受影响。
	if impact.AffectedRecord != 2 {
		t.Fatalf("期望受影响 2 条，实际 %d", impact.AffectedRecord)
	}
	if impact.AffectedTask != 1 {
		t.Fatalf("期望涉及 1 个任务，实际 %d", impact.AffectedTask)
	}
	// wet_t: 10*(0.5-0.4)=1；m3: 5*1.4*(0.5-0.4)=0.7；合计 +1.7 t。
	if impact.DeltaStandardT != 1.7 {
		t.Fatalf("期望影响增量 1.7 t，实际 %v", impact.DeltaStandardT)
	}
	if len(impact.Samples) != 2 {
		t.Fatalf("期望 2 条样例，实际 %d", len(impact.Samples))
	}
	// 全局总量：调整前 10*.4 + 10*.4 + 5*1.4*.4 = 4+4+2.8=10.8；
	// 调整后 4 + 5 + 3.5 = 12.5。
	if impact.GrandTotalBeforeT != 10.8 {
		t.Fatalf("调整前总量期望 10.8，实际 %v", impact.GrandTotalBeforeT)
	}
	if impact.GrandTotalAfterT != 12.5 {
		t.Fatalf("调整后总量期望 12.5，实际 %v", impact.GrandTotalAfterT)
	}
}

func TestRuleDeleteProtection(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := testsupport.NewServices(db)
	ctx := context.Background()

	items, err := svc.Conversion.List(ctx)
	if err != nil {
		t.Fatalf("规则列表失败: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("期望至少 2 条基线规则，实际 %d", len(items))
	}

	// 新增一个未被任何记录引用的未来版本，应可删除。
	rule, _, err := svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          sludge.PairM3ToWet,
		Factor:        1.50,
		EffectiveFrom: date.Today().AddDays(30),
	})
	if err != nil {
		t.Fatalf("新增未来版本失败: %v", err)
	}
	if err := svc.Conversion.Delete(ctx, rule.ID); err != nil {
		t.Fatalf("未被引用的未来最新版本应可删除，实际: %v", err)
	}

	// 基线删除应被拒绝（历史版本不允许删）。
	var baselineID uint
	baselineItems, _ := svc.Conversion.List(ctx)
	for _, item := range baselineItems {
		if item.Pair == sludge.PairM3ToWet {
			baselineID = item.ID
		}
	}
	err = svc.Conversion.Delete(ctx, baselineID)
	testsupport.RequireAppError(t, err, httpx.CodeInvalidState)
}

func TestPreviewConversion(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := testsupport.NewServices(db)
	ctx := context.Background()

	resp, err := svc.Conversion.Preview(ctx, conversion.PreviewRequest{
		Amount: 10, Caliber: sludge.CaliberM3, CleanedAt: date.MustParse("2026-03-01"),
	})
	if err != nil {
		t.Fatalf("折算预览失败: %v", err)
	}
	// 10 * 1.40 * 0.40 = 5.6 t
	if resp.StandardT != 5.6 {
		t.Fatalf("期望折算 5.6 t，实际 %v", resp.StandardT)
	}
	if resp.DensityT_M3 != 1.40 || resp.WetToDry != 0.40 {
		t.Fatalf("期望返回生效系数 1.40 / 0.40，实际 %v / %v", resp.DensityT_M3, resp.WetToDry)
	}

	_, err = svc.Conversion.Preview(ctx, conversion.PreviewRequest{
		Amount: 10, Caliber: "ton", CleanedAt: date.MustParse("2026-03-01"),
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}
