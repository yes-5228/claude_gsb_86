package dashboard_test

import (
	"context"
	"testing"

	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/modules/dashboard"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/num"
	"github.com/drainage/desilting/internal/shared/refx"
	"github.com/drainage/desilting/internal/shared/sludge"
	"github.com/drainage/desilting/internal/testsupport"
)

// TestSludgeTotalsConsistentAcrossLevels 验证核心需求：
// 任务、管段、片区、看板四个层级的折算干重合计必须完全一致；
// Go 换算引擎与 SQL 折算表达式对每条记录的结果也必须一致。
func TestSludgeTotalsConsistentAcrossLevels(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := testsupport.NewServices(db)
	ctx := context.Background()

	segEast := svc.CreateSegment(t, "PS-E-001", "城东片区")
	segWest := svc.CreateSegment(t, "PS-W-001", "城西片区")

	taskE1 := svc.CreateTask(t, segEast.ID, "城东任务一")
	taskE2 := svc.CreateTask(t, segEast.ID, "城东任务二")
	taskW1 := svc.CreateTask(t, segWest.ID, "城西任务一")

	// 三种口径混合：m3 / wet_t / dry_t；日期跨多月（含较早的补录数据）。
	d1 := date.MustParse("2026-03-10")
	d2 := date.MustParse("2026-04-11")
	d3 := date.MustParse("2026-05-12")
	createRecordRaw(t, &testsupport.Fixture{Services: svc, DB: db, Segment: segEast}, taskE1.ID, d1, 12.5, sludge.CaliberM3)
	createRecordRaw(t, &testsupport.Fixture{Services: svc, DB: db, Segment: segEast}, taskE1.ID, d2, 10.0, sludge.CaliberWetT)
	createRecordRaw(t, &testsupport.Fixture{Services: svc, DB: db, Segment: segEast}, taskE2.ID, d3, 8.0, sludge.CaliberDryT)
	createRecordRaw(t, &testsupport.Fixture{Services: svc, DB: db, Segment: segWest}, taskW1.ID, d2, 20.0, sludge.CaliberM3)

	// ---- 任务层 ----
	totE1, err := refx.TotalsByTaskID(ctx, db, taskE1.ID)
	if err != nil {
		t.Fatalf("任务 E1 汇总失败: %v", err)
	}
	totE2, err := refx.TotalsByTaskID(ctx, db, taskE2.ID)
	if err != nil {
		t.Fatalf("任务 E2 汇总失败: %v", err)
	}
	totW1, err := refx.TotalsByTaskID(ctx, db, taskW1.ID)
	if err != nil {
		t.Fatalf("任务 W1 汇总失败: %v", err)
	}

	engine, err := svc.Conversion.Engine(ctx)
	if err != nil {
		t.Fatalf("加载换算引擎失败: %v", err)
	}
	// 期望值全部用 Go 引擎逐条折算后求和（独立于 SQL 实现）。
	wantE1 := sludge.Round2(
		engine.StandardT(12.5, sludge.CaliberM3, d1) +
			engine.StandardT(10.0, sludge.CaliberWetT, d2))
	wantE2 := engine.StandardT(8.0, sludge.CaliberDryT, d3)
	wantW1 := engine.StandardT(20.0, sludge.CaliberM3, d2)
	if totE1.StandardSludgeT != wantE1 {
		t.Fatalf("任务 E1 折算合计期望 %v，实际 %v", wantE1, totE1.StandardSludgeT)
	}
	if totE2.StandardSludgeT != wantE2 {
		t.Fatalf("任务 E2 折算合计期望 %v，实际 %v", wantE2, totE2.StandardSludgeT)
	}
	if totW1.StandardSludgeT != wantW1 {
		t.Fatalf("任务 W1 折算合计期望 %v，实际 %v", wantW1, totW1.StandardSludgeT)
	}

	// 每条记录的 SQL 折算结果必须与 Go 引擎一致（逐条，不允许只对合计）。
	var rows []struct {
		ID        uint
		Amount    float64
		Caliber   string
		CleanedAt date.Date
		StandardT float64
	}
	expr, args := refx.StandardPerRecordExpr("r")
	if err := db.Table(refx.TableCleaningRecords+" AS r").
		Select("r.id, r."+refx.RawAmountColumn()+" AS amount, r."+refx.RawCaliberColumn()+" AS caliber, r.cleaned_at, "+expr+" AS standard_t", args...).
		Scan(&rows).Error; err != nil {
		t.Fatalf("逐条折算查询失败: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("期望 4 条记录，实际 %d", len(rows))
	}
	for _, row := range rows {
		want := engine.StandardT(row.Amount, row.Caliber, row.CleanedAt)
		if row.StandardT != want {
			t.Fatalf("记录 %d SQL 折算 %v 与引擎 %v 不一致（%v %s %s）",
				row.ID, row.StandardT, want, row.Amount, row.Caliber, row.CleanedAt)
		}
	}

	grand := num.Round2(wantE1 + wantE2 + wantW1)

	// ---- 管段层 ----
	segEastTotals, err := refx.SludgeTotalsForSegment(ctx, db, segEast.ID)
	if err != nil {
		t.Fatalf("管段城东汇总失败: %v", err)
	}
	segWestTotals, err := refx.SludgeTotalsForSegment(ctx, db, segWest.ID)
	if err != nil {
		t.Fatalf("管段城西汇总失败: %v", err)
	}
	if segEastTotals.StandardSludgeT != num.Round2(wantE1+wantE2) {
		t.Fatalf("管段城东合计期望 %v，实际 %v", num.Round2(wantE1+wantE2), segEastTotals.StandardSludgeT)
	}
	if segWestTotals.StandardSludgeT != wantW1 {
		t.Fatalf("管段城西合计期望 %v，实际 %v", wantW1, segWestTotals.StandardSludgeT)
	}

	// ---- 片区层 ----
	dash := dashboard.NewService(db)
	districts, err := dash.DistrictStats(ctx)
	if err != nil {
		t.Fatalf("片区统计失败: %v", err)
	}
	var districtGrand float64
	for _, stat := range districts {
		switch stat.District {
		case "城东片区":
			if stat.StandardSludgeT != num.Round2(wantE1+wantE2) {
				t.Fatalf("城东片区合计期望 %v，实际 %v", num.Round2(wantE1+wantE2), stat.StandardSludgeT)
			}
		case "城西片区":
			if stat.StandardSludgeT != wantW1 {
				t.Fatalf("城西片区合计期望 %v，实际 %v", wantW1, stat.StandardSludgeT)
			}
		}
		districtGrand += stat.StandardSludgeT
	}
	if num.Round2(districtGrand) != grand {
		t.Fatalf("片区合计 %v 与任务层合计 %v 不一致", num.Round2(districtGrand), grand)
	}

	// ---- 看板层 ----
	overview, err := dash.Overview(ctx)
	if err != nil {
		t.Fatalf("看板总览失败: %v", err)
	}
	if overview.SludgeTotalT != grand {
		t.Fatalf("看板累计清淤量 %v 与下层合计 %v 不一致", overview.SludgeTotalT, grand)
	}
	if overview.RecordTotal != 4 {
		t.Fatalf("看板记录数期望 4，实际 %d", overview.RecordTotal)
	}
}

// TestCrossMonthBackfillUsesRuleAtCleanedDate 跨月补录：
// 录入日期晚于新规则生效，但清淤日期更早时，必须按清淤当日生效的旧规则折算。
func TestCrossMonthBackfillUsesRuleAtCleanedDate(t *testing.T) {
	db := testsupport.NewDB(t)
	svc := testsupport.NewServices(db)
	ctx := context.Background()

	seg := svc.CreateSegment(t, "PS-B-001", "城中片区")
	task := svc.CreateTask(t, seg.ID, "跨月补录任务")

	// 历史作业日：2026-02-01；假设后来在 2026-06-01 干湿系数从 0.40 调整为 0.50。
	cleanedAt := date.MustParse("2026-02-01")
	newRuleDay := date.MustParse("2026-06-01")
	if _, _, err := svc.Conversion.Create(ctx, conversion.CreateRuleRequest{
		Pair:          sludge.PairWetToDry,
		Factor:        0.50,
		EffectiveFrom: newRuleDay,
		Remark:        "年中调整干湿系数",
	}); err != nil {
		t.Fatalf("新增规则版本失败: %v", err)
	}

	// 补录动作发生在今天（晚于 6 月），但清淤日期是 2 月，应仍按旧系数 0.40 折算。
	createRecordRaw(t, &testsupport.Fixture{Services: svc, DB: db, Segment: seg}, task.ID, cleanedAt, 10, sludge.CaliberWetT)

	totals, err := refx.TotalsByTaskID(ctx, db, task.ID)
	if err != nil {
		t.Fatalf("任务汇总失败: %v", err)
	}
	if totals.StandardSludgeT != 4.0 {
		t.Fatalf("跨月补录应按清淤当时 0.40 系数折算为 4.0 t，实际 %v", totals.StandardSludgeT)
	}

	// 同一口径、作业日在新生效日之后的记录应按 0.50 折算。
	afterDay := date.MustParse("2026-06-02")
	createRecordRaw(t, &testsupport.Fixture{Services: svc, DB: db, Segment: seg}, task.ID, afterDay, 10, sludge.CaliberWetT)
	totals2, err := refx.TotalsByTaskID(ctx, db, task.ID)
	if err != nil {
		t.Fatalf("任务汇总失败: %v", err)
	}
	if totals2.StandardSludgeT != 9.0 {
		t.Fatalf("新规则生效后记录应按 0.50 折算，任务合计期望 9.0 t，实际 %v", totals2.StandardSludgeT)
	}
}
