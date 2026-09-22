// Package conversion 维护清淤量湿重 / 干重 / 体积之间的换算规则。
//
// 换算规则按「方向 + 生效日期」形成 append-only 的版本链：
//
//   - 新版本只能新增，不能修改或删除已被记录引用的历史版本；
//   - 折算时按清淤日期取当时生效的版本，因此跨月补录的数据会按
//     清淤当日（而非录入当日）生效的规则折算；
//   - 调整规则后可通过影响预览说明对既有统计的影响范围。
package conversion

import (
	"time"

	"github.com/drainage/desilting/internal/shared/date"
)

// Rule 清淤量换算规则（一个方向一条版本链）。
type Rule struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Pair          string    `gorm:"size:24;not null;index:idx_sludge_rule_version,unique" json:"pair"`
	Factor        float64   `gorm:"not null" json:"factor"`
	EffectiveFrom date.Date `gorm:"type:date;not null;index:idx_sludge_rule_version,unique" json:"effectiveFrom"`
	Remark        string    `gorm:"size:255" json:"remark"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TableName 指定表名。
func (Rule) TableName() string {
	return "sludge_conversion_rules"
}
