package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/artalkjs/artalk/v2/internal/core"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/server/common"
	"github.com/artalkjs/artalk/v2/server/handler"
	"github.com/artalkjs/artalk/v2/test"
	"github.com/stretchr/testify/require"
)

// enableRewards 在内存里打开成长体系。
//
// 测试配置里 rewards 默认关闭，这里直接改内存中的配置对象——
// RewardsService 每次都读 app.Conf()，所以立刻生效，无需重启。
func enableRewards(t *testing.T, app *test.TestApp) {
	t.Helper()
	conf := app.Conf().Rewards
	conf.Enabled = true
	conf.Badges.Enabled = true
	conf.UID.Enabled = true
	app.Conf().Rewards = conf.Normalized()
}

// TestRewardsUIDAllocation 校验 UID 从 10001 起自动分配且互不重复。
func TestRewardsUIDAllocation(t *testing.T) {
	app, _ := NewApiTestApp()
	t.Cleanup(func() { require.NoError(t, app.Cleanup()) })

	// fixtures 里三个用户的编号连号，起点就是 10001
	require.Equal(t, uint64(10001), app.Dao().FindUserByID(1000).UID)
	require.Equal(t, uint64(10002), app.Dao().FindUserByID(1001).UID)
	require.Equal(t, uint64(10003), app.Dao().FindUserByID(1002).UID)

	first, err := app.Dao().FindCreateUser("RewardsFirst", "rewards-first@example.org", "")
	require.NoError(t, err)
	require.Greater(t, first.UID, uint64(10003))

	second, err := app.Dao().FindCreateUser("RewardsSecond", "rewards-second@example.org", "")
	require.NoError(t, err)
	require.Equal(t, first.UID+1, second.UID, "uid should be allocated sequentially")

	// 同一个 name + email 再查一次应当命中已有用户，不会重复分配
	again, err := app.Dao().FindCreateUser("RewardsFirst", "rewards-first@example.org", "")
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.Equal(t, first.UID, again.UID)

	// 全库不变量：每个用户都有编号，且编号互不重复
	var users []entity.User
	require.NoError(t, app.Dao().DB().Unscoped().Find(&users).Error)
	seen := make(map[uint64]uint, len(users))
	for _, u := range users {
		require.NotZero(t, u.UID, "user %d (%s) has no uid", u.ID, u.Name)
		dupID, exists := seen[u.UID]
		require.False(t, exists, "uid %d is shared by user %d and %d", u.UID, dupID, u.ID)
		seen[u.UID] = u.ID
	}
}

