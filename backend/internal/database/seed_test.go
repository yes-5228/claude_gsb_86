package database_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/drainage/desilting/internal/database"
	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/modules/dashboard"
	"github.com/drainage/desilting/internal/shared/num"
)

// TestSeedProducesConsistentConvertedTotals 验证全新库迁移 + 种子后：
// 记录已按当时规则折算、干重记录系数为 1、看板总量为全部记录折算值之和。
func TestSeedProducesConsistentConvertedTotals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := database.Seed(db, logger); err != nil {
		t.Fatalf("种子失败: %v", err)
	}

	// 全部记录折算值之和（叶子列）。
	var leafSum float64
	if err := db.Table("cleaning_records").
		Select("COALESCE(SUM(converted_dry_t), 0)").Scan(&leafSum).Error; err != nil {
		t.Fatalf("汇总折算值失败: %v", err)
	}

	conv := conversion.NewService(conversion.NewRepository(db))
	overview, err := dashboard.NewService(db, conv).Overview(context.Background())
	if err != nil {
		t.Fatalf("看板统计失败: %v", err)
	}
	if num.Round6(overview.SludgeTotalDryT) != num.Round6(leafSum) {
		t.Fatalf("看板总量 %v 与记录折算值之和 %v 不一致", overview.SludgeTotalDryT, leafSum)
	}

	districts, err := dashboard.NewService(db, conv).DistrictStats(context.Background())
	if err != nil {
		t.Fatalf("片区统计失败: %v", err)
	}
	var districtSum float64
	for _, row := range districts {
		districtSum += row.SludgeDryT
	}
	if num.Round6(districtSum) != num.Round6(leafSum) {
		t.Fatalf("片区合计 %v 与记录折算值之和 %v 不一致", districtSum, leafSum)
	}

	// 至少存在一条干重记录（系数 1、未命中规则）与一条旧规则期湿重记录。
	var dryCount int64
	if err := db.Table("cleaning_records").
		Where("weight_basis = ? AND conversion_factor = 1 AND conversion_rule_id = 0", "dry").
		Count(&dryCount).Error; err != nil {
		t.Fatalf("统计干重记录失败: %v", err)
	}
	if dryCount == 0 {
		t.Fatal("种子应至少包含一条干重口径记录")
	}

	var oldFactorCount int64
	if err := db.Table("cleaning_records").
		Where("weight_basis = ? AND conversion_factor = ?", "wet", 0.55).
		Count(&oldFactorCount).Error; err != nil {
		t.Fatalf("统计旧规则记录失败: %v", err)
	}
	if oldFactorCount == 0 {
		t.Fatal("种子应至少包含一条命中旧规则(55%)的湿重记录")
	}
}
