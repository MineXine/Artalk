package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/artalkjs/artalk/v2/internal/config"
	"github.com/artalkjs/artalk/v2/internal/dao"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/log"
	"github.com/artalkjs/artalk/v2/internal/uidlucky"
)

var _ Service = (*RewardsService)(nil)

var (
	// ErrRewardsDisabled 表示成长体系总开关未开启。
	ErrRewardsDisabled = errors.New("rewards are disabled")
	// ErrBadgeNotForSale 表示该徽章不能用积分购买。
	ErrBadgeNotForSale = errors.New("badge is not for sale")
	// ErrBadgeAlreadyOwned 表示用户已持有该徽章。
	ErrBadgeAlreadyOwned = errors.New("badge already owned")
	// ErrUIDNotAvailable 表示目标号码不可兑换。
	ErrUIDNotAvailable = errors.New("uid is not available")
)

// RewardsService 把积分、徽章、UID 三条线串成一个"成长体系"。
//
// 它只做编排：原子性由 dao 保证（原子 UPDATE、唯一索引），这里负责
// 顺序、开关判断、失败补偿和条件判定。
type RewardsService struct {
	app *App
}

func NewRewardsService(app *App) *RewardsService {
	return &RewardsService{app: app}
}

func (s *RewardsService) Init() error {
	conf := s.conf()
	if conf.Enabled {
		log.Infof("[Rewards] enabled (points: comment=%d/day=%d vote=%d checkin=%d, badges=%v, uid=%v)",
			conf.Points.CommentCreate, conf.Points.CommentCreateDailyCap,
			conf.Points.VoteReceived, conf.Points.Checkin,
			conf.Badges.Enabled, conf.UID.Enabled)
	}
	return nil
}

func (s *RewardsService) Dispose() error {
	return nil
}

func (s *RewardsService) conf() config.RewardsConf {
	if s.app.Conf() == nil {
		return config.RewardsConf{}
	}
	return s.app.Conf().Rewards
}

// -------
//  积分
// -------

