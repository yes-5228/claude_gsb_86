package conversion_test

import (
	"context"
	"testing"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningrecord"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/num"
	"github.com/drainage/desilting/internal/testsupport"
)

// cleaningRecordRequest 构造一条可指定原始重量、口径与清淤日期的记录请求。
func cleaningRecordRequest(taskID uint, rawWeight float64, basis string, cleanedAt date.Date) cleaningrecord.SaveRequest {
	return cleaningrecord.SaveRequest{
		TaskID:         taskID,
		CleanedAt:      cleanedAt,
		LengthM:        100,
		SludgeVolumeM3: rawWeight * 0.7,
		RawWeightT:     rawWeight,
		WeightBasis:    basis,
		WaterVolumeM3:  30,
		PersonnelCount: 5,
		Method:         cleaningtask.MethodHighPressure,
		Weather:        cleaningrecord.WeatherSunny,
		RecorderName:   "测试记录人",
	}
}

func TestConvertWetAndDry(t *testing.T) {
	wet := conversion.Convert(10, conversion.BasisWet, 0.65)
	if wet.Factor != 0.65 || wet.DryT != 6.5 {
		t.Fatalf("湿重折算错误: %+v", wet)
	}
	dry := conversion.Convert(10, conversion.BasisDry, 0.65)
	if dry.Factor != 1 || dry.DryT != 10 {
		t.Fatalf("干重应保持原值、系数为 1: %+v", dry)
	}
}

func TestRecordUsesRuleEffectiveAtCleanedDate(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()
	task := fixture.CreateTask(t, fixture.Segment.ID, "跨月补录任务")

	// 在更早日期新增一条 50% 规则（默认规则自 2000 年起为 60%）。
	oldDate := date.Today().AddDays(-60)
	_, _, err := fixture.Conversions.CreateRule(ctx, conversion.SaveRequest{
		Name: "旧规则", EffectiveFrom: oldDate, WetToDryFactor: 0.5,
	})
	testsupport.RequireNoError(t, err)

	// 跨月补录一条清淤日期落在旧规则期的湿重记录。
	record, err := fixture.Records.Create(ctx, cleaningRecordRequest(task.ID, 10, conversion.BasisWet, oldDate.AddDays(2)))
	testsupport.RequireNoError(t, err)

	// 应命中旧规则（50%），而不是默认 60% 或当前规则。
	if record.ConversionFactor != 0.5 || record.ConvertedDryT != 5 {
		t.Fatalf("跨月补录应按清淤日期当时规则折算，实际 factor=%v dry=%v",
			record.ConversionFactor, record.ConvertedDryT)
	}

	// 同期补录一条干重记录，系数恒为 1、不命中任何规则。
	dryRecord, err := fixture.Records.Create(ctx, cleaningRecordRequest(task.ID, 8, conversion.BasisDry, oldDate.AddDays(3)))
	testsupport.RequireNoError(t, err)
	if dryRecord.ConversionFactor != 1 || dryRecord.ConvertedDryT != 8 || dryRecord.ConversionRuleID != 0 {
		t.Fatalf("干重记录折算应保持原值且不命中规则: %+v", dryRecord)
	}
}

func TestCreateRuleRestatesOnlyWindowAndKeepsRaw(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()

	segA := fixture.CreateSegment(t, "PS-TEST-A", "城东片区")
	segB := fixture.CreateSegment(t, "PS-TEST-B", "城西片区")
	taskA := fixture.CreateTask(t, segA.ID, "城东任务")
	taskB := fixture.CreateTask(t, segB.ID, "城西任务")

	// 默认规则 60%。两条湿重记录分别落在新旧区间。
	newDate := date.Today().AddDays(-5)
	oldRec, err := fixture.Records.Create(ctx, cleaningRecordRequest(taskA.ID, 20, conversion.BasisWet, newDate.AddDays(-20)))
	testsupport.RequireNoError(t, err) // 12.0（60%），新规则生效前
	rec, err := fixture.Records.Create(ctx, cleaningRecordRequest(taskB.ID, 20, conversion.BasisWet, newDate.AddDays(1)))
	testsupport.RequireNoError(t, err) // 12.0（60%），新规则生效后→将重算为 16

	// 预览不落库。
	preview, err := fixture.Conversions.PreviewImpact(ctx, conversion.SaveRequest{
		Name: "新规则", EffectiveFrom: newDate, WetToDryFactor: 0.8,
	})
	testsupport.RequireNoError(t, err)
	if preview.AffectedRecords != 1 || preview.AffectedSegments != 1 {
		t.Fatalf("预览影响范围应为 1 条记录/1 个管段，实际 %+v", preview)
	}
	if num.Round6(preview.DryBefore) != 12 || num.Round6(preview.DryAfter) != 16 || num.Round6(preview.Delta) != 4 {
		t.Fatalf("预览折算前后合计错误: %+v", preview)
	}

	// 落库创建规则并重算。
	_, impact, err := fixture.Conversions.CreateRule(ctx, conversion.SaveRequest{
		Name: "新规则", EffectiveFrom: newDate, WetToDryFactor: 0.8,
	})
	testsupport.RequireNoError(t, err)
	if impact.AffectedRecords != 1 || impact.AffectedDistricts != 1 {
		t.Fatalf("落库影响范围错误: %+v", impact)
	}

	reloaded := mustRecord(t, fixture, rec.ID)
	if reloaded.ConversionFactor != 0.8 || reloaded.ConvertedDryT != 16 {
		t.Fatalf("新区间记录应按新系数重算: %+v", reloaded)
	}
	// 原始计量值不得被改写。
	if reloaded.RawWeightT != 20 {
		t.Fatalf("原始重量被改写: %v", reloaded.RawWeightT)
	}

	oldReloaded := mustRecord(t, fixture, oldRec.ID)
	if oldReloaded.ConversionFactor != 0.6 || oldReloaded.ConvertedDryT != 12 {
		t.Fatalf("旧区间记录不应被新规则影响: %+v", oldReloaded)
	}

	// 变更日志已记录。
	logs, err := fixture.Conversions.ListLogs(ctx, 10)
	testsupport.RequireNoError(t, err)
	if len(logs) != 1 || logs[0].AffectedRecords != 1 {
		t.Fatalf("应写入 1 条变更日志，实际 %+v", logs)
	}
}