// TestRewardsBadgeExchangeFlow 覆盖徽章从创建到佩戴的完整链路。
func TestRewardsBadgeExchangeFlow(t *testing.T) {
	app, router := NewApiTestApp()
	t.Cleanup(func() { require.NoError(t, app.Cleanup()) })

	handler.BadgeList(app.App, router)
	handler.BadgeExchange(app.App, router)
	handler.BadgeEquip(app.App, router)
	handler.BadgeUnequip(app.App, router)
	handler.BadgeAdminCreate(app.App, router)
	handler.BadgeAdminList(app.App, router)
	handler.UserPointsAdjust(app.App, router)
	enableRewards(t, app)

	admin, err := common.LoginGetUserToken(app.Dao().FindUserByID(1000), app.Conf().AppKey, 60)
	require.NoError(t, err)
	reader, err := common.LoginGetUserToken(app.Dao().FindUserByID(1001), app.Conf().AppKey, 60)
	require.NoError(t, err)

	// 匿名也能看徽章墙，只是没有持有状态
	flowRequest(t, router, http.MethodGet, "/badges", nil, "", http.StatusOK)

	// 管理员上架一枚 500 分的兑换徽章
	create := handler.ParamsBadgeUpsert{
		Key: "founder", Name: "创始人", Color: "#ff8800",
		Description: "首批支持者", GrantType: "exchange", Price: 500,
	}
	var badge entity.Badge
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodPost, "/badges", create, admin, http.StatusOK), &badge))
	require.NotZero(t, badge.ID)
	require.True(t, badge.Enabled, "a newly created badge should be enabled by default")

	// 同 key 不能重复创建
	flowRequest(t, router, http.MethodPost, "/badges", create, admin, http.StatusBadRequest)

	// 非管理员不能建徽章
	flowRequest(t, router, http.MethodPost, "/badges",
		handler.ParamsBadgeUpsert{Key: "sneaky", Name: "偷偷", GrantType: "manual"},
		reader, http.StatusForbidden)

	exchangePath := fmt.Sprintf("/badges/%d/exchange", badge.ID)

	// 0 分时兑换被拒绝
	flowRequest(t, router, http.MethodPost, exchangePath, nil, reader, http.StatusBadRequest)
	require.True(t, app.Dao().FindUserBadge(1001, badge.ID).IsEmpty())

	// 管理员补 600 分
	flowRequest(t, router, http.MethodPost, "/users/1001/points",
		map[string]any{"amount": 600, "note": "manual grant"}, admin, http.StatusOK)
	require.Equal(t, int64(600), app.Dao().UserPoints(1001))

	// 兑换成功，余额扣到 100
	flowRequest(t, router, http.MethodPost, exchangePath, nil, reader, http.StatusOK)
	require.Equal(t, int64(100), app.Dao().UserPoints(1001))
	require.False(t, app.Dao().FindUserBadge(1001, badge.ID).IsEmpty())

	// 重复兑换同一枚徽章被拒绝，且不会重复扣分
	flowRequest(t, router, http.MethodPost, exchangePath, nil, reader, http.StatusBadRequest)
	require.Equal(t, int64(100), app.Dao().UserPoints(1001))

	// 佩戴后 users.badge_name / badge_color 同步为徽章快照
	equipPath := fmt.Sprintf("/badges/%d/equip", badge.ID)
	flowRequest(t, router, http.MethodPost, equipPath, nil, reader, http.StatusOK)
	stored := app.Dao().FindUserByID(1001)
	require.Equal(t, "创始人", stored.BadgeName)
	require.Equal(t, "#ff8800", stored.BadgeColor)
	require.True(t, app.Dao().FindUserBadge(1001, badge.ID).Equipped)

	// 佩戴是幂等的
	flowRequest(t, router, http.MethodPost, equipPath, nil, reader, http.StatusOK)

	// 卸下后快照被清空
	flowRequest(t, router, http.MethodDelete, "/badges/equip", nil, reader, http.StatusOK)
	stored = app.Dao().FindUserByID(1001)
	require.Empty(t, stored.BadgeName)
	require.False(t, app.Dao().FindUserBadge(1001, badge.ID).Equipped)

	// 未持有的徽章不能佩戴
	other := handler.ParamsBadgeUpsert{Key: "veteran", Name: "老用户", GrantType: "manual"}
	var otherBadge entity.Badge
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodPost, "/badges", other, admin, http.StatusOK), &otherBadge))
	flowRequest(t, router, http.MethodPost, fmt.Sprintf("/badges/%d/equip", otherBadge.ID),
		nil, reader, http.StatusBadRequest)
}

// TestRewardsConditionBadgeGrant 校验条件徽章在达标时自动发放，且只发一次。
func TestRewardsConditionBadgeGrant(t *testing.T) {
	app, router := NewApiTestApp()
	t.Cleanup(func() { require.NoError(t, app.Cleanup()) })

	handler.BadgeAdminCreate(app.App, router)
	enableRewards(t, app)

	admin, err := common.LoginGetUserToken(app.Dao().FindUserByID(1000), app.Conf().AppKey, 60)
	require.NoError(t, err)

	// 评论数 >= 1 即达成
	create := handler.ParamsBadgeUpsert{
		Key: "first-comment", Name: "初次发声", GrantType: "condition",
		ConditionKind: string(entity.BadgeConditionCommentCount), ConditionValue: 1,
	}
	var badge entity.Badge
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodPost, "/badges", create, admin, http.StatusOK), &badge))

	svc, err := core.AppService[*core.RewardsService](app.App)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// fixtures 里的用户可能已经发过评论，用一个全新用户来验证"达标前不发"
	newcomer, err := app.Dao().FindCreateUser("BadgeNewcomer", "badge-newcomer@example.org", "")
	require.NoError(t, err)

	require.True(t, app.Dao().FindUserBadge(newcomer.ID, badge.ID).IsEmpty())
	require.Empty(t, svc.EvaluateBadgesForUser(newcomer.ID))

	// 造一条评论让它达标
	page := app.Dao().FindCreatePage("/rewards-badge", "Rewards", "Default Site")
	require.NoError(t, app.Dao().CreateComment(&entity.Comment{
		Content: "第一个评论", UserID: newcomer.ID, PageKey: page.Key, SiteName: page.SiteName,
		IsVerified: true,
	}))

	granted := svc.EvaluateBadgesForUser(newcomer.ID)
	require.Len(t, granted, 1)
	require.Equal(t, badge.ID, granted[0].ID)
	require.False(t, app.Dao().FindUserBadge(newcomer.ID, badge.ID).IsEmpty())

	// 再判一次不会重复发放
	require.Empty(t, svc.EvaluateBadgesForUser(newcomer.ID))
}

