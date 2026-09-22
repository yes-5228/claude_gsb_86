package conversion

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Register 注册换算规则路由，并返回 service 供清淤记录模块装配为换算网关。
func Register(router fiber.Router, db *gorm.DB) *Service {
	svc := NewService(NewRepository(db))
	handler := NewHandler(svc)

	group := router.Group("/sludge-rules")
	group.Get("", handler.List)
	group.Post("", handler.Create)
	group.Post("/impact", handler.Impact)
	group.Post("/preview", handler.Preview)
	group.Delete("/:id", handler.Delete)

	return svc
}
