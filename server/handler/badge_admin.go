package handler

import (
	"regexp"
	"strings"

	"github.com/artalkjs/artalk/v2/internal/core"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/i18n"
	"github.com/artalkjs/artalk/v2/server/common"
	"github.com/gofiber/fiber/v2"
)

// badgeKeyPattern 限制徽章标识的字符集：它会被写进配置文件、导入导出
// 文件和 API 路径，保持 kebab-case 才不会在各处被转义。
var badgeKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

// ParamsBadgeUpsert 是创建 / 更新徽章的请求体。
type ParamsBadgeUpsert struct {
	Key         string `json:"key" validate:"required"`   // Stable identifier, kebab-case
	Name        string `json:"name" validate:"required"`  // Display name
	Color       string `json:"color" validate:"optional"` // hex or a CSS color name
	Icon        string `json:"icon" validate:"optional"`  // icon URL or a single emoji
	Description string `json:"description" validate:"optional"`
	// CategoryID 指向徽章分类，0 表示未分类
	CategoryID uint `json:"category_id" validate:"optional"`
	// GrantType 决定徽章在列表里的归类：condition / exchange / manual
	GrantType string `json:"grant_type" validate:"required"`
	// ConditionKind 仅对 condition 类有意义
	ConditionKind  string `json:"condition_kind" validate:"optional"`
	ConditionValue int64  `json:"condition_value" validate:"optional"`
	// Price 为 0 表示不能用积分购买
	Price     int64 `json:"price" validate:"optional"`
	SortOrder int   `json:"sort_order" validate:"optional"`
	// Enabled 用指针区分"没传"与"传了 false"
	Enabled *bool `json:"enabled" validate:"optional"`
}

// normalize 把入参收敛成合法值，返回错误信息（空串表示通过）。
func (p *ParamsBadgeUpsert) normalize() string {
	p.Key = strings.TrimSpace(p.Key)
	p.Name = strings.TrimSpace(p.Name)
	p.Color = strings.TrimSpace(p.Color)
	p.Icon = strings.TrimSpace(p.Icon)
	p.Description = strings.TrimSpace(p.Description)
	p.GrantType = strings.TrimSpace(p.GrantType)
	p.ConditionKind = strings.TrimSpace(p.ConditionKind)

	if !badgeKeyPattern.MatchString(p.Key) {
		return i18n.T("Badge key must be kebab-case (a-z, 0-9, - and _)")
	}
	if p.Name == "" {
		return i18n.T("{{name}} is required", Map{"name": i18n.T("Name")})
	}
	if len([]rune(p.Name)) > 64 {
		return i18n.T("{{name}} is too long", Map{"name": i18n.T("Name")})
	}
	if len([]rune(p.Description)) > 500 {
		return i18n.T("{{name}} is too long", Map{"name": i18n.T("Description")})
	}
	if p.Price < 0 {
		return i18n.T("{{name}} cannot be negative", Map{"name": i18n.T("Price")})
	}

	switch entity.BadgeGrantType(p.GrantType) {
	case entity.BadgeGrantCondition, entity.BadgeGrantExchange, entity.BadgeGrantManual:
	default:
		return i18n.T("Invalid {{name}}", Map{"name": "grant_type"})
	}

	if p.ConditionKind == "" {
		p.ConditionKind = string(entity.BadgeConditionNone)
	}
	if !entity.BadgeConditionKind(p.ConditionKind).Valid() {
		return i18n.T("Invalid {{name}}", Map{"name": "condition_kind"})
	}
	if p.ConditionValue < 0 {
		return i18n.T("{{name}} cannot be negative", Map{"name": "condition_value"})
	}
	// 条件徽章必须有阈值，否则永远发不出去，属于配置错误
	if entity.BadgeConditionKind(p.ConditionKind) != entity.BadgeConditionNone && p.ConditionValue <= 0 {
		return i18n.T("A condition badge requires a positive condition_value")
	}

	return ""
}

// validateBadgeCategory 确认目标分类存在。
//
// 挂在不存在的分类上的徽章不会报错，但会从任何分组里消失——徽章墙按分类
// 分组渲染，孤儿 ID 谁都不认领。所以在写入前拦住。
func validateBadgeCategory(app *core.App, categoryID uint) string {
	if categoryID == 0 {
		return ""
	}
	if app.Dao().FindBadgeCategoryByID(categoryID).IsEmpty() {
		return i18n.T("{{name}} not found", Map{"name": i18n.T("Category")})
	}
	return ""
}

