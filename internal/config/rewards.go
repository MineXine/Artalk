package config

import "github.com/artalkjs/artalk/v2/internal/uidlucky"

// RewardsConf 是积分、徽章、UID 靓号这一套成长体系的配置。
//
// 三项能力共用一个总开关，是因为它们互相依赖：徽章与靓号都靠积分兑换，
// 关掉积分再单独开着兑换没有意义。
type RewardsConf struct {
	Enabled bool `koanf:"enabled" json:"enabled"` // 成长体系总开关

	Points RewardsPointsConf `koanf:"points" json:"points"` // 积分获取规则
	Badges RewardsBadgeConf  `koanf:"badges" json:"badges"` // 徽章
	UID    RewardsUIDConf    `koanf:"uid" json:"uid"`       // UID 靓号
}

// RewardsPointsConf 定义积分的来源与额度。
//
// 每一项填 0 表示关闭该来源。积分没有"全局上限"，只有单来源的每日上限，
// 因为上限的意义是防止刷某种行为，而不是限制用户长期活跃。
type RewardsPointsConf struct {
	// CommentCreate 是发表一条评论获得的积分。
	CommentCreate int64 `koanf:"comment_create" json:"comment_create"`
	// CommentCreateDailyCap 是每天通过发评论最多能获得多少积分。
	CommentCreateDailyCap int64 `koanf:"comment_create_daily_cap" json:"comment_create_daily_cap"`
	// VoteReceived 是自己的评论每被赞一次获得的积分。
	VoteReceived int64 `koanf:"vote_received" json:"vote_received"`
	// Checkin 是每日签到获得的积分。
	Checkin int64 `koanf:"checkin" json:"checkin"`
}

// RewardsBadgeConf 控制徽章能力的开关。
//
// 徽章库本身由管理接口维护，不进配置文件——它属于运营数据，不是部署参数。
type RewardsBadgeConf struct {
	Enabled bool `koanf:"enabled" json:"enabled"`
	// MaxEquipped 预留给"同时佩戴多枚"的扩展，当前只支持佩戴一枚。
	MaxEquipped int `koanf:"max_equipped" json:"max_equipped"`

	// DisableAutoGrant 关掉"条件达标自动发放"。
	//
	// 写成"禁用"而不是"启用"，是为了让零值与既有行为一致：老配置里没有
	// 这一项时，条件徽章照常自动发放，不会因为升级而悄悄停掉。
	DisableAutoGrant bool `koanf:"disable_auto_grant" json:"disable_auto_grant"`
}

// RewardsUIDConf 控制 UID 靓号兑换。
type RewardsUIDConf struct {
	Enabled bool `koanf:"enabled" json:"enabled"`
	// Price 是各等级靓号的兑换价格，单位与积分一致。
	Price uidlucky.PriceTable `koanf:"price" json:"price"`
}

const (
	// DefaultPointsCommentCreate 等默认值刻意保守：一条评论 5 分、
	// 每天评论最多 30 分，意味着"只靠灌水"一天换不到一个 C 级靓号（400 分）。
	DefaultPointsCommentCreate      int64 = 5
	DefaultPointsCommentCreateDaily int64 = 30
	DefaultPointsVoteReceived       int64 = 1
	DefaultPointsCheckin            int64 = 10
	DefaultRewardsMaxEquipped       int   = 1
)

// Normalized 用默认值补齐缺失项。
//
// 注意这里把"未配置（0）"和"明确关闭（0）"视为同一种情况：积分规则想
// 真正关掉某项，应该用 rewards.enabled 或把整个来源理解为不启用，
// 而不是把数值写成 0 还期待它有别的含义。
func (c RewardsConf) Normalized() RewardsConf {
	out := c

	if out.Points.CommentCreate <= 0 {
		out.Points.CommentCreate = DefaultPointsCommentCreate
	}
	if out.Points.CommentCreateDailyCap <= 0 {
		out.Points.CommentCreateDailyCap = DefaultPointsCommentCreateDaily
	}
	if out.Points.VoteReceived <= 0 {
		out.Points.VoteReceived = DefaultPointsVoteReceived
	}
	if out.Points.Checkin <= 0 {
		out.Points.Checkin = DefaultPointsCheckin
	}
	if out.Badges.MaxEquipped <= 0 {
		out.Badges.MaxEquipped = DefaultRewardsMaxEquipped
	}
	out.UID.Price = out.UID.Price.Normalized()

	return out
}
