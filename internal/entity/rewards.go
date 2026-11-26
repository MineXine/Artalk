package entity

import (
	"time"
)

// ===============
//  Badge
// ===============

// BadgeGrantType 是徽章的主要获取途径。
//
// 一个徽章只会有一个"主途径"，用于在列表里给它归类：
//   - condition 满足条件自动发放
//   - exchange  用积分兑换
//   - manual    仅管理员授予
//
// 主途径不排斥其他途径：一个 condition 徽章如果同时设置了 Price，
// 买家也可以在未达标时直接换购。
type BadgeGrantType string

const (
	BadgeGrantCondition BadgeGrantType = "condition"
	BadgeGrantExchange  BadgeGrantType = "exchange"
	BadgeGrantManual    BadgeGrantType = "manual"
)

// BadgeConditionKind 是条件徽章的判定维度。
//
// ConditionValue 的含义随 Kind 变化：前两者是"至少多少次"，
// register_days 是"至少多少天"，points 是"余额至少多少"。
type BadgeConditionKind string

const (
	BadgeConditionNone         BadgeConditionKind = "none"
	BadgeConditionCommentCount BadgeConditionKind = "comment_count"
	BadgeConditionVoteReceived BadgeConditionKind = "vote_received"
	BadgeConditionRegisterDays BadgeConditionKind = "register_days"
	BadgeConditionPoints       BadgeConditionKind = "points"
)

// Valid 判断条件类型是否被支持，避免脏配置导致徽章永远发不出去。
func (k BadgeConditionKind) Valid() bool {
	switch k {
	case BadgeConditionNone, BadgeConditionCommentCount, BadgeConditionVoteReceived,
		BadgeConditionRegisterDays, BadgeConditionPoints:
		return true
	}
	return false
}

// BadgeCategory 是徽章墙上的分组。
//
// 分类只影响展示顺序和分组方式，不参与发放逻辑：把一枚徽章从一个分类挪到
// 另一个，不会影响谁拥有它。
type BadgeCategory struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Key       string `gorm:"uniqueIndex;size:64" json:"key"` // 稳定标识，供导入导出引用
	Name      string `gorm:"size:64" json:"name"`
	SortOrder int    `gorm:"not null;default:0" json:"sort_order"`
	Enabled   bool   `gorm:"not null;default:true" json:"enabled"`
}

func (c BadgeCategory) IsEmpty() bool {
	return c.ID == 0
}

// Badge 是徽章定义（徽章库），与"谁拥有它"分开存放在 UserBadge。
//
// 刻意不用 gorm.Model：徽章是配置类数据，采用硬删除（软删除会让 Key 的
// 唯一索引在重建同名徽章时冲突），而 gorm.Model 的字段也没有 json tag，
// 直接序列化会给前端返回 "ID" / "CreatedAt" 这种驼峰键。
type Badge struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Key  string `gorm:"uniqueIndex;size:64" json:"key"` // 稳定标识，供 API、导入导出引用
	Name string `gorm:"size:64" json:"name"`            // 展示名
	// Color 允许 hex（#ff8800）或语义名（orange），前端原样输出到 CSS 变量。
	Color string `gorm:"size:32" json:"color"`
	Icon  string `gorm:"size:512" json:"icon"` // 图标 URL 或单个 emoji
	// Description 说明这枚徽章怎么来的，会展示在徽章墙上。
	Description string `gorm:"size:500" json:"description"`

	// CategoryID 指向徽章分类，0 表示未分类。删除分类时徽章会退回 0 而不是
	// 一起消失——徽章属于用户，不该因为整理目录而丢掉。
	CategoryID uint `gorm:"index;not null;default:0" json:"category_id"`

	GrantType BadgeGrantType `gorm:"size:32;index" json:"grant_type"`

	ConditionKind  BadgeConditionKind `gorm:"size:32" json:"condition_kind"`
	ConditionValue int64              `gorm:"not null;default:0" json:"condition_value"`

	// Price 是兑换所需积分，0 表示不可用积分购买。
	Price int64 `gorm:"not null;default:0" json:"price"`

	SortOrder int  `gorm:"not null;default:0" json:"sort_order"`
	Enabled   bool `gorm:"not null;default:true" json:"enabled"`
}

func (b Badge) IsEmpty() bool {
	return b.ID == 0
}

// IsExchangeable 判断能否用积分购买。
func (b Badge) IsExchangeable() bool {
	return b.Enabled && b.Price > 0
}

