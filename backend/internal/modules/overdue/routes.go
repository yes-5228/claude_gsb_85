package overdue

import (
	"context"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Register 注册超期预警路由，并返回 service 供任务列表与看板复用同一判定口径。
func Register(router fiber.Router, db *gorm.DB) *Service {
	svc := NewService(NewRepository(db))
	handler := NewHandler(svc)

	group := router.Group("/overdue-warnings")
	group.Get("", handler.ListWarnings)
	group.Get("/summary", handler.Summary)

	rules := router.Group("/overdue-rules")
	rules.Get("", handler.Rules)
	rules.Post("", handler.SaveRule)

	// 无论新老库都保证三阶段有可用阈值；失败不阻断启动，判定时会回退默认值。
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		slog.Error("初始化超期预警默认阈值失败", "error", err)
	}
	return svc
}
