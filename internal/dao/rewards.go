package dao

import (
	"errors"
	"time"

	"github.com/artalkjs/artalk/v2/internal/entity"
	"gorm.io/gorm"
)

var (
	// ErrBadgeNotFound 表示徽章不存在或已停用。
	ErrBadgeNotFound = errors.New("badge not found")
	// ErrBadgeNotOwned 表示用户未持有该徽章。
	ErrBadgeNotOwned = errors.New("badge not owned")
	// ErrInsufficientPoints 表示积分余额不足。
	ErrInsufficientPoints = errors.New("insufficient points")
	// ErrUserNotFound 表示用户不存在。
	ErrUserNotFound = errors.New("user not found")
)

// ===============
//  Badge
// ===============

// FindAllBadges 返回徽章库。includeDisabled 为 false 时只给启用中的。
func (dao *Dao) FindAllBadges(includeDisabled bool) []entity.Badge {
	badges := []entity.Badge{}
	q := dao.DB().Model(&entity.Badge{})
	if !includeDisabled {
		q = q.Where("enabled = ?", true)
	}
	_ = q.Order("sort_order ASC, id ASC").Find(&badges).Error
	return badges
}

func (dao *Dao) FindBadgeByID(id uint) entity.Badge {
	var badge entity.Badge
	_ = dao.DB().Where("id = ?", id).First(&badge).Error
	return badge
}

func (dao *Dao) FindBadgeByKey(key string) entity.Badge {
	var badge entity.Badge
	_ = dao.DB().Where("key = ?", key).First(&badge).Error
	return badge
}

func (dao *Dao) CreateBadge(badge *entity.Badge) error {
	return dao.DB().Create(badge).Error
}

func (dao *Dao) UpdateBadge(badge *entity.Badge) error {
	return dao.DB().Save(badge).Error
}

// DeleteBadge 硬删除徽章定义，并清理所有持有关系。
//
// 硬删除是刻意的：Badge.Key 有唯一索引，软删除会让"删除后重建同名徽章"
// 撞上那条已删除的记录。徽章属于配置数据，硬删符合直觉。
func (dao *Dao) DeleteBadge(id uint) error {
	return dao.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("badge_id = ?", id).Delete(&entity.UserBadge{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&entity.Badge{}, id).Error
	})
}

// ===============
//  BadgeCategory
// ===============

// FindAllBadgeCategories 按展示顺序返回分类。
func (dao *Dao) FindAllBadgeCategories(includeDisabled bool) []entity.BadgeCategory {
	cats := []entity.BadgeCategory{}
	q := dao.DB().Model(&entity.BadgeCategory{})
	if !includeDisabled {
		q = q.Where("enabled = ?", true)
	}
	_ = q.Order("sort_order ASC").Order("id ASC").Find(&cats).Error
	return cats
}

func (dao *Dao) FindBadgeCategoryByID(id uint) entity.BadgeCategory {
	var cat entity.BadgeCategory
	if id == 0 {
		return cat
	}
	_ = dao.DB().Where("id = ?", id).First(&cat).Error
	return cat
}

func (dao *Dao) FindBadgeCategoryByKey(key string) entity.BadgeCategory {
	var cat entity.BadgeCategory
	if key == "" {
		return cat
	}
	_ = dao.DB().Where("key = ?", key).First(&cat).Error
	return cat
}

func (dao *Dao) CreateBadgeCategory(cat *entity.BadgeCategory) error {
	return dao.DB().Create(cat).Error
}

func (dao *Dao) UpdateBadgeCategory(cat *entity.BadgeCategory) error {
	return dao.DB().Save(cat).Error
}

// DeleteBadgeCategory 删除分类，把该分类下的徽章退回"未分类"。
//
// 不级联删除徽章：徽章是用户已经拿到手的资产，管理员整理分组不该让它们消失。
func (dao *Dao) DeleteBadgeCategory(id uint) error {
	return dao.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&entity.Badge{}).Where("category_id = ?", id).
			Update("category_id", 0).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&entity.BadgeCategory{}, id).Error
	})
}

// CountBadgesInCategory 统计分类下的徽章数，供删除前提示。
func (dao *Dao) CountBadgesInCategory(id uint) int64 {
	var n int64
	_ = dao.DB().Model(&entity.Badge{}).Where("category_id = ?", id).Count(&n).Error
	return n
}

// FindUserBadges 返回用户持有的徽章，附带徽章定义。
func (dao *Dao) FindUserBadges(userID uint) []entity.UserBadge {
	badges := []entity.UserBadge{}
	_ = dao.DB().Where("user_id = ?", userID).Order("equipped DESC, id ASC").Find(&badges).Error
	return badges
}

