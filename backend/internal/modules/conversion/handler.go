package conversion

import (
	"github.com/gofiber/fiber/v2"

	"github.com/drainage/desilting/internal/httpx"
)

// Handler 换算规则 HTTP 接口。
type Handler struct {
	svc *Service
}

// NewHandler 构造处理器。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListRules 规则版本列表。
func (h *Handler) ListRules(c *fiber.Ctx) error {
	rules, err := h.svc.ListRules(c.UserContext())
	if err != nil {
		return err
	}
	return httpx.OK(c, rules)
}

// Caliber 当前统一口径说明。
func (h *Handler) Caliber(c *fiber.Ctx) error {
	caliber, err := h.svc.Caliber(c.UserContext())
	if err != nil {
		return err
	}
	return httpx.OK(c, caliber)
}

// PreviewImpact 预览新规则影响范围（不落库）。
func (h *Handler) PreviewImpact(c *fiber.Ctx) error {
	var req SaveRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	impact, err := h.svc.PreviewImpact(c.UserContext(), req)
	if err != nil {
		return err
	}
	return httpx.OK(c, impact)
}

// CreateRule 新增规则版本并重算生效区间内既有记录折算值。
func (h *Handler) CreateRule(c *fiber.Ctx) error {
	var req SaveRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	rule, impact, err := h.svc.CreateRule(c.UserContext(), req)
	if err != nil {
		return err
	}
	return httpx.Created(c, fiber.Map{"rule": rule, "impact": impact})
}

// ListLogs 规则变更日志。
func (h *Handler) ListLogs(c *fiber.Ctx) error {
	logs, err := h.svc.ListLogs(c.UserContext(), c.QueryInt("limit", 50))
	if err != nil {
		return err
	}
	return httpx.OK(c, logs)
}
