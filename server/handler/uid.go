package handler

import (
	"github.com/artalkjs/artalk/v2/internal/core"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/i18n"
	"github.com/artalkjs/artalk/v2/internal/uidlucky"
	"github.com/artalkjs/artalk/v2/server/common"
	"github.com/gofiber/fiber/v2"
)

// ParamsUIDQuote 是查询某个号码报价的参数。
type ParamsUIDQuote struct {
	UID uint64 `query:"uid" json:"uid" validate:"required"` // The UID to price
}

// ResponseUIDQuote 把报价和用户当前状况一起返回，方便前端直接渲染。
type ResponseUIDQuote struct {
	uidlucky.Quote
	CurrentUID uint64 `json:"current_uid"` // 调用者当前的 UID，未登录为 0
	Points     int64  `json:"points"`      // 调用者当前积分，未登录为 0
	IsLogin    bool   `json:"is_login"`
}

// @Id           QuoteUID
// @Summary      Price a UID
// @Description  Report whether a UID is a "lucky number", its tier and its price
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        uid  query  int  true  "The UID to price"
// @Produce      json
// @Success      200  {object}  ResponseUIDQuote
// @Failure      400  {object}  Map{msg=string}
// @Router       /uid/quote  [get]
func UIDQuote(app *core.App, router fiber.Router) {
	router.Get("/uid/quote", func(c *fiber.Ctx) error {
		var p ParamsUIDQuote
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}

		svc, err := core.AppService[*core.RewardsService](app)
		if err != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		// 报价允许匿名查询：知道一个号多少钱本身不敏感，
		// 而且未注册访客先看到价格再决定注册，转化路径更短。
		user, _ := common.GetUserByReq(app, c)
		isLogin := !user.IsEmpty()

		userID := uint(0)
		if isLogin {
			userID = user.ID
		}

		return common.RespData(c, ResponseUIDQuote{
			Quote:      svc.QuoteUID(p.UID),
			CurrentUID: user.UID,
			Points:     app.Dao().UserPoints(userID),
			IsLogin:    isLogin,
		})
	})
}

// ParamsUIDExchange 是兑换靓号的请求体。
type ParamsUIDExchange struct {
	UID uint64 `json:"uid" validate:"required"` // The UID to buy
}

// ResponseUIDExchange 是兑换成功后的结果。
type ResponseUIDExchange struct {
	uidlucky.Quote
	PreviousUID uint64 `json:"previous_uid"`
	CurrentUID  uint64 `json:"current_uid"`
	Points      int64  `json:"points"`
}

// @Id           ExchangeUID
// @Summary      Exchange a lucky UID with points
// @Description  Replace the caller's UID with a purchased lucky number
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        payload  body  ParamsUIDExchange  true  "The UID to buy"
// @Accept       json
// @Produce      json
// @Success      200  {object}  ResponseUIDExchange
// @Failure      400  {object}  Map{msg=string}
// @Failure      401  {object}  Map{msg=string}
// @Failure      403  {object}  Map{msg=string}
// @Router       /uid/exchange  [post]
func UIDExchange(app *core.App, router fiber.Router) {
	router.Post("/uid/exchange", common.LoginGuard(app, func(c *fiber.Ctx, user entity.User) error {
		var p ParamsUIDExchange
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}

		svc, err := core.AppService[*core.RewardsService](app)
		if err != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		previous := user.UID
		quote, balance, err := svc.ExchangeUID(user.ID, p.UID)
		if err != nil {
			return respRewardsError(c, err)
		}

		return common.RespData(c, ResponseUIDExchange{
			Quote:       quote,
			PreviousUID: previous,
			CurrentUID:  p.UID,
			Points:      balance,
		})
	}))
}