// TestRewardsCheckinIsIdempotent 校验同一天重复签到不会重复加积分。
func TestRewardsCheckinIsIdempotent(t *testing.T) {
	app, router := NewApiTestApp()
	t.Cleanup(func() { require.NoError(t, app.Cleanup()) })

	handler.PointsCheckin(app.App, router)
	handler.PointsGet(app.App, router)
	enableRewards(t, app)

	reader, err := common.LoginGetUserToken(app.Dao().FindUserByID(1001), app.Conf().AppKey, 60)
	require.NoError(t, err)

	var first handler.ResponseCheckin
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodPost, "/points/checkin", nil, reader, http.StatusOK), &first))
	require.True(t, first.CheckedIn)
	require.Equal(t, app.Conf().Rewards.Normalized().Points.Checkin, first.Awarded)

	// 第二次是 no-op
	var second handler.ResponseCheckin
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodPost, "/points/checkin", nil, reader, http.StatusOK), &second))
	require.False(t, second.CheckedIn)
	require.Zero(t, second.Awarded)
	require.Equal(t, first.Points, second.Points)

	// 概览里也应标记为已签到
	var overview handler.ResponsePoints
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodGet, "/points", nil, reader, http.StatusOK), &overview))
	require.True(t, overview.CheckedInToday)
	require.Equal(t, first.Points, overview.Points)
	require.Equal(t, app.Dao().FindUserByID(1001).UID, overview.UID)
}

// TestRewardsUIDExchange 覆盖报价与兑换。
func TestRewardsUIDExchange(t *testing.T) {
	app, router := NewApiTestApp()
	t.Cleanup(func() { require.NoError(t, app.Cleanup()) })

	handler.UIDQuote(app.App, router)
	handler.UIDExchange(app.App, router)
	handler.UserPointsAdjust(app.App, router)
	enableRewards(t, app)

	admin, err := common.LoginGetUserToken(app.Dao().FindUserByID(1000), app.Conf().AppKey, 60)
	require.NoError(t, err)
	reader, err := common.LoginGetUserToken(app.Dao().FindUserByID(1002), app.Conf().AppKey, 60)
	require.NoError(t, err)

	// 匿名也能查价
	var quote handler.ResponseUIDQuote
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodGet, "/uid/quote?uid=88888", nil, "", http.StatusOK), &quote))
	require.True(t, quote.Available)
	require.Equal(t, int64(8000), quote.Price)
	require.False(t, quote.IsLogin)

	// 普通号不可购买
	var plain handler.ResponseUIDQuote
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodGet, "/uid/quote?uid=12346", nil, "", http.StatusOK), &plain))
	require.False(t, plain.Available)
	require.Equal(t, "uid_not_lucky", plain.Message)

	// 被 fixture 用户占用的号不能买
	var taken handler.ResponseUIDQuote
	require.NoError(t, json.Unmarshal(
		flowRequest(t, router, http.MethodGet, "/uid/quote?uid=10001", nil, "", http.StatusOK), &taken))
	require.False(t, taken.Available)
	require.Equal(t, "uid_already_taken", taken.Message)

	// 积分不足时兑换被拒，UID 不变
	before := app.Dao().FindUserByID(1002).UID
	flowRequest(t, router, http.MethodPost, "/uid/exchange",
		map[string]any{"uid": 88888}, reader, http.StatusBadRequest)
	require.Equal(t, before, app.Dao().FindUserByID(1002).UID)

	// 补够积分后兑换成功
	flowRequest(t, router, http.MethodPost, "/users/1002/points",
		map[string]any{"amount": 9000, "note": "uid budget"}, admin, http.StatusOK)
	flowRequest(t, router, http.MethodPost, "/uid/exchange",
		map[string]any{"uid": 88888}, reader, http.StatusOK)
	require.Equal(t, uint64(88888), app.Dao().FindUserByID(1002).UID)
	require.Equal(t, int64(1000), app.Dao().UserPoints(1002))

	// 旧号被释放：它本身不是靓号，所以"能买"为假是正常的，
	// 这里直接验证它已经不在占用表里。
	require.False(t, app.Dao().IsUIDTaken(before), "the previous uid should have been released")
	require.True(t, app.Dao().FindUserByUID(before).IsEmpty())
}