// toEntity 把入参落到实体上。existing 为空表示新建。
func (p *ParamsBadgeUpsert) toEntity(existing entity.Badge) entity.Badge {
	badge := existing
	badge.Key = p.Key
	badge.Name = p.Name
	badge.Color = p.Color
	badge.Icon = p.Icon
	badge.Description = p.Description
	badge.CategoryID = p.CategoryID
	badge.GrantType = entity.BadgeGrantType(p.GrantType)
	badge.ConditionKind = entity.BadgeConditionKind(p.ConditionKind)
	badge.ConditionValue = p.ConditionValue
	badge.Price = p.Price
	badge.SortOrder = p.SortOrder

	if p.Enabled != nil {
		badge.Enabled = *p.Enabled
	} else if existing.IsEmpty() {
		badge.Enabled = true // 新建默认启用
	}

	return badge
}

// ResponseBadgeAdminList 是管理端的徽章列表。
type ResponseBadgeAdminList struct {
	Badges []entity.Badge `json:"badges"`
	// 管理端连停用的分类一起返回，否则"停用"就没有入口可以改回来。
	Categories []entity.BadgeCategory `json:"categories"`
}

// @Id           AdminListBadges
// @Summary      List all badges including disabled ones
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200  {object}  ResponseBadgeAdminList
// @Failure      403  {object}  Map{msg=string}
// @Router       /badges/all  [get]
func BadgeAdminList(app *core.App, router fiber.Router) {
	// 注册在 BadgeAdminCreate 之前也不冲突（方法不同），但放在一起更好读
	router.Get("/badges/all", common.AdminGuard(app, func(c *fiber.Ctx) error {
		return common.RespData(c, ResponseBadgeAdminList{
			Badges:     app.Dao().FindAllBadges(true),
			Categories: app.Dao().FindAllBadgeCategories(true),
		})
	}))
}

// @Id           AdminCreateBadge
// @Summary      Create a badge
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        badge  body  ParamsBadgeUpsert  true  "The badge"
// @Accept       json
// @Produce      json
// @Success      200  {object}  entity.Badge
// @Failure      400  {object}  Map{msg=string}
// @Failure      403  {object}  Map{msg=string}
// @Router       /badges  [post]
func BadgeAdminCreate(app *core.App, router fiber.Router) {
	router.Post("/badges", common.AdminGuard(app, func(c *fiber.Ctx) error {
		var p ParamsBadgeUpsert
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}
		if msg := p.normalize(); msg != "" {
			return common.RespError(c, 400, msg)
		}
		if msg := validateBadgeCategory(app, p.CategoryID); msg != "" {
			return common.RespError(c, 400, msg)
		}
		if !app.Dao().FindBadgeByKey(p.Key).IsEmpty() {
			return common.RespError(c, 400, i18n.T("{{name}} already exists", Map{"name": "Badge key"}))
		}

		badge := p.toEntity(entity.Badge{})
		if err := app.Dao().CreateBadge(&badge); err != nil {
			// 并发创建同名 key 时会撞唯一索引
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
				strings.Contains(strings.ToLower(err.Error()), "unique") {
				return common.RespError(c, 400, i18n.T("{{name}} already exists", Map{"name": "Badge key"}))
			}
			return common.RespError(c, 500, i18n.T("{{name}} creation failed", Map{"name": i18n.T("Badge")}))
		}

		return common.RespData(c, badge)
	}))
}

