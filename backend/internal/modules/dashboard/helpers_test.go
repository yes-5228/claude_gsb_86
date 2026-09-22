package dashboard_test

import (
	"context"
	"testing"

	"github.com/drainage/desilting/internal/modules/cleaningrecord"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/testsupport"
)

// createRecordRaw 按指定口径 / 日期录入清淤记录，用于构造混合口径、跨月补录场景。
func createRecordRaw(t *testing.T, f *testsupport.Fixture, taskID uint, day date.Date, amount float64, caliber string) {
	t.Helper()
	_, err := f.Records.Create(context.Background(), cleaningrecord.SaveRequest{
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
	})
	if err != nil {
		t.Fatalf("录入清淤记录失败: %v", err)
	}
}

var _ = cleaningtask.StatusAccepted
