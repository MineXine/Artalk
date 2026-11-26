package core

import (
	"fmt"

	"github.com/artalkjs/artalk/v2/internal/entity"
)

// 内置徽章：一套覆盖常见场景的默认配置，让新站点不必从空库开始画图标。
//
// 图标用 emoji 而不是图片文件：emoji 在任何平台都能渲染，不需要额外静态资源，
// 也不会因为主题换肤而失效。管理员随时可以把它换成图片 URL——Badge.Icon 两种
// 都接受。

type defaultBadgeCategory struct {
	Key       string
	Name      string
	SortOrder int
}

type defaultBadge struct {
	Key            string
	Name           string
	Color          string
	Icon           string
	Description    string
	CategoryKey    string
	GrantType      entity.BadgeGrantType
	ConditionKind  entity.BadgeConditionKind
	ConditionValue int64
	Price          int64
	SortOrder      int
}

var defaultBadgeCategories = []defaultBadgeCategory{
	{Key: "growth", Name: "成长足迹", SortOrder: 10},
	{Key: "popularity", Name: "人气荣誉", SortOrder: 20},
	{Key: "special", Name: "特殊收藏", SortOrder: 30},
}

var defaultBadges = []defaultBadge{
	// 成长足迹：发表行为累积
	{
		Key: "first-comment", Name: "初来乍到", Color: "#22c55e", Icon: "🌱",
		Description: "发出第一条评论", CategoryKey: "growth",
		GrantType: entity.BadgeGrantCondition, ConditionKind: entity.BadgeConditionCommentCount,
		ConditionValue: 1, SortOrder: 10,
	},
	{
		Key: "active-commenter", Name: "活跃分子", Color: "#3b82f6", Icon: "💬",
		Description: "累计发表 10 条评论", CategoryKey: "growth",
		GrantType: entity.BadgeGrantCondition, ConditionKind: entity.BadgeConditionCommentCount,
		ConditionValue: 10, SortOrder: 20,
	},
	{
		Key: "veteran-commenter", Name: "评论老手", Color: "#6366f1", Icon: "🚀",
		Description: "累计发表 100 条评论", CategoryKey: "growth",
		GrantType: entity.BadgeGrantCondition, ConditionKind: entity.BadgeConditionCommentCount,
		ConditionValue: 100, SortOrder: 30,
	},
	{
		Key: "old-timer", Name: "元老", Color: "#a16207", Icon: "🏛️",
		Description: "注册满一年", CategoryKey: "growth",
		GrantType: entity.BadgeGrantCondition, ConditionKind: entity.BadgeConditionRegisterDays,
		ConditionValue: 365, SortOrder: 40,
	},

	// 人气荣誉：别人给的认可
	{
		Key: "liked-10", Name: "小有人气", Color: "#f59e0b", Icon: "⭐",
		Description: "评论累计获得 10 个赞", CategoryKey: "popularity",
		GrantType: entity.BadgeGrantCondition, ConditionKind: entity.BadgeConditionVoteReceived,
		ConditionValue: 10, SortOrder: 10,
	},
	{
		Key: "liked-100", Name: "万众瞩目", Color: "#ef4444", Icon: "🔥",
		Description: "评论累计获得 100 个赞", CategoryKey: "popularity",
		GrantType: entity.BadgeGrantCondition, ConditionKind: entity.BadgeConditionVoteReceived,
		ConditionValue: 100, SortOrder: 20,
	},
	{
		Key: "liked-500", Name: "众望所归", Color: "#eab308", Icon: "👑",
		Description: "评论累计获得 500 个赞", CategoryKey: "popularity",
		GrantType: entity.BadgeGrantCondition, ConditionKind: entity.BadgeConditionVoteReceived,
		ConditionValue: 500, SortOrder: 30,
	},

	// 特殊收藏：赚不到，只能换或被人授予
	{
		Key: "rich", Name: "富甲一方", Color: "#f59e0b", Icon: "💰",
		Description: "用 1000 积分兑换", CategoryKey: "special",
		GrantType: entity.BadgeGrantExchange, ConditionKind: entity.BadgeConditionNone,
		Price: 1000, SortOrder: 10,
	},
	{
		Key: "collector", Name: "收藏家", Color: "#8b5cf6", Icon: "🎖️",
		Description: "由管理员授予的特别纪念", CategoryKey: "special",
		GrantType: entity.BadgeGrantManual, ConditionKind: entity.BadgeConditionNone,
		SortOrder: 20,
	},
	{
		Key: "founder", Name: "创始人", Color: "#ec4899", Icon: "🏵️",
		Description: "站点最早的一批支持者", CategoryKey: "special",
		GrantType: entity.BadgeGrantManual, ConditionKind: entity.BadgeConditionNone,
		SortOrder: 30,
	},
}

// SeedDefaultBadges 导入内置分类与徽章，返回新建的分类数和徽章数。
//
// 幂等：按 key 判断，已存在就跳过。刻意不覆盖已有记录——管理员可能改过名称
// 或阈值，重复导入不该把他的调整冲掉。
func (s *RewardsService) SeedDefaultBadges() (int, int, error) {
	d := s.app.Dao()
	if d == nil {
		return 0, 0, fmt.Errorf("database is not available")
	}

	categoryIDs := make(map[string]uint, len(defaultBadgeCategories))
	createdCategories := 0
	for _, c := range defaultBadgeCategories {
		if existing := d.FindBadgeCategoryByKey(c.Key); !existing.IsEmpty() {
			categoryIDs[c.Key] = existing.ID
			continue
		}
		category := entity.BadgeCategory{
			Key:       c.Key,
			Name:      c.Name,
			SortOrder: c.SortOrder,
			Enabled:   true,
		}
		if err := d.CreateBadgeCategory(&category); err != nil {
			return createdCategories, 0, err
		}
		categoryIDs[c.Key] = category.ID
		createdCategories++
	}

	createdBadges := 0
	for _, b := range defaultBadges {
		if !d.FindBadgeByKey(b.Key).IsEmpty() {
			continue
		}
		badge := entity.Badge{
			Key:            b.Key,
			Name:           b.Name,
			Color:          b.Color,
			Icon:           b.Icon,
			Description:    b.Description,
			CategoryID:     categoryIDs[b.CategoryKey],
			GrantType:      b.GrantType,
			ConditionKind:  b.ConditionKind,
			ConditionValue: b.ConditionValue,
			Price:          b.Price,
			SortOrder:      b.SortOrder,
			Enabled:        true,
		}
		if err := d.CreateBadge(&badge); err != nil {
			return createdCategories, createdBadges, err
		}
		createdBadges++
	}

	return createdCategories, createdBadges, nil
}