// @Id           AdminUpdateBadge
// @Summary      Update a badge
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id     path  int                true  "Badge ID"
// @Param        badge  body  ParamsBadgeUpsert  true  "The badge"
// @Accept       json
// @Produce      json
// @Success      200  {object}  entity.Badge
// @Failure      400  {object}  Map{msg=string}
// @Failure      404  {object}  Map{msg=string}
// @Router       /badges/{id}  [put]
func BadgeAdminUpdate(app *core.App, router fiber.Router) {
	router.Put("/badges/:id", common.AdminGuard(app, func(c *fiber.Ctx) error {
		badgeID, err := c.ParamsInt("id")
		if err != nil || badgeID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Badge ID"}))
		}

		var p ParamsBadgeUpsert
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}
		if msg := p.normalize(); msg != "" {
			return common.RespError(c, 400, msg)
		}
		if msg := validateBadgeCategory(app, p.CategoryID); msg != "" {
			return common.RespError(c, 400, msg)
		}

		existing := app.Dao().FindBadgeByID(uint(badgeID))
		if existing.IsEmpty() {
			return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Badge")}))
		}

		// Key 是外部引用（API 路径、导入导出），换掉会让引用悬空
		if p.Key != existing.Key {
			return common.RespError(c, 400, i18n.T("Badge key cannot be changed"))
		}

		badge := p.toEntity(existing)
		if err := app.Dao().UpdateBadge(&badge); err != nil {
			return common.RespError(c, 500, i18n.T("{{name}} save failed", Map{"name": i18n.T("Badge")}))
		}

		return common.RespData(c, badge)
	}))
}

// @Id           AdminDeleteBadge
// @Summary      Delete a badge and every ownership of it
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id  path  int  true  "Badge ID"
// @Produce      json
// @Success      200  {object}  Map{msg=string}
// @Failure      404  {object}  Map{msg=string}
// @Router       /badges/{id}  [delete]
func BadgeAdminDelete(app *core.App, router fiber.Router) {
	router.Delete("/badges/:id", common.AdminGuard(app, func(c *fiber.Ctx) error {
		badgeID, err := c.ParamsInt("id")
		if err != nil || badgeID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Badge ID"}))
		}
		if app.Dao().FindBadgeByID(uint(badgeID)).IsEmpty() {
			return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Badge")}))
		}
		if err := app.Dao().DeleteBadge(uint(badgeID)); err != nil {
			return common.RespError(c, 500, i18n.T("{{name}} deletion failed", Map{"name": i18n.T("Badge")}))
		}
		return common.RespSuccess(c)
	}))
}

// ParamsAdminAdjustPoints 是管理员调整积分的请求体。
type ParamsAdminAdjustPoints struct {
	// Amount 为增量，可正可负；扣减时余额不足会被拒绝
	Amount int64  `json:"amount" validate:"required"`
	Note   string `json:"note" validate:"optional"`
}

// ResponseAdminAdjustPoints 返回调整后的余额。
type ResponseAdminAdjustPoints struct {
	Points int64 `json:"points"`
}

// @Id           AdminAdjustPoints
// @Summary      Adjust a user's points
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id       path  int                       true  "User ID"
// @Param        payload  body  ParamsAdminAdjustPoints   true  "The adjustment"
// @Accept       json
// @Produce      json
// @Success      200  {object}  ResponseAdminAdjustPoints
// @Failure      400  {object}  Map{msg=string}
// @Failure      404  {object}  Map{msg=string}
// @Router       /users/{id}/points  [post]
func UserPointsAdjust(app *core.App, router fiber.Router) {
	router.Post("/users/:id/points", common.AdminGuard(app, func(c *fiber.Ctx) error {
		userID, err := c.ParamsInt("id")
		if err != nil || userID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "User ID"}))
		}

		var p ParamsAdminAdjustPoints
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}
		if p.Amount == 0 {
			return common.RespError(c, 400, i18n.T("{{name}} cannot be zero", Map{"name": i18n.T("Amount")}))
		}

		note := strings.TrimSpace(p.Note)
		if len([]rune(note)) > 200 {
			note = string([]rune(note)[:200])
		}

		balance, err := app.Dao().AdjustPoints(uint(userID), p.Amount,
			entity.PointReasonAdminAdjust, "admin", 0, note, "")
		if err != nil {
			return respRewardsError(c, err)
		}

		return common.RespData(c, ResponseAdminAdjustPoints{Points: balance})
	}))
}

// ResponseUserBadges 是某用户的徽章持有情况。
type ResponseUserBadges struct {
	Badges     []core.BadgeView       `json:"badges"`
	Categories []entity.BadgeCategory `json:"categories"`
	Points     int64                  `json:"points"`
	UID        uint64                 `json:"uid"`
}

