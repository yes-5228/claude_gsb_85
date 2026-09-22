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

// List 预警列表。
func (h *Handler) List(c *fiber.Ctx) error {
	query := ParseWarningListQuery(c)
	items, total, err := h.svc.List(c.UserContext(), query)
	if err != nil {
		return err
	}
	return httpx.OKPage(c, items, total, query.Page.Page, query.Page.PageSize)
}

// Summary 分阶段统计。
func (h *Handler) Summary(c *fiber.Ctx) error {
	summary, err := h.svc.Summary(c.UserContext())
	if err != nil {
		return err
	}
	return httpx.OK(c, summary)
}

// ListRules 规则列表。
func (h *Handler) ListRules(c *fiber.Ctx) error {
	rules, err := h.svc.ListRules(c.UserContext())
	if err != nil {
		return err
	}
	return httpx.OK(c, rules)
}

// CreateRule 新增规则。
func (h *Handler) CreateRule(c *fiber.Ctx) error {
	var req RuleSaveRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	rule, err := h.svc.CreateRule(c.UserContext(), req)
	if err != nil {
		return err
	}
	return httpx.Created(c, rule)
}
