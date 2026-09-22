package overdue

import (
	"github.com/gofiber/fiber/v2"

	"github.com/drainage/desilting/internal/httpx"
)

// Handler 超期预警 HTTP 接口。
type Handler struct {
	svc *Service
}

// NewHandler 构造处理器。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListWarnings 预警列表。
func (h *Handler) ListWarnings(c *fiber.Ctx) error {
	query, err := ParseListQuery(c)
	if err != nil {
		return err
	}
	items, total, err := h.svc.ListWarnings(c.UserContext(), query)
	if err != nil {
		return err
	}
	return httpx.OKPage(c, items, total, query.Page.Page, query.Page.PageSize)
}

// Summary 分阶段预警汇总。
func (h *Handler) Summary(c *fiber.Ctx) error {
	total, byStage, err := h.svc.CountByStage(c.UserContext())
	if err != nil {
		return err
	}
	return httpx.OK(c, Summary{Total: total, ByStage: byStage})
}

// Rules 阈值配置总览。
func (h *Handler) Rules(c *fiber.Ctx) error {
	overview, err := h.svc.RulesOverview(c.UserContext())
	if err != nil {
		return err
	}
	return httpx.OK(c, overview)
}

// SaveRule 新增阈值配置版本。
func (h *Handler) SaveRule(c *fiber.Ctx) error {
	var req SaveRuleRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	rule, err := h.svc.SaveRule(c.UserContext(), req)
	if err != nil {
		return err
	}
	return httpx.Message(c, "阈值配置已保存，自生效日期起参与判定", rule)
}
