package cleaningrecord_test

import (
	"context"
	"testing"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningrecord"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/sludge"
	"github.com/drainage/desilting/internal/testsupport"
)

func recordRequest(taskID uint) cleaningrecord.SaveRequest {
	return cleaningrecord.SaveRequest{
		TaskID:         taskID,
		CleanedAt:      date.Today().AddDays(-1),
		LengthM:        80,
		SludgeAmount:   12.5,
		SludgeCaliber:  sludge.CaliberM3,
		WaterVolumeM3:  40,
		PersonnelCount: 6,
		Method:         cleaningtask.MethodHighPressure,
		Weather:        cleaningrecord.WeatherSunny,
		RecorderName:   "李伟",
	}
}

func TestRecordRejectsFutureDate(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := fixture.CreateTask(t, fixture.Segment.ID, "日期校验任务")
	request := recordRequest(task.ID)
	request.CleanedAt = date.Today().AddDays(1)

	_, err := fixture.Records.Create(context.Background(), request)
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}

func TestRecordRejectsUnknownWeather(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := fixture.CreateTask(t, fixture.Segment.ID, "天气校验任务")
	request := recordRequest(task.ID)
	request.Weather = "typhoon"

	_, err := fixture.Records.Create(context.Background(), request)
	testsupport.RequireAppError(t, err, httpx.CodeValidation)
}

func TestRecordBlockedOnCompletedTask(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := fixture.TaskReadyForAcceptance(t, fixture.Segment.ID, "待验收任务")

	_, err := fixture.Records.Create(context.Background(), recordRequest(task.ID))
	appErr := testsupport.RequireAppError(t, err, httpx.CodeInvalidState)
	if appErr.Message == "" {
		t.Fatal("错误提示不应为空")
	}
}

func TestUpdateRecordBlockedWhenTaskCompleted(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := fixture.CreateTask(t, fixture.Segment.ID, "更新校验任务")
	record := fixture.CreateRecord(t, task.ID, 9)
	testsupport.RequireNoError(t, ignoreTask(fixture.Tasks.Complete(context.Background(), task.ID)))

	request := recordRequest(task.ID)
	request.SludgeAmount = 20
	_, err := fixture.Records.Update(context.Background(), record.ID, request)
	testsupport.RequireAppError(t, err, httpx.CodeInvalidState)
}

func TestUpdateRecordRejectsChangingTask(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	first := fixture.CreateTask(t, fixture.Segment.ID, "任务一")
	second := fixture.CreateTask(t, fixture.Segment.ID, "任务二")
	record := fixture.CreateRecord(t, first.ID, 9)

	request := recordRequest(second.ID)
	_, err := fixture.Records.Update(context.Background(), record.ID, request)
	testsupport.RequireAppError(t, err, httpx.CodeInvalidState)
}

func TestDeleteRecordBlockedWhenReferencedByAcceptance(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := fixture.CreateTask(t, fixture.Segment.ID, "被验收引用的任务")
	record := fixture.CreateRecord(t, task.ID, 15)
	testsupport.RequireNoError(t, ignoreTask(fixture.Tasks.Complete(context.Background(), task.ID)))

	request := testsupport.ReworkRequest(task.ID)
	request.CleaningRecordID = &record.ID
	_, err := fixture.Acceptances.Create(context.Background(), request)
	testsupport.RequireNoError(t, err)

	// 验收需整改会把任务退回清淤中，此时记录本身可编辑，但已被验收引用不能删除
	err = fixture.Records.Delete(context.Background(), record.ID)
	testsupport.RequireAppError(t, err, httpx.CodeInvalidState)
}

func TestRecordTotalsAggregateByTask(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	task := fixture.CreateTask(t, fixture.Segment.ID, "汇总任务")
	fixture.CreateRecord(t, task.ID, 10)
	fixture.CreateRecord(t, task.ID, 15)

	totals, err := fixture.Records.TotalsByTask(context.Background(), task.ID)
	testsupport.RequireNoError(t, err)
	if totals.RecordCount != 2 {
		t.Fatalf("期望记录条数 2，实际 %d", totals.RecordCount)
	}
	// 统一口径为干重 t：25 m³ × 1.40（密度）× 0.40（干湿系数）= 14.00 t
	if totals.StandardSludgeT != 14 {
		t.Fatalf("期望折算干重合计 14 t，实际 %v", totals.StandardSludgeT)
	}
	if len(totals.RawAmounts) != 1 || totals.RawAmounts[0].Caliber != sludge.CaliberM3 || totals.RawAmounts[0].Amount != 25 {
		t.Fatalf("期望原始口径合计 m³ 25，实际 %+v", totals.RawAmounts)
	}
	if totals.CleanedLengthM != 200 {
		t.Fatalf("期望清淤长度合计 200，实际 %v", totals.CleanedLengthM)
	}
}

func TestListRecordsFiltersByTask(t *testing.T) {
	fixture := testsupport.NewFixture(t)
	first := fixture.CreateTask(t, fixture.Segment.ID, "任务一")
	second := fixture.CreateTask(t, fixture.Segment.ID, "任务二")
	fixture.CreateRecord(t, first.ID, 10)
	fixture.CreateRecord(t, second.ID, 20)

	items, total, err := fixture.Records.List(context.Background(), cleaningrecord.ListQuery{
		TaskID: first.ID,
		Page:   httpx.PageQuery{Page: 1, PageSize: 10},
	})
	testsupport.RequireNoError(t, err)
	if total != 1 || len(items) != 1 {
		t.Fatalf("期望筛出 1 条清淤记录，实际 total=%d len=%d", total, len(items))
	}
	if items[0].Task == nil || items[0].Task.ID != first.ID {
		t.Fatalf("列表项应带出所属任务信息，实际 %+v", items[0].Task)
	}
}

func ignoreTask(_ *cleaningtask.CleaningTask, err error) error {
	return err
}
