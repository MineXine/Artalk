package entity

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Name       string `gorm:"index;size:255"`
	Email      string `gorm:"index;size:255"`
	Link       string
	Password   string
	BadgeName  string
	BadgeColor string
	LastIP     string
	LastUA     string
	IsAdmin    bool

	// UID 是给人看的业务编号（门牌号），从 10001 起分配，与主键 ID 刻意分开，
	// 这样兑换靓号只动展示编号，不会影响评论、通知等按主键建立的外键关系。
	//
	// 唯一索引刻意不写在 tag 上：老库升级时 AutoMigrate 会先把所有历史行
	// 填成默认值 0，此时建唯一索引必然因重复值失败。索引改由
	// dao.migrateUserUID 在回填完成后显式创建。
	UID uint64 `gorm:"not null;default:0"`

	// Points 是积分余额。所有增减都走原子 UPDATE（见 dao.AdjustPoints），
	// 不读改写，避免并发兑换把余额扣成负数。
	Points int64 `gorm:"not null;default:0"`

	// IsAIAssistant marks the dedicated user identity used by the AI comment assistant.
	// It is separate from Email so an assistant may share an address with another user.
	IsAIAssistant  bool
	ReceiveEmail   bool `gorm:"default:true"`
	TokenValidFrom sql.NullTime

	// 配置文件中添加的
	IsInConf bool
}

func (u User) IsEmpty() bool {
	return u.ID == 0
}

func (u *User) SetPasswordEncrypt(password string) (err error) {
	var encrypted []byte
	if encrypted, err = bcrypt.GenerateFromPassword(
		[]byte(password), bcrypt.DefaultCost,
	); err != nil {
		return err
	}
	u.Password = "(bcrypt)" + string(encrypted)
	u.TokenValidFrom.Scan(time.Now())
	return nil
}

func (u *User) CheckPassword(input string) bool {
	if u.ID == 0 {
		return false
	}
	password := strings.TrimSpace(u.Password)
	if password == "" {
		return false
	}

	const BcryptPrefix = "(bcrypt)"
	const MD5Prefix = "(md5)"

	switch {
	case strings.HasPrefix(password, BcryptPrefix):
		if err := bcrypt.CompareHashAndPassword([]byte(password[len(BcryptPrefix):]),
			[]byte(input)); err == nil {
			return true
		}
	case strings.HasPrefix(password, MD5Prefix):
		if strings.EqualFold(password[len(MD5Prefix):],
			fmt.Sprintf("%x", md5.Sum([]byte(input)))) {
			return true
		}
	default:
		if password == input {
			return true
		}
	}

	return false
}
