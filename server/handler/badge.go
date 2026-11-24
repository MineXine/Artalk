package handler

import (
	"errors"

	"github.com/artalkjs/artalk/v2/internal/core"
	"github.com/artalkjs/artalk/v2/internal/dao"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/i18n"
	"github.com/artalkjs/artalk/v2/server/common"
	"github.com/gofiber/fiber/v2"
)

// ResponseBadges 是徽章墙的数据。
type ResponseBadges struct {
	Badges []core.BadgeView `json:"badges"`
	// IsLogin 为 false 时 Badges 里的 owned / equipped 一律为 false，
	// 但徽章库本身仍然可见，未注册访客也能看到有哪些徽章可以拿。
	IsLogin bool   `json:"is_login"`
	Points  int64  `json:"points"`
	UID     uint64 `json:"uid"`
}

// @Id           GetBadges
// @Summary      List badges
// @Description  Return the badge library with the caller's ownership and progress
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200  {object}  ResponseBadges
// @Router       /badges  [get]
func BadgeList(app *core.App, router fiber.Router) {
	router.Get("/badges", func(c *fiber.Ctx) error {
		svc, err := core.AppService[*core.RewardsService](app)
		if err != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		// 徽章墙允许匿名浏览，所以这里不能用 LoginGuard
		user, _ := common.GetUserByReq(app, c)
		isLogin := !user.IsEmpty()

		userID := uint(0)
		if isLogin {
			userID = user.ID
		}

		return common.RespData(c, ResponseBadges{
			Badges:  svc.ListBadgesForUser(userID),
			IsLogin: isLogin,
			Points:  app.Dao().UserPoints(userID),
			UID:     user.UID,
		})
	})
}

// ParamsBadgeExchange 是兑换徽章的请求体。目前无需参数，保留结构体以便后续扩展。
type ParamsBadgeExchange struct{}

// ResponseBadgeMutation 是徽章相关写操作的通用返回。
type ResponseBadgeMutation struct {
	Points int64 `json:"points"`
}

// @Id           ExchangeBadge
// @Summary      Exchange a badge with points
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id  path  int  true  "Badge ID"
// @Produce      json
// @Success      200  {object}  ResponseBadgeMutation
// @Failure      400  {object}  Map{msg=string}
// @Failure      401  {object}  Map{msg=string}
// @Failure      403  {object}  Map{msg=string}
// @Router       /badges/{id}/exchange  [post]
func BadgeExchange(app *core.App, router fiber.Router) {
	router.Post("/badges/:id/exchange", common.LoginGuard(app, func(c *fiber.Ctx, user entity.User) error {
		svc, err := core.AppService[*core.RewardsService](app)
		if err != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		badgeID, err := c.ParamsInt("id")
		if err != nil || badgeID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Badge ID"}))
		}

		balance, err := svc.ExchangeBadge(user.ID, uint(badgeID))
		if err != nil {
			return respRewardsError(c, err)
		}

		return common.RespData(c, ResponseBadgeMutation{Points: balance})
	}))
}

// @Id           EquipBadge
// @Summary      Equip a badge
// @Description  Only one badge can be equipped at a time; equipping replaces the current one
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        id  path  int  true  "Badge ID"
// @Produce      json
// @Success      200  {object}  Map{msg=string}
// @Failure      400  {object}  Map{msg=string}
// @Failure      401  {object}  Map{msg=string}
// @Router       /badges/{id}/equip  [post]
func BadgeEquip(app *core.App, router fiber.Router) {
	router.Post("/badges/:id/equip", common.LoginGuard(app, func(c *fiber.Ctx, user entity.User) error {
		badgeID, err := c.ParamsInt("id")
		if err != nil || badgeID <= 0 {
			return common.RespError(c, 400, i18n.T("Invalid {{name}}", Map{"name": "Badge ID"}))
		}

		if err := app.Dao().EquipBadge(user.ID, uint(badgeID)); err != nil {
			return respRewardsError(c, err)
		}

		return common.RespSuccess(c)
	}))
}

// @Id           UnequipBadge
// @Summary      Take off the current badge
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200  {object}  Map{msg=string}
// @Failure      401  {object}  Map{msg=string}
// @Router       /badges/equip  [delete]
func BadgeUnequip(app *core.App, router fiber.Router) {
	router.Delete("/badges/equip", common.LoginGuard(app, func(c *fiber.Ctx, user entity.User) error {
		if err := app.Dao().UnequipBadge(user.ID); err != nil {
			return common.RespError(c, 500, i18n.T("Failed to take off the badge"))
		}
		return common.RespSuccess(c)
	}))
}

// respRewardsError 把服务层的哨兵错误翻译成 HTTP 状态码。
//
// 集中在一处映射，避免每个接口各写一遍 switch 导致同样的错误返回不同状态。
func respRewardsError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, core.ErrRewardsDisabled):
		return common.RespError(c, 403, i18n.T("Rewards are not enabled"))
	case errors.Is(err, dao.ErrBadgeNotFound):
		return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Badge")}))
	case errors.Is(err, dao.ErrBadgeNotOwned):
		return common.RespError(c, 400, i18n.T("You do not own this badge"))
	case errors.Is(err, core.ErrBadgeNotForSale):
		return common.RespError(c, 400, i18n.T("This badge cannot be exchanged with points"))
	case errors.Is(err, core.ErrBadgeAlreadyOwned):
		return common.RespError(c, 400, i18n.T("You already own this badge"))
	case errors.Is(err, dao.ErrInsufficientPoints):
		return common.RespError(c, 400, i18n.T("Not enough points"))
	case errors.Is(err, core.ErrUIDNotAvailable):
		return common.RespError(c, 400, i18n.T("This UID is not available"))
	case errors.Is(err, dao.ErrUIDBelowStart):
		return common.RespError(c, 400, i18n.T("This UID is reserved"))
	case errors.Is(err, dao.ErrUIDUnchanged):
		return common.RespError(c, 400, i18n.T("This is already your UID"))
	case errors.Is(err, dao.ErrUserNotFound):
		return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("User")}))
	}

	return common.RespError(c, 500, i18n.T("Operation failed"))
}
