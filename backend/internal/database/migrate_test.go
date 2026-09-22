package database_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/drainage/desilting/internal/database"
	"github.com/drainage/desilting/internal/modules/acceptance"
	"github.com/drainage/desilting/internal/modules/cleaningrecord"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/modules/pipesegment"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/refx"
	"github.com/drainage/desilting/internal/shared/sludge"
)

func dateParse(value string) date.Date { return date.MustParse(value) }

// TestMigrateBackfillsLegacySludgeVolume 验证口径统一改造前的历史库：
// 旧列 sludge_volume_m3 的原始数值必须原样搬迁（不改写、不折算）为 m³ 口径，
// 迁移后四层共用的折算统计可正常工作。
func TestMigrateBackfillsLegacySludgeVolume(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// 先建出不含本次改造的完整“旧版本”表结构（含全部历史 NOT NULL 列），
	// 再删除新列，精确模拟升级前只有 sludge_volume_m3 的形态。
	if err := db.AutoMigrate(
		&pipesegment.PipeSegment{},
		&cleaningtask.CleaningTask{},
		&acceptance.AcceptanceRecord{},
	); err != nil {
		t.Fatalf("建立旧版本表结构失败: %v", err)
	}
	if err := db.AutoMigrate(&cleaningrecord.CleaningRecord{}); err != nil {
		t.Fatalf("建立旧版本清淤记录表失败: %v", err)
	}
	// 回到改造前形态：把新列重命名为旧列 sludge_volume_m3，并删除口径列与规则表。
	if err := db.Exec(`ALTER TABLE cleaning_records RENAME COLUMN sludge_amount TO sludge_volume_m3`).Error; err != nil {
		t.Fatalf("重命名为旧列失败: %v", err)
	}
	if err := db.Migrator().DropColumn(&cleaningrecord.CleaningRecord{}, "sludge_caliber"); err != nil {
		t.Fatalf("移除口径列失败: %v", err)
	}
	if err := db.Migrator().DropTable(&conversion.Rule{}); err != nil {
		t.Fatalf("移除规则表失败: %v", err)
	}

	seg := pipesegment.PipeSegment{
		Code: "PS-OLD-1", Name: "旧管段", District: "老城片区", PipeType: pipesegment.TypeRainwater,
		Material: "concrete", DiameterMm: 600, LengthM: 100, DepthM: 2, Status: pipesegment.StatusNormal,
	}
	if err := db.Create(&seg).Error; err != nil {
		t.Fatalf("插入旧管段失败: %v", err)
	}
	task := cleaningtask.CleaningTask{
		Code: "QX-OLD-1", Title: "旧任务", PipeSegmentID: seg.ID,
		Priority: cleaningtask.PriorityNormal, Source: cleaningtask.SourcePlan,
		PlanStartDate: dateParse("2026-02-01"), PlanEndDate: dateParse("2026-02-05"),
		Status: cleaningtask.StatusAccepted,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("插入旧任务失败: %v", err)
	}
	// 直接按旧列写入一条历史记录。
	if err := db.Exec(`INSERT INTO cleaning_records
		(code, task_id, cleaned_at, length_m, sludge_volume_m3, personnel_count, recorder_name)
		VALUES ('QJ-OLD-1', ?, '2026-02-03', 100, 12.5, 3, '旧记录人')`, task.ID).Error; err != nil {
		t.Fatalf("插入旧记录失败: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 原始值必须原样搬迁：12.5、口径 m3。
	var m struct {
		Amount  float64
		Caliber string
	}
	if err := db.Table(refx.TableCleaningRecords).
		Select("sludge_amount AS amount, sludge_caliber AS caliber").
		Where("code = ?", "QJ-OLD-1").Scan(&m).Error; err != nil {
		t.Fatalf("读取迁移后记录失败: %v", err)
	}
	if m.Amount != 12.5 || m.Caliber != sludge.CaliberM3 {
		t.Fatalf("历史原始值应原样搬迁为 12.5 m³，实际 amount=%v caliber=%q", m.Amount, m.Caliber)
	}

	// 折算统计正常：12.5 × 1.40 × 0.40 = 7.00 t。
	totals, err := refx.TotalsByTaskID(context.Background(), db, task.ID)
	if err != nil {
		t.Fatalf("迁移后任务汇总失败: %v", err)
	}
	if totals.StandardSludgeT != 7.0 {
		t.Fatalf("迁移后折算干重期望 7.00 t，实际 %v", totals.StandardSludgeT)
	}

	// 基线规则已写入。
	var ruleCount int64
	if err := db.Model(&conversion.Rule{}).Count(&ruleCount).Error; err != nil {
		t.Fatalf("统计规则失败: %v", err)
	}
	if ruleCount < 2 {
		t.Fatalf("期望至少写入 2 条基线规则，实际 %d", ruleCount)
	}

	// 迁移幂等：再跑一次不报错、原始值不被二次改写。
	if err := database.Migrate(db); err != nil {
		t.Fatalf("二次迁移失败: %v", err)
	}
	var m2 struct {
		Amount  float64
		Caliber string
	}
	if err := db.Table(refx.TableCleaningRecords).
		Select("sludge_amount AS amount, sludge_caliber AS caliber").
		Where("code = ?", "QJ-OLD-1").Scan(&m2).Error; err != nil {
		t.Fatalf("二次迁移后读取记录失败: %v", err)
	}
	if m2.Amount != 12.5 || m2.Caliber != sludge.CaliberM3 {
		t.Fatalf("二次迁移后原始值应保持 12.5 m³，实际 %+v", m2)
	}
}
