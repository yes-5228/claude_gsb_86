package conversion

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/shared/date"
)

// RuleItem 规则列表 / 详情项。
type RuleItem struct {
	Rule
	FromCaliber    string `json:"fromCaliber"`
	ToCaliber      string `json:"toCaliber"`
	Latest         bool   `json:"latest"`
	Referenced     bool   `json:"referenced"`
	Deletable      bool   `json:"deletable"`
	AffectedRecord int64  `json:"affectedRecord"`
}

// CreateRuleRequest 新增换算规则版本的请求体。
type CreateRuleRequest struct {
	Pair          string    `json:"pair" label:"换算方向" validate:"required"`
	Factor        float64   `json:"factor" label:"换算系数" validate:"gt=0,lte=100"`
	EffectiveFrom date.Date `json:"effectiveFrom" label:"生效日期"`
	Remark        string    `json:"remark" label:"备注" validate:"max=255"`
}

// ImpactRequest 规则影响预览：不写库，按「假设新版本生效」试算影响范围。
type ImpactRequest struct {
	Pair          string    `json:"pair"`
	Factor        float64   `json:"factor"`
	EffectiveFrom date.Date `json:"effectiveFrom"`
	RuleID        *uint     `json:"ruleId,omitempty"`
}

// ParseImpactRequest 解析影响预览请求体。
func ParseImpactRequest(c *fiber.Ctx) (ImpactRequest, error) {
	var req ImpactRequest
	if err := c.BodyParser(&req); err != nil {
		return ImpactRequest{}, httpx.BadRequest("请求体格式不正确，应为 JSON")
	}
	return req, nil
}

// AffectedRecordSample 受影响记录样例。
type AffectedRecordSample struct {
	RecordID  uint    `json:"recordId"`
	Code      string  `json:"code"`
	TaskID    uint    `json:"taskId"`
	CleanedAt string  `json:"cleanedAt"`
	District  string  `json:"district"`
	Caliber   string  `json:"caliber"`
	RawAmount float64 `json:"rawAmount"`
	BeforeT   float64 `json:"beforeT"`
	AfterT    float64 `json:"afterT"`
	DeltaT    float64 `json:"deltaT"`
}

// ImpactSummary 规则调整对既有统计的影响范围。
type ImpactSummary struct {
	Pair              string                 `json:"pair"`
	Factor            float64                `json:"factor"`
	EffectiveFrom     string                 `json:"effectiveFrom"`
	AffectedRecord    int64                  `json:"affectedRecord"`
	AffectedTask      int64                  `json:"affectedTask"`
	AffectedDistricts []string               `json:"affectedDistricts"`
	BeforeStandardT   float64                `json:"beforeStandardT"`
	AfterStandardT    float64                `json:"afterStandardT"`
	DeltaStandardT    float64                `json:"deltaStandardT"`
	GrandTotalBeforeT float64                `json:"grandTotalBeforeT"`
	GrandTotalAfterT  float64                `json:"grandTotalAfterT"`
	Samples           []AffectedRecordSample `json:"samples"`
}

// PreviewRequest 录入表单的单值折算预览。
type PreviewRequest struct {
	Amount    float64   `json:"amount"`
	Caliber   string    `json:"caliber"`
	CleanedAt date.Date `json:"cleanedAt"`
}

// PreviewResponse 单值折算结果。
type PreviewResponse struct {
	Amount       float64 `json:"amount"`
	Caliber      string  `json:"caliber"`
	CleanedAt    string  `json:"cleanedAt"`
	StandardT    float64 `json:"standardT"`
	DensityT_M3  float64 `json:"densityTPerM3"`
	WetToDry     float64 `json:"wetToDryFactor"`
	FactorSource string  `json:"factorSource"`
}

func (r ImpactRequest) String() string {
	return fmt.Sprintf("pair=%s factor=%.4f effectiveFrom=%s", r.Pair, r.Factor, r.EffectiveFrom)
}
