package handler

import (
	"strings"

	"github.com/artalkjs/artalk/v2/internal/core"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/i18n"
	"github.com/artalkjs/artalk/v2/server/common"
	"github.com/gofiber/fiber/v2"
)

// ParamsBadgeCategoryUpsert 是创建 / 更新徽章分类的请求体。
type ParamsBadgeCategoryUpsert struct {
	Key       string `json:"key" validate:"required"`  // Stable identifier, kebab-case
	Name      string `json:"name" validate:"required"` // Display name
	SortOrder int    `json:"sort_order" validate:"optional"`
	Enabled   *bool  `json:"enabled" validate:"optional"`
}

func (p *ParamsBadgeCategoryUpsert) normalize() string {
	p.Key = strings.TrimSpace(p.Key)
	p.Name = strings.TrimSpace(p.Name)

	if !badgeKeyPattern.MatchString(p.Key) {
		return i18n.T("Badge key must be kebab-case (a-z, 0-9, - and _)")
	}
	if p.Name == "" {
		return i18n.T("{{name}} is required", Map{"name": i18n.T("Name")})
	}
	if len([]rune(p.Name)) > 64 {
		return i18n.T("{{name}} is too long", Map{"name": i18n.T("Name")})
	}
	return ""
}

// ResponseBadgeCategories 是分类列表。
type ResponseBadgeCategories struct {
	Categories []entity.BadgeCategory `json:"categories"`
}

// @Id           AdminListBadgeCategories
// @Summary      List badge categories
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200  {object}  ResponseBadgeCategories
// @Failure      403  {object}  Map{msg=string}
// @Router       /badge-categories  [get]
func BadgeCategoryList(app *core.App, router fiber.Router) {
	router.Get("/badge-categories", common.AdminGuard(app, func(c *fiber.Ctx) error {
		return common.RespData(c, ResponseBadgeCategories{
			Categories: app.Dao().FindAllBadgeCategories(true),
		})
	}))
}

// @Id           AdminCreateBadgeCategory
// @Summary      Create a badge category
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        category  body  ParamsBadgeCategoryUpsert  true  "The category"
// @Accept       json
// @Produce      json
// @Success      200  {object}  entity.BadgeCategory
// @Failure      400  {object}  Map{msg=string}
// @Router       /badge-categories  [post]
func BadgeCategoryCreate(app *core.App, router fiber.Router) {
	router.Post("/badge-categories", common.AdminGuard(app, func(c *fiber.Ctx) error {
		var p ParamsBadgeCategoryUpsert
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}
		if msg := p.normalize(); msg != "" {
			return common.RespError(c, 400, msg)
		}
		if !app.Dao().FindBadgeCategoryByKey(p.Key).IsEmpty() {
			return common.RespError(c, 400, i18n.T("{{name}} already exists", Map{"name": "Category key"}))
		}

		category := entity.BadgeCategory{
			Key:       p.Key,
			Name:      p.Name,
			SortOrder: p.SortOrder,
			Enabled:   true,
		}
		if p.Enabled != nil {
			category.Enabled = *p.Enabled
		}
		if err := app.Dao().CreateBadgeCategory(&category); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
				strings.Contains(strings.ToLower(err.Error()), "unique") {
				return common.RespError(c, 400, i18n.T("{{name}} already exists", Map{"name": "Category key"}))
			}
			return common.RespError(c, 500, i18n.T("{{name}} creation failed", Map{"name": i18n.T("Category")}))
		}

		return common.RespData(c, category)
	}))
}

// @Id           AdminUpdateBadgeCategory
// @Summary      Update a badge category
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id        path  int                       true  "Category ID"
// @Param        category  body  ParamsBadgeCategoryUpsert  true  "The category"
// @Accept       json
// @Produce      json
// @Success      200  {object}  entity.BadgeCategory
// @Failure      400  {object}  Map{msg=string}
// @Failure      404  {object}  Map{msg=string}
// @Router       /badge-categories/{id}  [put]
func BadgeCategoryUpdate(app *core.App, router fiber.Router) {
	router.Put("/badge-categories/:id", common.AdminGuard(app, func(c *fiber.Ctx) error {
		categoryID, err := c.ParamsInt("id")
		if err != nil || categoryID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Category ID"}))
		}

		var p ParamsBadgeCategoryUpsert
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}
		if msg := p.normalize(); msg != "" {
			return common.RespError(c, 400, msg)
		}

		existing := app.Dao().FindBadgeCategoryByID(uint(categoryID))
		if existing.IsEmpty() {
			return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Category")}))
		}
		if p.Key != existing.Key {
			return common.RespError(c, 400, i18n.T("Category key cannot be changed"))
		}

		existing.Name = p.Name
		existing.SortOrder = p.SortOrder
		if p.Enabled != nil {
			existing.Enabled = *p.Enabled
		}
		if err := app.Dao().UpdateBadgeCategory(&existing); err != nil {
			return common.RespError(c, 500, i18n.T("{{name}} save failed", Map{"name": i18n.T("Category")}))
		}

		return common.RespData(c, existing)
	}))
}

// ResponseBadgeCategoryDelete 报告删除分类时有多少徽章被退回未分类。
type ResponseBadgeCategoryDelete struct {
	Moved int64 `json:"moved"`
}

// @Id           AdminDeleteBadgeCategory
// @Summary      Delete a badge category
// @Description  Badges in the category are moved to "uncategorised" instead of being deleted
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id  path  int  true  "Category ID"
// @Produce      json
// @Success      200  {object}  ResponseBadgeCategoryDelete
// @Failure      404  {object}  Map{msg=string}
// @Router       /badge-categories/{id}  [delete]
func BadgeCategoryDelete(app *core.App, router fiber.Router) {
	router.Delete("/badge-categories/:id", common.AdminGuard(app, func(c *fiber.Ctx) error {
		categoryID, err := c.ParamsInt("id")
		if err != nil || categoryID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Category ID"}))
		}
		if app.Dao().FindBadgeCategoryByID(uint(categoryID)).IsEmpty() {
			return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Category")}))
		}

		moved := app.Dao().CountBadgesInCategory(uint(categoryID))
		if err := app.Dao().DeleteBadgeCategory(uint(categoryID)); err != nil {
			return common.RespError(c, 500, i18n.T("{{name}} deletion failed", Map{"name": i18n.T("Category")}))
		}

		return common.RespData(c, ResponseBadgeCategoryDelete{Moved: moved})
	}))
}