func TestCreateRuleRejectsDuplicateEffectiveDate(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	day := date.Today().AddDays(-3)
	_, _, err := fixture.Conversions.CreateRule(context.Background(), conversion.SaveRequest{
		Name: "规则一", EffectiveFrom: day, WetToDryFactor: 0.7,
	})
	testsupport.RequireNoError(t, err)
	_, _, err = fixture.Conversions.CreateRule(context.Background(), conversion.SaveRequest{
		Name: "规则二", EffectiveFrom: day, WetToDryFactor: 0.9,
	})
	testsupport.RequireAppError(t, err, httpx.CodeConflict)
}

func TestInvalidFactorRejected(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	_, err := fixture.Conversions.PreviewImpact(context.Background(), conversion.SaveRequest{
		Name: "异常规则", EffectiveFrom: date.Today(), WetToDryFactor: 1.5,
	})
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}

func TestRuleWindowUpperBound(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	ctx := context.Background()
	d1 := date.Today().AddDays(-30)
	d2 := date.Today().AddDays(-10)
	_, _, err := fixture.Conversions.CreateRule(ctx, conversion.SaveRequest{Name: "r1", EffectiveFrom: d1, WetToDryFactor: 0.5})
	testsupport.RequireNoError(t, err)
	_, _, err = fixture.Conversions.CreateRule(ctx, conversion.SaveRequest{Name: "r2", EffectiveFrom: d2, WetToDryFactor: 0.7})
	testsupport.RequireNoError(t, err)

	views, err := fixture.Conversions.ListRules(ctx)
	testsupport.RequireNoError(t, err)

	byCode := make(map[string]conversion.RuleView, len(views))
	for _, v := range views {
		byCode[v.Code] = v
	}
	r1Code := "HG-" + d1.Format("20060102")
	r2Code := "HG-" + d2.Format("20060102")
	r1, ok1 := byCode[r1Code]
	r2, ok2 := byCode[r2Code]
	if !ok1 || !ok2 {
		t.Fatalf("未找到新增规则: r1=%v r2=%v", ok1, ok2)
	}
	// 倒序：r2 比 r1 新。r2 若是当前最新规则则为开放区间；r1 上界为 d2 前一天。
	if r2.EffectiveFrom.String() != d2.String() {
		t.Fatalf("规则顺序错误: %+v", r2)
	}
	want := d2.AddDays(-1)
	if r1.EffectiveTo == nil || r1.EffectiveTo.String() != want.String() {
		t.Fatalf("旧规则上界应为 %s，实际 %+v", want, r1.EffectiveTo)
	}
}

func mustRecord(t *testing.T, fixture *testsupport.Fixture, id uint) *recordView {
	t.Helper()
	detail, err := fixture.Records.Detail(context.Background(), id)
	testsupport.RequireNoError(t, err)
	return &recordView{
		ID:               detail.Record.ID,
		RawWeightT:       detail.Record.RawWeightT,
		ConversionFactor: detail.Record.ConversionFactor,
		ConvertedDryT:    detail.Record.ConvertedDryT,
	}
}

type recordView struct {
	ID               uint
	RawWeightT       float64
	ConversionFactor float64
	ConvertedDryT    float64
}
