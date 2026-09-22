package overdue

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Register 注册超期预警路由，并返回 service 供任务列表与看板复用同一套口径。
func Register(router fiber.Router, db *gorm.DB) *Service {
	svc := NewService(NewRepository(db))
	handler := NewHandler(svc)

	group := router.Group("/overdue-warnings")
	group.Get("", handler.List)
	group.Get("/summary", handler.Summary)

	rules := router.Group("/overdue-rules")
	rules.Get("", handler.ListRules)
	rules.Post("", handler.CreateRule)

	return svc
}