func (dao *Dao) FindUserBadge(userID uint, badgeID uint) entity.UserBadge {
	var ub entity.UserBadge
	_ = dao.DB().Where("user_id = ? AND badge_id = ?", userID, badgeID).First(&ub).Error
	return ub
}

// GrantBadge 发放徽章，已持有时返回 false 而不报错。
//
// 幂等由 (user_id, badge_id) 唯一索引保证：并发发放时后到的那次会拿到
// 唯一约束错误，这里把它当作"已经持有"处理。
func (dao *Dao) GrantBadge(userID uint, badgeID uint, source entity.BadgeSource, now time.Time) (bool, error) {
	if userID == 0 || badgeID == 0 {
		return false, nil
	}
	if !dao.FindUserBadge(userID, badgeID).IsEmpty() {
		return false, nil
	}

	ub := entity.UserBadge{
		UserID:    userID,
		BadgeID:   badgeID,
		Source:    source,
		GrantedAt: now,
	}
	if err := dao.DB().Create(&ub).Error; err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RevokeBadge 收回徽章。若被收回的正是佩戴中的徽章，一并清空用户快照。
func (dao *Dao) RevokeBadge(userID uint, badgeID uint) error {
	ub := dao.FindUserBadge(userID, badgeID)
	if ub.IsEmpty() {
		return ErrBadgeNotOwned
	}

	err := dao.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("id = ?", ub.ID).Delete(&entity.UserBadge{}).Error; err != nil {
			return err
		}
		if ub.Equipped {
			if err := tx.Model(&entity.User{}).Where("id = ?", userID).
				Updates(map[string]any{"badge_name": "", "badge_color": ""}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	dao.refreshUserCache(userID)
	return nil
}

// EquipBadge 佩戴徽章。一次只能佩戴一枚，因此先把其余的全部卸下。
//
// 同步写 users.badge_name / badge_color 是刻意的：评论列表、用户 DTO、
// 邮件模板都直接读这两列，保持它们是"当前佩戴徽章"的快照，老前端无需改动。
func (dao *Dao) EquipBadge(userID uint, badgeID uint) error {
	var badge entity.Badge

	err := dao.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND enabled = ?", badgeID, true).First(&badge).Error; err != nil {
			return ErrBadgeNotFound
		}

		var ub entity.UserBadge
		if err := tx.Where("user_id = ? AND badge_id = ?", userID, badgeID).First(&ub).Error; err != nil {
			return ErrBadgeNotOwned
		}
		if ub.Equipped {
			return nil // 已经戴着，幂等返回
		}

		if err := tx.Model(&entity.UserBadge{}).Where("user_id = ?", userID).
			Update("equipped", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&entity.UserBadge{}).Where("id = ?", ub.ID).
			Update("equipped", true).Error; err != nil {
			return err
		}
		return tx.Model(&entity.User{}).Where("id = ?", userID).
			Updates(map[string]any{
				"badge_name":  badge.Name,
				"badge_color": badge.Color,
			}).Error
	})
	if err != nil {
		return err
	}

	dao.refreshUserCache(userID)
	return nil
}

// UnequipBadge 卸下当前佩戴的徽章。
func (dao *Dao) UnequipBadge(userID uint) error {
	err := dao.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&entity.UserBadge{}).Where("user_id = ?", userID).
			Update("equipped", false).Error; err != nil {
			return err
		}
		return tx.Model(&entity.User{}).Where("id = ?", userID).
			Updates(map[string]any{"badge_name": "", "badge_color": ""}).Error
	})
	if err != nil {
		return err
	}

	dao.refreshUserCache(userID)
	return nil
}

// ===============
//  Points
// ===============

