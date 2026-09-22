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

// List 规则版本链列表。
func (h *Handler) List(c *fiber.Ctx) error {
	items, err := h.svc.List(c.UserContext())
	if err != nil {
		return err
	}
	return httpx.OK(c, items)
}

// Create 新增规则版本。
func (h *Handler) Create(c *fiber.Ctx) error {
	var req CreateRuleRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	rule, impact, err := h.svc.Create(c.UserContext(), req)
	if err != nil {
		return err
	}
	return httpx.Created(c, fiber.Map{"rule": rule, "impact": impact})
}

// Impact 规则影响预览（不写库）。
func (h *Handler) Impact(c *fiber.Ctx) error {
	req, err := ParseImpactRequest(c)
	if err != nil {
		return err
	}
	summary, err := h.svc.Impact(c.UserContext(), req)
	if err != nil {
		return err
	}
	return httpx.OK(c, summary)
}

// Preview 单值折算预览（录入表单使用）。
func (h *Handler) Preview(c *fiber.Ctx) error {
	var req PreviewRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	resp, err := h.svc.Preview(c.UserContext(), req)
	if err != nil {
		return err
	}
	return httpx.OK(c, resp)
}

// Delete 删除未被引用的最新版本。
func (h *Handler) Delete(c *fiber.Ctx) error {
	id, err := httpx.PathID(c, "id", "换算规则")
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return httpx.Message(c, "换算规则版本已删除", fiber.Map{"id": id})
}
