package conversion

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Register 注册换算规则路由，并返回 service 供清淤记录模块装配依赖。
func Register(router fiber.Router, db *gorm.DB) *Service {
	svc := NewService(NewRepository(db))
	handler := NewHandler(svc)

	group := router.Group("/conversion-rules")
	group.Get("", handler.ListRules)
	group.Get("/caliber", handler.Caliber)
	group.Get("/logs", handler.ListLogs)
	group.Post("/impact-preview", handler.PreviewImpact)
	group.Post("", handler.CreateRule)

	return svc
}