// @Id           AdminListUserBadges
// @Summary      List a user's badges
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id  path  int  true  "User ID"
// @Produce      json
// @Success      200  {object}  ResponseUserBadges
// @Failure      404  {object}  Map{msg=string}
// @Router       /users/{id}/badges  [get]
func UserBadgesList(app *core.App, router fiber.Router) {
	router.Get("/users/:id/badges", common.AdminGuard(app, func(c *fiber.Ctx) error {
		userID, err := c.ParamsInt("id")
		if err != nil || userID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "User ID"}))
		}

		user := app.Dao().FindUserByID(uint(userID))
		if user.IsEmpty() {
			return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("User")}))
		}

		svc, svcErr := core.AppService[*core.RewardsService](app)
		if svcErr != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		wall := svc.ListBadgesForUser(user.ID)

		return common.RespData(c, ResponseUserBadges{
			Badges:     wall.Badges,
			Categories: wall.Categories,
			Points:     user.Points,
			UID:        user.UID,
		})
	}))
}

// @Id           AdminGrantUserBadge
// @Summary      Grant a badge to a user
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id       path  int  true  "User ID"
// @Param        badgeId  path  int  true  "Badge ID"
// @Produce      json
// @Success      200  {object}  Map{msg=string}
// @Failure      400  {object}  Map{msg=string}
// @Failure      404  {object}  Map{msg=string}
// @Router       /users/{id}/badges/{badgeId}  [post]
func UserBadgeGrant(app *core.App, router fiber.Router) {
	router.Post("/users/:id/badges/:badgeId", common.AdminGuard(app, func(c *fiber.Ctx) error {
		userID, err := c.ParamsInt("id")
		if err != nil || userID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "User ID"}))
		}
		badgeID, err := c.ParamsInt("badgeId")
		if err != nil || badgeID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Badge ID"}))
		}
		if app.Dao().FindUserByID(uint(userID)).IsEmpty() {
			return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("User")}))
		}

		svc, svcErr := core.AppService[*core.RewardsService](app)
		if svcErr != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		if err := svc.GrantBadgeByAdmin(uint(userID), uint(badgeID)); err != nil {
			return respRewardsError(c, err)
		}
		return common.RespSuccess(c)
	}))
}

// @Id           AdminRevokeUserBadge
// @Summary      Take a badge back from a user
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id       path  int  true  "User ID"
// @Param        badgeId  path  int  true  "Badge ID"
// @Produce      json
// @Success      200  {object}  Map{msg=string}
// @Failure      400  {object}  Map{msg=string}
// @Router       /users/{id}/badges/{badgeId}  [delete]
func UserBadgeRevoke(app *core.App, router fiber.Router) {
	router.Delete("/users/:id/badges/:badgeId", common.AdminGuard(app, func(c *fiber.Ctx) error {
		userID, err := c.ParamsInt("id")
		if err != nil || userID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "User ID"}))
		}
		badgeID, err := c.ParamsInt("badgeId")
		if err != nil || badgeID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Badge ID"}))
		}

		if err := app.Dao().RevokeBadge(uint(userID), uint(badgeID)); err != nil {
			return respRewardsError(c, err)
		}
		return common.RespSuccess(c)
	}))
}

// ResponseBadgeSeed 报告这次导入了多少内置数据。
type ResponseBadgeSeed struct {
	Categories int `json:"categories"`
	Badges     int `json:"badges"`
}

// @Id           AdminSeedDefaultBadges
// @Summary      Import the built-in badge set
// @Description  Idempotent — keys that already exist are left untouched
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200  {object}  ResponseBadgeSeed
// @Failure      403  {object}  Map{msg=string}
// @Failure      500  {object}  Map{msg=string}
// @Router       /badges/seed-defaults  [post]
func BadgeSeedDefaults(app *core.App, router fiber.Router) {
	router.Post("/badges/seed-defaults", common.AdminGuard(app, func(c *fiber.Ctx) error {
		svc, svcErr := core.AppService[*core.RewardsService](app)
		if svcErr != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		categories, badges, err := svc.SeedDefaultBadges()
		if err != nil {
			return common.RespError(c, 500, i18n.T("Operation failed"))
		}

		return common.RespData(c, ResponseBadgeSeed{Categories: categories, Badges: badges})
	}))
}
