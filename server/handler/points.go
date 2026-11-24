package handler

import (
	"time"

	"github.com/artalkjs/artalk/v2/internal/core"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/i18n"
	"github.com/artalkjs/artalk/v2/server/common"
	"github.com/gofiber/fiber/v2"
)

// ParamsPoints 是积分流水的分页参数。
type ParamsPoints struct {
	Limit  int `query:"limit" json:"limit" validate:"optional"`   // Page size
	Offset int `query:"offset" json:"offset" validate:"optional"` // Offset
}

// ResponsePointsRules 把当前的积分规则一并返回，让前端不必硬编码文案。
type ResponsePointsRules struct {
	CommentCreate         int64 `json:"comment_create"`
	CommentCreateDailyCap int64 `json:"comment_create_daily_cap"`
	VoteReceived          int64 `json:"vote_received"`
	Checkin               int64 `json:"checkin"`
}

// ResponsePoints 是"我的积分"页所需的全部数据。
type ResponsePoints struct {
	UID            uint64                    `json:"uid"`
	Points         int64                     `json:"points"`
	CheckedInToday bool                      `json:"checked_in_today"`
	Rules          ResponsePointsRules       `json:"rules"`
	Transactions   []entity.PointTransaction `json:"transactions"`
}

// @Id           GetPoints
// @Summary      Get my points
// @Description  Return the current balance, today's check-in state and recent transactions
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Param        options  query  ParamsPoints  false  "Pagination"
// @Produce      json
// @Success      200  {object}  ResponsePoints
// @Failure      401  {object}  Map{msg=string}
// @Router       /points  [get]
func PointsGet(app *core.App, router fiber.Router) {
	router.Get("/points", common.LoginGuard(app, func(c *fiber.Ctx, user entity.User) error {
		var p ParamsPoints
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}

		conf := app.Conf().Rewards
		// 余额从库里重新取，避免 token 解析时的缓存快照落后于刚完成的签到。
		points := app.Dao().UserPoints(user.ID)

		return common.RespData(c, ResponsePoints{
			UID:            user.UID,
			Points:         points,
			CheckedInToday: app.Dao().HasPointRecordSince(user.ID, entity.PointReasonCheckin, startOfTodayLocal()),
			Rules: ResponsePointsRules{
				CommentCreate:         conf.Points.CommentCreate,
				CommentCreateDailyCap: conf.Points.CommentCreateDailyCap,
				VoteReceived:          conf.Points.VoteReceived,
				Checkin:               conf.Points.Checkin,
			},
			Transactions: app.Dao().FindPointTransactions(user.ID, p.Limit, p.Offset),
		})
	}))
}

// ResponseCheckin 是签到的结果。
type ResponseCheckin struct {
	Points    int64 `json:"points"`     // 最新余额
	Awarded   int64 `json:"awarded"`    // 本次获得的积分，0 表示今天已签过
	CheckedIn bool  `json:"checked_in"` // 本次是否真的签到成功
}

// @Id           Checkin
// @Summary      Daily check-in
// @Description  Award points once per local day, repeated calls are no-ops
// @Tags         Rewards
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200  {object}  ResponseCheckin
// @Failure      401  {object}  Map{msg=string}
// @Failure      403  {object}  Map{msg=string}
// @Router       /points/checkin  [post]
func PointsCheckin(app *core.App, router fiber.Router) {
	router.Post("/points/checkin", common.LoginGuard(app, func(c *fiber.Ctx, user entity.User) error {
		svc, err := core.AppService[*core.RewardsService](app)
		if err != nil || svc == nil {
			return common.RespError(c, 500, i18n.T("Rewards service is unavailable"))
		}

		balance, ok, err := svc.Checkin(user.ID)
		if err != nil {
			if err == core.ErrRewardsDisabled {
				return common.RespError(c, 403, i18n.T("Rewards are not enabled"))
			}
			return common.RespError(c, 500, i18n.T("Check-in failed"))
		}

		awarded := int64(0)
		if ok {
			awarded = app.Conf().Rewards.Normalized().Points.Checkin
		}

		return common.RespData(c, ResponseCheckin{
			Points:    balance,
			Awarded:   awarded,
			CheckedIn: ok,
		})
	}))
}

// startOfTodayLocal 返回本地时区今天零点，与服务层的每日判定保持一致。
func startOfTodayLocal() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}