// AdjustPoints 原子地增减积分并落一条流水，返回变动后的余额。
//
// 扣减时把余额条件写进 WHERE，让"余额不足"由数据库判断，避免
// 先读后写的竞态把余额扣成负数。
//
// dedupKey 非空时先做一次查重：同一用户 + 同一原因 + 同一去重键
// 只入账一次，重复调用直接返回当前余额。
func (dao *Dao) AdjustPoints(
	userID uint, amount int64, reason entity.PointReason,
	refType string, refID uint, note string, dedupKey string,
) (int64, error) {
	if amount == 0 {
		return dao.UserPoints(userID), nil
	}
	if dedupKey != "" && dao.HasDedupRecord(userID, reason, dedupKey) {
		return dao.UserPoints(userID), nil
	}

	var balance int64
	err := dao.DB().Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&entity.User{}).Where("id = ?", userID)
		if amount < 0 {
			q = q.Where("points >= ?", -amount)
		}

		res := q.Update("points", gorm.Expr("points + ?", amount))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 没有命中的行：要么用户不存在，要么余额不足以扣减。
			if dao.userExists(tx, userID) {
				return ErrInsufficientPoints
			}
			return ErrUserNotFound
		}

		var user entity.User
		if err := tx.Select("id", "points").Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		balance = user.Points

		return tx.Create(&entity.PointTransaction{
			UserID:   userID,
			Amount:   amount,
			Balance:  balance,
			Reason:   string(reason),
			RefType:  refType,
			RefID:    refID,
			DedupKey: dedupKey,
			Note:     note,
		}).Error
	})
	if err != nil {
		return 0, err
	}

	dao.refreshUserCache(userID)
	return balance, nil
}

// HasDedupRecord 判断某个去重键是否已经入过账。
func (dao *Dao) HasDedupRecord(userID uint, reason entity.PointReason, dedupKey string) bool {
	if dedupKey == "" {
		return false
	}
	var count int64
	_ = dao.DB().Model(&entity.PointTransaction{}).
		Where("user_id = ? AND reason = ? AND dedup_key = ?", userID, string(reason), dedupKey).
		Count(&count).Error
	return count > 0
}

func (dao *Dao) userExists(tx *gorm.DB, userID uint) bool {
	var count int64
	_ = tx.Unscoped().Model(&entity.User{}).Where("id = ?", userID).Count(&count).Error
	return count > 0
}

// UserPoints 返回当前积分余额。
func (dao *Dao) UserPoints(userID uint) int64 {
	var user entity.User
	if err := dao.DB().Select("id", "points").Where("id = ?", userID).First(&user).Error; err != nil {
		return 0
	}
	return user.Points
}

// FindPointTransactions 按时间倒序返回积分流水。
func (dao *Dao) FindPointTransactions(userID uint, limit int, offset int) []entity.PointTransaction {
	list := []entity.PointTransaction{}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	_ = dao.DB().Where("user_id = ?", userID).
		Order("id DESC").Limit(limit).Offset(offset).Find(&list).Error
	return list
}

// SumPointsSince 汇总某个时间点之后某类原因的积分增量，用于每日上限。
func (dao *Dao) SumPointsSince(userID uint, reason entity.PointReason, since time.Time) int64 {
	var total int64
	_ = dao.DB().Model(&entity.PointTransaction{}).
		Where("user_id = ? AND reason = ? AND amount > 0 AND created_at >= ?", userID, string(reason), since).
		Select("COALESCE(SUM(amount), 0)").Scan(&total).Error
	return total
}

// HasPointRecordSince 判断某个时间点之后是否已有同类记录，用于签到幂等。
func (dao *Dao) HasPointRecordSince(userID uint, reason entity.PointReason, since time.Time) bool {
	var count int64
	_ = dao.DB().Model(&entity.PointTransaction{}).
		Where("user_id = ? AND reason = ? AND created_at >= ?", userID, string(reason), since).
		Count(&count).Error
	return count > 0
}

// ===============
//  Statistics
// ===============

// CountCommentsByUser 统计用户发表的评论数（不含待审与被折叠）。
func (dao *Dao) CountCommentsByUser(userID uint) int64 {
	var count int64
	_ = dao.DB().Model(&entity.Comment{}).
		Where("user_id = ? AND is_pending = ?", userID, false).
		Count(&count).Error
	return count
}

// CountVotesReceivedByUser 统计用户所有评论收到的赞数之和。
func (dao *Dao) CountVotesReceivedByUser(userID uint) int64 {
	var total int64
	_ = dao.DB().Model(&entity.Comment{}).
		Where("user_id = ?", userID).
		Select("COALESCE(SUM(vote_up), 0)").Scan(&total).Error
	return total
}

// refreshUserCache 在事务提交后重新加载用户并刷新缓存。
//
// 徽章与积分的写入都是"多列局部更新"，直接 Save 一个旧快照会把其他
// 并发修改覆盖回去，所以这里总是重新读一遍库再写缓存。
func (dao *Dao) refreshUserCache(userID uint) {
	if userID == 0 {
		return
	}
	var user entity.User
	if err := dao.DB().First(&user, userID).Error; err != nil {
		return
	}
	dao.CacheAction(func(cache *DaoCache) {
		_ = cache.UserCacheSave(&user)
	})
}
