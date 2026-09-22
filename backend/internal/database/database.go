// Package database 负责数据库连接、表结构迁移。
package database

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/drainage/desilting/internal/config"
	"github.com/drainage/desilting/internal/modules/acceptance"
	"github.com/drainage/desilting/internal/modules/cleaningrecord"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/modules/conversion"
	"github.com/drainage/desilting/internal/modules/pipesegment"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// Open 根据配置建立数据库连接。
//
// 生产与 docker compose 使用 PostgreSQL；本地开发或单元测试可以切到 SQLite，
// 两种驱动共用同一套 model 与查询代码。
func Open(cfg *config.Config) (*gorm.DB, error) {
	dialector, err := dialectorFor(cfg)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Warn),
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().In(time.Local) },
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

func dialectorFor(cfg *config.Config) (gorm.Dialector, error) {
	switch cfg.DBDriver {
	case config.DriverSQLite:
		if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("创建 SQLite 数据目录失败: %w", err)
			}
		}
		return sqlite.Open(cfg.DBPath), nil
	case config.DriverPostgres:
		return postgres.Open(cfg.PostgresDSN()), nil
	default:
		return nil, fmt.Errorf("不支持的数据库驱动 %q，可选值为 %s / %s",
			cfg.DBDriver, config.DriverPostgres, config.DriverSQLite)
	}
}

// baselineEffectiveFrom 系统基线换算规则的生效日期。
//
// 取一个足够早的日期，保证任何历史（含跨月补录）记录都能解析到当时生效的规则。
var baselineEffectiveFrom = date.MustParse("2000-01-01")

// Migrate 建立/更新所有业务表。
//
// 表之间的引用关系由应用层在 service 中校验，因此这里不建外键约束，
// 便于后续按模块拆库时平滑迁移。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&pipesegment.PipeSegment{},
		&cleaningtask.CleaningTask{},
		&cleaningrecord.CleaningRecord{},
		&acceptance.AcceptanceRecord{},
		&conversion.Rule{},
	); err != nil {
		return err
	}
	if err := backfillLegacySludge(db); err != nil {
		return err
	}
	if err := seedBaselineRules(db); err != nil {
		return err
	}
	return nil
}

// backfillLegacySludge 把口径统一改造前的历史数据迁移到新列：
//
//	sludge_volume_m3（旧） -> sludge_amount + sludge_caliber='m3'
//
// 历史原始数值原样搬迁，不做任何改写或折算；折算始终在查询时动态计算。
// 新列带默认值（加列约束所必需），迁移时口径列可能已是占位值 'm3'、amount 为 0，
// 因此条件识别「尚未真正回填」的行：旧列有值且（口径为空 或 新数值为 0）。
// 仅当旧列仍存在时执行，重复执行幂等（回填后 amount 非 0，不再命中）。
func backfillLegacySludge(db *gorm.DB) error {
	if !db.Migrator().HasTable(&cleaningrecord.CleaningRecord{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&cleaningrecord.CleaningRecord{}, "sludge_volume_m3") {
		return nil
	}
	return db.Exec(`UPDATE cleaning_records
		SET sludge_amount = sludge_volume_m3, sludge_caliber = ?
		WHERE sludge_volume_m3 IS NOT NULL AND sludge_volume_m3 <> 0
			AND (sludge_caliber IS NULL OR sludge_caliber = '' OR sludge_amount = 0)`,
		sludge.CaliberM3).Error
}

// seedBaselineRules 写入系统基线换算规则（幂等）。
//
// 默认：湿污泥密度 1.40 t/m³、湿重 -> 干重系数 0.40。
// 现场口径调整时通过规则管理页面新增更晚生效的版本，基线本身永不修改。
func seedBaselineRules(db *gorm.DB) error {
	baselines := []conversion.Rule{
		{
			Pair:          sludge.PairM3ToWet,
			Factor:        1.40,
			EffectiveFrom: baselineEffectiveFrom,
			Remark:        "系统基线：湿污泥密度 1.40 t/m³",
		},
		{
			Pair:          sludge.PairWetToDry,
			Factor:        0.40,
			EffectiveFrom: baselineEffectiveFrom,
			Remark:        "系统基线：湿重折算干重系数 0.40",
		},
	}
	for _, rule := range baselines {
		var count int64
		if err := db.Model(&conversion.Rule{}).
			Where("pair = ? AND effective_from = ?", rule.Pair, rule.EffectiveFrom.Time).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&rule).Error; err != nil {
			// 并发启动等场景下唯一索引兜底，已存在即视为成功。
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				continue
			}
			return err
		}
	}
	return nil
}