// IsConditionMet 用给定的统计值判断条件是否达成。
//
// 条件为 none 或未配置阈值时永远返回 false：这种徽章只能由管理员授予，
// 自动判定不应该凭空发放。
func (b Badge) IsConditionMet(stat BadgeStat) bool {
	if !b.Enabled || b.ConditionKind == BadgeConditionNone || b.ConditionValue <= 0 {
		return false
	}
	switch b.ConditionKind {
	case BadgeConditionCommentCount:
		return stat.CommentCount >= b.ConditionValue
	case BadgeConditionVoteReceived:
		return stat.VoteReceived >= b.ConditionValue
	case BadgeConditionRegisterDays:
		return stat.RegisterDays >= b.ConditionValue
	case BadgeConditionPoints:
		return stat.Points >= b.ConditionValue
	}
	return false
}

// BadgeStat 是一次条件判定所需的用户统计快照。
type BadgeStat struct {
	CommentCount int64
	VoteReceived int64
	RegisterDays int64
	Points       int64
}

// BadgeSource 记录这枚徽章是怎么到用户手上的。
type BadgeSource string

const (
	BadgeSourceCondition BadgeSource = "condition" // 自动达成
	BadgeSourceExchange  BadgeSource = "exchange"  // 积分兑换
	BadgeSourceManual    BadgeSource = "manual"    // 管理员授予
	BadgeSourceImport    BadgeSource = "import"    // 数据迁移带过来的
)

// UserBadge 是"用户持有某枚徽章"的关系。
//
// 唯一索引 (user_id, badge_id) 让重复发放天然幂等。同样采用硬删除，
// 否则收回后再次发放会撞上已软删除的那一行。
type UserBadge struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	UserID  uint `gorm:"uniqueIndex:idx_user_badge_pair,priority:1;index" json:"user_id"`
	BadgeID uint `gorm:"uniqueIndex:idx_user_badge_pair,priority:2;index" json:"badge_id"`

	Source   BadgeSource `gorm:"size:32" json:"source"`
	Equipped bool        `gorm:"not null;default:false;index" json:"equipped"`

	GrantedAt time.Time `json:"granted_at"`
}

func (ub UserBadge) IsEmpty() bool {
	return ub.ID == 0
}

// ===============
//  Points
// ===============

// PointReason 是积分变动的业务原因，同时用于每日上限的归类统计。
type PointReason string

const (
	PointReasonCommentCreate PointReason = "comment_create" // 发表评论
	PointReasonVoteReceived  PointReason = "vote_received"  // 评论被赞
	PointReasonCheckin       PointReason = "checkin"        // 每日签到
	PointReasonBadgeExchange PointReason = "badge_exchange" // 兑换徽章
	PointReasonUIDExchange   PointReason = "uid_exchange"   // 兑换靓号
	PointReasonAdminAdjust   PointReason = "admin_adjust"   // 管理员调整
	// PointReasonRefund 是兑换失败后的退回。单独一种原因而不是用负数冲正，
	// 是为了让流水里"花出去又退回来"这件事一眼可辨。
	PointReasonRefund PointReason = "refund"
)

// PointTransaction 是积分流水。
//
// 只追加不修改。Balance 存变动后的余额快照，是为了让对账不必重放全部
// 历史——出问题时直接看最后一行的 Balance 与 users.points 是否一致。
type PointTransaction struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	UserID  uint   `gorm:"index;index:idx_point_dedup,priority:1" json:"user_id"`
	Amount  int64  `gorm:"not null" json:"amount"`  // 正数入账，负数出账
	Balance int64  `gorm:"not null" json:"balance"` // 变动后余额
	Reason  string `gorm:"size:32;index;index:idx_point_dedup,priority:2" json:"reason"`

	// RefType / RefID 指向触发这次变动的对象，如 comment:123、badge:4。
	RefType string `gorm:"size:32" json:"ref_type"`
	RefID   uint   `json:"ref_id"`

	// DedupKey 是可选的业务去重键：同一用户 + 同一原因 + 同一去重键
	// 只会产生一条入账。用于"同一个人给同一条评论点赞只计一次分"，
	// 防止用户用"取消赞再点赞"的循环刷分。属于内部字段，不下发。
	DedupKey string `gorm:"size:128;index:idx_point_dedup,priority:3" json:"-"`

	Note string `gorm:"size:255" json:"note"`
}

func (t PointTransaction) IsEmpty() bool {
	return t.ID == 0
}

// UID 相关的常量。
const (
	// DefaultUIDStart 是业务编号的起点。User.ID（主键）仍然从 1 开始，
	// UID 是给人看的门牌号，两者刻意分开，避免靓号兑换动到主键。
	DefaultUIDStart uint64 = 10001

	// MaxUIDExchangeRetries 是抢号失败时的重试次数。冲突来自唯一索引，
	// 说明别人先一步拿走了，重试只会拿到同一个"已被占用"结论，
	// 所以只需要有限几次来吸收并发插入的时序差。
	MaxUIDExchangeRetries = 3
)