// startOfToday 返回本地时区的今天零点。
//
// 用 time.Local 而不是 UTC：每日上限和签到都要跟用户感知的"今天"一致，
// Bootstrap 已经按配置把 time.Local 设好了。
func startOfToday(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// AddPoints 给用户加积分。amount <= 0 时直接忽略，避免误用成扣减入口。
//
// dedupKey 非空时同键只入账一次，用于点赞这类"同一触发者不应重复计分"的场景。
func (s *RewardsService) AddPoints(userID uint, amount int64, reason entity.PointReason, refType string, refID uint, note string, dedupKey string) {
	if userID == 0 || amount <= 0 {
		return
	}
	if _, err := s.app.Dao().AdjustPoints(userID, amount, reason, refType, refID, note, dedupKey); err != nil {
		log.Errorf("[Rewards] add points failed (user=%d reason=%s): %v", userID, reason, err)
	}
}

// OnCommentCreated 在评论创建成功后结算积分与条件徽章。
//
// 调用方应当放在后台任务里：这里会做聚合查询，不该阻塞评论响应。
func (s *RewardsService) OnCommentCreated(userID uint, commentID uint) {
	conf := s.conf()
	if !conf.Enabled || userID == 0 {
		return
	}
	// 本方法是异步调用，应用关闭时 app.dao 已被置空，此时跳过结算而不是崩溃
	d := s.app.Dao()
	if d == nil {
		return
	}

	if conf.Points.CommentCreate > 0 && conf.Points.CommentCreateDailyCap > 0 {
		earned := d.SumPointsSince(userID, entity.PointReasonCommentCreate, startOfToday(time.Now()))
		remaining := conf.Points.CommentCreateDailyCap - earned
		if remaining > 0 {
			amount := conf.Points.CommentCreate
			if amount > remaining {
				amount = remaining
			}
			// 评论 ID 天然唯一，不需要额外的去重键
			s.AddPoints(userID, amount, entity.PointReasonCommentCreate, "comment", commentID, "", "")
		}
	}

	s.EvaluateBadgesForUser(userID)
}

// OnVoteReceived 在自己的评论被赞时结算积分。
//
// voterKey 标识"谁投的这一票"。同一个投票者对同一条评论只会计一次分，
// 由 dedupKey 保证——否则用户可以靠"取消赞再点赞"的循环无限刷分。
func (s *RewardsService) OnVoteReceived(commentOwnerID uint, commentID uint, voterKey string) {
	conf := s.conf()
	if !conf.Enabled || commentOwnerID == 0 {
		return
	}
	if conf.Points.VoteReceived <= 0 {
		return
	}
	// 与 OnCommentCreated 同理：这里跑在 goroutine 里，需要容忍应用已关闭
	if s.app.Dao() == nil {
		return
	}

	dedupKey := fmt.Sprintf("vote:%d:%s", commentID, voterKey)
	s.AddPoints(commentOwnerID, conf.Points.VoteReceived, entity.PointReasonVoteReceived,
		"vote", commentID, voterKey, dedupKey)

	s.EvaluateBadgesForUser(commentOwnerID)
}

// Checkin 执行每日签到。返回最新余额与本次是否真的签到成功。
func (s *RewardsService) Checkin(userID uint) (int64, bool, error) {
	conf := s.conf()
	if !conf.Enabled {
		return 0, false, ErrRewardsDisabled
	}
	if conf.Points.Checkin <= 0 {
		return s.app.Dao().UserPoints(userID), false, ErrRewardsDisabled
	}

	now := time.Now()
	if s.app.Dao().HasPointRecordSince(userID, entity.PointReasonCheckin, startOfToday(now)) {
		return s.app.Dao().UserPoints(userID), false, nil // 今天已经签过
	}

	today := now.Format("2006-01-02")
	// 前面的查询已经挡住重复签到，这里再去重一次是为了兜住并发：
	// 两个请求同时通过上面的检查时，只有一条流水能落库。
	balance, err := s.app.Dao().AdjustPoints(userID, conf.Points.Checkin,
		entity.PointReasonCheckin, "checkin", 0, today, "checkin:"+today)
	if err != nil {
		return 0, false, err
	}

	s.EvaluateBadgesForUser(userID)
	return balance, true, nil
}

// -------
//  徽章
// -------

// BadgeStat 汇总一次条件判定所需的统计值。
func (s *RewardsService) BadgeStat(userID uint) entity.BadgeStat {
	d := s.app.Dao()
	stat := entity.BadgeStat{
		CommentCount: d.CountCommentsByUser(userID),
		VoteReceived: d.CountVotesReceivedByUser(userID),
		Points:       d.UserPoints(userID),
	}

	if user := d.FindUserByID(userID); !user.IsEmpty() && !user.CreatedAt.IsZero() {
		days := int64(time.Since(user.CreatedAt).Hours() / 24)
		if days < 0 {
			days = 0
		}
		stat.RegisterDays = days
	}

	return stat
}

// EvaluateBadgesForUser 检查所有条件徽章，把新达成的发给用户。
//
// 返回本次新获得的徽章，便于调用方提示。没有条件徽章时只做一次查询
// 就返回，不让每次评论都跑两个聚合统计。
func (s *RewardsService) EvaluateBadgesForUser(userID uint) []entity.Badge {
	conf := s.conf()
	if !conf.Enabled || !conf.Badges.Enabled || userID == 0 {
		return nil
	}
	d := s.app.Dao()
	if d == nil {
		return nil // 应用正在关闭
	}

	badges := d.FindAllBadges(false)
	candidates := make([]entity.Badge, 0, len(badges))
	for _, b := range badges {
		if b.GrantType == entity.BadgeGrantCondition && b.ConditionKind != entity.BadgeConditionNone {
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	owned := make(map[uint]struct{})
	for _, ub := range d.FindUserBadges(userID) {
		owned[ub.BadgeID] = struct{}{}
	}

	// 全部已持有就不必再查统计
	pending := make([]entity.Badge, 0, len(candidates))
	for _, b := range candidates {
		if _, has := owned[b.ID]; !has {
			pending = append(pending, b)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	stat := s.BadgeStat(userID)
	now := time.Now()
	granted := []entity.Badge{}

	for _, b := range pending {
		if !b.IsConditionMet(stat) {
			continue
		}
		ok, err := d.GrantBadge(userID, b.ID, entity.BadgeSourceCondition, now)
		if err != nil {
			log.Errorf("[Rewards] grant badge %s to user %d failed: %v", b.Key, userID, err)
			continue
		}
		if ok {
			granted = append(granted, b)
		}
	}

	return granted
}

// ExchangeBadge 用积分兑换徽章。
//
// 扣分与发放不是一次事务，而是"先扣、失败再补"：GrantBadge 由唯一索引
// 保证幂等，所以补偿不会造成重复发放，而补一笔固定金额的流水比让两个
// 互相独立的 DAO 操作共享事务要简单得多。
func (s *RewardsService) ExchangeBadge(userID uint, badgeID uint) (int64, error) {
	conf := s.conf()
	if !conf.Enabled || !conf.Badges.Enabled {
		return 0, ErrRewardsDisabled
	}

	badge := s.app.Dao().FindBadgeByID(badgeID)
	if badge.IsEmpty() || !badge.Enabled {
		return 0, dao.ErrBadgeNotFound
	}
	if !badge.IsExchangeable() {
		return 0, ErrBadgeNotForSale
	}
	if !s.app.Dao().FindUserBadge(userID, badgeID).IsEmpty() {
		return 0, ErrBadgeAlreadyOwned
	}

	balance, err := s.app.Dao().AdjustPoints(userID, -badge.Price,
		entity.PointReasonBadgeExchange, "badge", badge.ID, badge.Name, "")
	if err != nil {
		return 0, err
	}

	if _, err := s.app.Dao().GrantBadge(userID, badgeID, entity.BadgeSourceExchange, time.Now()); err != nil {
		// 发放失败，把这笔积分退回去，流水留痕便于排查
		if _, refundErr := s.app.Dao().AdjustPoints(userID, badge.Price,
			entity.PointReasonRefund, "badge", badge.ID, "refund: "+badge.Key, ""); refundErr != nil {
			log.Errorf("[Rewards] refund badge %s for user %d failed: %v", badge.Key, userID, refundErr)
		}
		return 0, err
	}

	return balance, nil
}

// GrantBadgeByAdmin 管理员直接授予徽章。
func (s *RewardsService) GrantBadgeByAdmin(userID uint, badgeID uint) error {
	badge := s.app.Dao().FindBadgeByID(badgeID)
	if badge.IsEmpty() {
		return dao.ErrBadgeNotFound
	}
	_, err := s.app.Dao().GrantBadge(userID, badgeID, entity.BadgeSourceManual, time.Now())
	return err
}

// -------
//  UID
// -------

// QuoteUID 给目标号码报价。occupied 由数据库实时查询。
func (s *RewardsService) QuoteUID(target uint64) uidlucky.Quote {
	conf := s.conf()
	return uidlucky.QuoteFor(target, s.app.Dao().IsUIDTaken(target), conf.UID.Price)
}

// ExchangeUID 用积分兑换靓号。
//
// 顺序是"先报价 → 再扣分 → 再改号"：改号可能因为并发抢占失败（唯一
// 索引兜底），此时把积分退回去。反过来做（先改号再扣分）会在积分不足
// 时留下一个已经改好但没付钱的号码。
func (s *RewardsService) ExchangeUID(userID uint, target uint64) (uidlucky.Quote, int64, error) {
	conf := s.conf()
	if !conf.Enabled || !conf.UID.Enabled {
		return uidlucky.Quote{}, 0, ErrRewardsDisabled
	}

	quote := s.QuoteUID(target)
	if !quote.Available {
		return quote, 0, ErrUIDNotAvailable
	}

	balance, err := s.app.Dao().AdjustPoints(userID, -quote.Price,
		entity.PointReasonUIDExchange, "uid", 0, fmt.Sprintf("%d", target), "")
	if err != nil {
		return quote, 0, err
	}

	if _, err := s.app.Dao().ChangeUserUID(userID, target); err != nil {
		refundNote := fmt.Sprintf("refund uid %d", target)
		if _, refundErr := s.app.Dao().AdjustPoints(userID, quote.Price,
			entity.PointReasonRefund, "uid", 0, refundNote, ""); refundErr != nil {
			log.Errorf("[Rewards] refund uid %d for user %d failed: %v", target, userID, refundErr)
		}
		switch {
		case errors.Is(err, dao.ErrUIDTaken):
			return quote, 0, ErrUIDNotAvailable
		case errors.Is(err, dao.ErrUIDBelowStart):
			return quote, 0, dao.ErrUIDBelowStart
		}
		return quote, 0, err
	}

	return quote, balance, nil
}

// BadgeView 是徽章 + 持有状态的展示结构。
type BadgeView struct {
	entity.Badge
	Owned    bool               `json:"owned"`
	Equipped bool               `json:"equipped"`
	Source   entity.BadgeSource `json:"source,omitempty"`
	// Achieved 表示条件型徽章当前是否已达标（用于展示进度条）。
	Achieved bool  `json:"achieved"`
	Progress int64 `json:"progress"`
	Target   int64 `json:"target"`
}

// ListBadgesForUser 返回徽章库并标注该用户的持有情况。
func (s *RewardsService) ListBadgesForUser(userID uint) []BadgeView {
	badges := s.app.Dao().FindAllBadges(false)

	ownedByID := map[uint]entity.UserBadge{}
	if userID != 0 {
		for _, ub := range s.app.Dao().FindUserBadges(userID) {
			ownedByID[ub.BadgeID] = ub
		}
	}

	var stat entity.BadgeStat
	if userID != 0 {
		stat = s.BadgeStat(userID)
	}

	views := make([]BadgeView, 0, len(badges))
	for _, b := range badges {
		view := BadgeView{Badge: b}
		if ub, has := ownedByID[b.ID]; has {
			view.Owned = true
			view.Equipped = ub.Equipped
			view.Source = ub.Source
		}
		if b.ConditionKind != entity.BadgeConditionNone && b.ConditionValue > 0 {
			view.Target = b.ConditionValue
			view.Progress = conditionProgress(b.ConditionKind, stat)
			view.Achieved = b.IsConditionMet(stat)
		}
		views = append(views, view)
	}

	return views
}

// conditionProgress 取出条件对应的当前值，让前端能画进度。
func conditionProgress(kind entity.BadgeConditionKind, stat entity.BadgeStat) int64 {
	switch kind {
	case entity.BadgeConditionCommentCount:
		return stat.CommentCount
	case entity.BadgeConditionVoteReceived:
		return stat.VoteReceived
	case entity.BadgeConditionRegisterDays:
		return stat.RegisterDays
	case entity.BadgeConditionPoints:
		return stat.Points
	}
	return 0
}
