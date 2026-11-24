package dao

import (
	"errors"
	"fmt"
	"strings"

	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/log"
	"gorm.io/gorm"
)

// UID 分配与迁移。
//
// 设计要点：UID 是展示用的业务编号，与主键 ID 解耦。所有写入都靠
// "MAX(uid)+1" 找一个候选值，再用唯一索引兜底并发抢占——冲突就重试，
// 因此不需要行锁，也不依赖任何数据库特有的序列语法。

var (
	// ErrUIDTaken 表示目标 UID 已被占用。
	ErrUIDTaken = errors.New("uid already taken")
	// ErrUIDUnchanged 表示目标 UID 与当前值相同，无需兑换。
	ErrUIDUnchanged = errors.New("uid unchanged")
	// ErrUIDBelowStart 表示目标 UID 小于起始值，属于保留段。
	ErrUIDBelowStart = errors.New("uid below the reserved start")
	// ErrUIDExhausted 表示连续抢占失败，通常意味着并发极高。
	ErrUIDExhausted = errors.New("uid allocation exhausted, please retry")
)

// userUIDIndexName 按当前 NamingStrategy 推导索引名，保证与 GORM 的
// HasIndex 判断一致（不同表前缀会得到不同的名字）。
func (dao *Dao) userUIDIndexName() string {
	return dao.DB().NamingStrategy.IndexName(dao.GetTableName(&entity.User{}), "uid")
}

// isUniqueViolation 判断错误是否来自唯一约束冲突。
//
// 这里刻意不用 gorm.ErrDuplicatedKey：它要求全局开启 TranslateError，
// 而那会改变项目里其他所有数据库错误的形态。改用各方言的固定措辞匹配，
// 覆盖面更窄但零副作用。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"unique constraint failed",    // sqlite
		"duplicate entry",             // mysql / mariadb
		"duplicate key value",         // postgresql
		"violation of unique key",     // sqlserver
		"cannot insert duplicate key", // sqlserver（另一种措辞）
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// migrateUserUID 给已存在的 users 表补上 uid 列并回填历史行。
//
// 只做"加列 + 回填"，不建索引：全新库在这一步时表还不存在，索引必须等
// AutoMigrate 建完表之后再补。见 createUserUIDIndex 的调用点。
//
// 两步顺序不可调换：加列时不能带唯一索引，因为所有历史行都会落在默认值
// 0 上；先回填让每行拿到互不相同的编号，约束才可能成立。
func (dao *Dao) migrateUserUID() {
	if !dao.DB().Migrator().HasTable(&entity.User{}) {
		return // 全新库：AutoMigrate 会一次建好列，回填由 CreateUser 按需做
	}

	table := dao.GetTableName(&entity.User{})

	if !dao.DB().Migrator().HasColumn(&entity.User{}, "uid") {
		// 用裸 SQL 加列，避开 GORM 把字段 tag 上的约束一并带上。
		// BIGINT 在 sqlite 下落到 INTEGER，其余三种方言原生支持。
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN uid BIGINT NOT NULL DEFAULT 0", table)
		if err := dao.DB().Exec(stmt).Error; err != nil {
			log.Fatal("[DB Migrator] Failed to add users.uid: ", err)
		}
		log.Info("[DB Migrator] Added column users.uid")
	}

	dao.backfillUserUID()
}

// backfillUserUID 给所有还没有编号的用户补上 UID。
//
// 用 Unscoped 覆盖软删除的行：它们和正常行共享同一个唯一索引，
// 留着 uid=0 会让索引无法建立。
func (dao *Dao) backfillUserUID() {
	var ids []uint
	if err := dao.DB().Unscoped().Model(&entity.User{}).
		Where("uid = 0").Order("id ASC").Pluck("id", &ids).Error; err != nil {
		log.Fatal("[DB Migrator] Failed to scan users without uid: ", err)
	}
	if len(ids) == 0 {
		return
	}

	// 从当前最大值继续，重复执行迁移也不会打乱已分配的编号。
	next := dao.nextUserUIDAfterMax()
	for _, id := range ids {
		if err := dao.DB().Unscoped().Model(&entity.User{}).
			Where("id = ?", id).Update("uid", next).Error; err != nil {
			log.Fatal("[DB Migrator] Failed to backfill user uid: ", err)
		}
		next++
	}
	log.Infof("[DB Migrator] Backfilled %d user UID(s) from %d", len(ids), entity.DefaultUIDStart)
}

// createUserUIDIndex 在数据唯一之后补建唯一索引。
func (dao *Dao) createUserUIDIndex() {
	indexName := dao.userUIDIndexName()
	if dao.DB().Migrator().HasIndex(&entity.User{}, indexName) {
		return
	}

	table := dao.GetTableName(&entity.User{})
	stmt := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (uid)", indexName, table)
	if err := dao.DB().Exec(stmt).Error; err != nil {
		// 索引可能被并发的另一个进程抢先建好，重新确认一次再决定是否致命。
		if dao.DB().Migrator().HasIndex(&entity.User{}, indexName) {
			return
		}
		log.Fatal("[DB Migrator] Failed to create users.uid unique index: ", err)
	}
	log.Info("[DB Migrator] Created unique index on users.uid")
}

// nextUserUIDAfterMax 返回当前最大 UID + 1，且不低于起始值。
func (dao *Dao) nextUserUIDAfterMax() uint64 {
	var maxUID uint64
	// COALESCE 让空表也返回 0，避免 Scan 到 NULL 报错。
	_ = dao.DB().Unscoped().Model(&entity.User{}).
		Select("COALESCE(MAX(uid), 0)").Scan(&maxUID).Error

	next := maxUID + 1
	if next < entity.DefaultUIDStart {
		next = entity.DefaultUIDStart
	}
	return next
}

// IsUIDTaken 判断某个 UID 是否已被占用（含软删除用户）。
func (dao *Dao) IsUIDTaken(uid uint64) bool {
	var count int64
	_ = dao.DB().Unscoped().Model(&entity.User{}).Where("uid = ?", uid).Count(&count).Error
	return count > 0
}

// FindUserByUID 按业务编号查用户。
func (dao *Dao) FindUserByUID(uid uint64) entity.User {
	var user entity.User
	_ = dao.DB().Where("uid = ?", uid).First(&user).Error
	return user
}

// ChangeUserUID 把用户的 UID 换成目标值，返回旧编号。
//
// 先用一次 COUNT 给出可读的错误（"已被占用"），真正的并发保证来自
// users.uid 上的唯一索引：两个请求同时通过 COUNT 检查时，只有一个能
// 写成功，另一个在 UPDATE 处收到唯一约束错误并转成 ErrUIDTaken。
func (dao *Dao) ChangeUserUID(userID uint, target uint64) (uint64, error) {
	if target < entity.DefaultUIDStart {
		return 0, ErrUIDBelowStart
	}

	var oldUID uint64
	err := dao.DB().Transaction(func(tx *gorm.DB) error {
		var user entity.User
		if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		oldUID = user.UID
		if oldUID == target {
			return ErrUIDUnchanged
		}

		var taken int64
		if err := tx.Unscoped().Model(&entity.User{}).
			Where("uid = ? AND id <> ?", target, userID).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrUIDTaken
		}

		if err := tx.Unscoped().Model(&entity.User{}).
			Where("id = ?", userID).Update("uid", target).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrUIDTaken
			}
			return err
		}
		return nil
	})

	return oldUID, err
}
