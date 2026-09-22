package dashboard_test

import (
	"context"
	"testing"

	"github.com/drainage/desilting/internal/modules/cleaningrecord"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/modules/dashboard"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/num"
	"github.com/drainage/desilting/internal/shared/refx"
	"github.com/drainage/desilting/internal/testsupport"
)

// TestFourLevelTotalsConsistent 验证任务、管段、片区、看板四级的统一口径干重合计
// 完全一致：它们都来自同一条叶子列 converted_dry_t，任何一级都不先取整。
func TestFourLevelTotalsConsistent(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	// 新增一条更晚的规则，使数据横跨两个折算系数区间（默认 60% 与新 70%）。
	newRuleFrom := date.Today().AddDays(-8)
	_, _, err := fixture.Conversions.CreateRule(ctx, conversion.SaveRequest{
		Name: "新规则", EffectiveFrom: newRuleFrom, WetToDryFactor: 0.7,
	})
	testsupport.RequireNoError(t, err)

	districts := []struct{ code, district string }{
		{"PS-C-A", "城东片区"},
		{"PS-C-B", "城东片区"},
		{"PS-C-C", "城西片区"},
	}
	var segmentIDs []uint
	var taskIDs []uint
	type recSpec struct {
		taskIdx   int
		raw       float64
		basis     string
		dayOffset int
	}
	specs := []recSpec{
		{0, 10, conversion.BasisWet, -20}, // 旧 60%
		{0, 5, conversion.BasisDry, -20},  // 干重
		{1, 20, conversion.BasisWet, -2},  // 新 70%
		{2, 30, conversion.BasisWet, -1},  // 新 70%
		{2, 8, conversion.BasisDry, -15},  // 干重
	}

	for _, d := range districts {
		seg := fixture.CreateSegment(t, d.code, d.district)
		segmentIDs = append(segmentIDs, seg.ID)
		task := fixture.CreateTask(t, seg.ID, "任务-"+d.code)
		taskIDs = append(taskIDs, task.ID)
	}
	for _, sp := range specs {
		req := cleaningrecord.SaveRequest{
			TaskID:         taskIDs[sp.taskIdx],
			CleanedAt:      date.Today().AddDays(sp.dayOffset),
			LengthM:        50,
			SludgeVolumeM3: sp.raw * 0.7,
			RawWeightT:     sp.raw,
			WeightBasis:    sp.basis,
			WaterVolumeM3:  10,
			PersonnelCount: 4,
			Method:         cleaningtask.MethodHighPressure,
			Weather:        cleaningrecord.WeatherSunny,
			RecorderName:   "测试人",
		}
		_, err := fixture.Records.Create(ctx, req)
		testsupport.RequireNoError(t, err)
	}

	// 第一级：任务合计之和。
	taskTotals, err := refx.TotalsByTaskIDs(ctx, fixture.DB, taskIDs)
	testsupport.RequireNoError(t, err)
	var taskSum float64
	for _, v := range taskTotals {
		taskSum += v.ConvertedDryT
	}
	taskSum = num.Round6(taskSum)

	// 第二级：管段合计之和。
	var segmentSum float64
	for _, sid := range segmentIDs {
		totals, err := refx.SludgeTotalsForSegment(ctx, fixture.DB, sid)
		testsupport.RequireNoError(t, err)
		segmentSum += totals.ConvertedDryT
	}
	segmentSum = num.Round6(segmentSum)

	svc := dashboard.NewService(fixture.DB, fixture.Conversions)

	// 第三级：片区合计之和。
	districtStats, err := svc.DistrictStats(ctx)
	testsupport.RequireNoError(t, err)
	var districtSum float64
	for _, row := range districtStats {
		districtSum += row.SludgeDryT
	}
	districtSum = num.Round6(districtSum)

	// 第四级：看板总量。
	overview, err := svc.Overview(ctx)
	testsupport.RequireNoError(t, err)
	boardTotal := num.Round6(overview.SludgeTotalDryT)

	// 期望值：旧规则湿重 10*0.6=6；干重 5；新规则湿重 20*0.7=14、30*0.7=21；干重 8 => 54。
	const want = 54.0
	if taskSum != want {
		t.Fatalf("任务级合计应为 %v，实际 %v", want, taskSum)
	}
	if segmentSum != want {
		t.Fatalf("管段级合计应为 %v，实际 %v", want, segmentSum)
	}
	if districtSum != want {
		t.Fatalf("片区级合计应为 %v，实际 %v", want, districtSum)
	}
	if boardTotal != want {
		t.Fatalf("看板级合计应为 %v，实际 %v", want, boardTotal)
	}
	if taskSum != segmentSum || segmentSum != districtSum || districtSum != boardTotal {
		t.Fatalf("四级合计不一致: task=%v segment=%v district=%v board=%v",
			taskSum, segmentSum, districtSum, boardTotal)
	}

	// 口径说明已下发且当前规则为最新一条。
	if overview.Caliber == nil || overview.Caliber.CurrentRule == nil {
		t.Fatalf("看板应返回统一口径与当前规则: %+v", overview.Caliber)
	}
	if overview.Caliber.CurrentRule.WetToDryFactor != 0.7 {
		t.Fatalf("当前生效规则系数应为 0.7，实际 %v", overview.Caliber.CurrentRule.WetToDryFactor)
	}
}
