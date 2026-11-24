package uidlucky_test

import (
	"testing"

	"github.com/artalkjs/artalk/v2/internal/uidlucky"
	"github.com/stretchr/testify/assert"
)

func TestEvaluateTiers(t *testing.T) {
	cases := []struct {
		name   string
		uid    uint64
		tier   uidlucky.Tier
		reason uidlucky.Reason
	}{
		{"六位豹子", 111111, uidlucky.TierS, uidlucky.ReasonRepeated},
		{"五位豹子", 88888, uidlucky.TierS, uidlucky.ReasonRepeated},
		{"四个一位数", 7777, uidlucky.TierS, uidlucky.ReasonRepeated},

		{"升序顺子", 12345, uidlucky.TierA, uidlucky.ReasonSequential},
		{"降序顺子", 54321, uidlucky.TierA, uidlucky.ReasonSequential},
		{"四位顺子", 6789, uidlucky.TierA, uidlucky.ReasonSequential},
		{"五位回文", 12321, uidlucky.TierA, uidlucky.ReasonPalindrome},
		{"起点号恰好是回文", 10001, uidlucky.TierA, uidlucky.ReasonPalindrome},

		{"尾部三位重复", 12888, uidlucky.TierB, uidlucky.ReasonTailTriple},
		{"整千", 12000, uidlucky.TierB, uidlucky.ReasonTailTriple},
		{"六位尾部三位重复", 123666, uidlucky.TierB, uidlucky.ReasonTailTriple},

		{"尾部两位重复", 12388, uidlucky.TierC, uidlucky.ReasonTailDouble},
		{"整百", 12300, uidlucky.TierC, uidlucky.ReasonTailDouble},

		{"普通号", 12346, uidlucky.TierNone, uidlucky.ReasonNone},
		{"第二个自动分配的号", 10002, uidlucky.TierNone, uidlucky.ReasonNone},
		{"步长不为一不算顺子", 1357, uidlucky.TierNone, uidlucky.ReasonNone},
		{"隔位递增不算顺子", 1122, uidlucky.TierC, uidlucky.ReasonTailDouble},
		{"长度不足不参与判定", 999, uidlucky.TierNone, uidlucky.ReasonNone},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := uidlucky.Evaluate(c.uid)
			assert.Equal(t, c.tier, got.Tier, "tier of %d", c.uid)
			assert.Equal(t, c.reason, got.Reason, "reason of %d", c.uid)
			assert.Equal(t, c.tier != uidlucky.TierNone, got.IsLucky, "is_lucky of %d", c.uid)
		})
	}
}

func TestPriceTableNormalized(t *testing.T) {
	// 全零配置必须被默认值兜住，否则靓号会变成免费
	got := uidlucky.PriceTable{}.Normalized()
	assert.Equal(t, uidlucky.DefaultPriceTable, got)

	// 只缺一项时只补这一项，其余保留用户配置
	partial := uidlucky.PriceTable{S: 100, A: 0, B: 50, C: 0}.Normalized()
	assert.Equal(t, int64(100), partial.S)
	assert.Equal(t, uidlucky.DefaultPriceTable.A, partial.A)
	assert.Equal(t, int64(50), partial.B)
	assert.Equal(t, uidlucky.DefaultPriceTable.C, partial.C)

	// 负数也应当补成默认值，避免"倒贴积分"
	negative := uidlucky.PriceTable{S: -1, A: -1, B: -1, C: -1}.Normalized()
	assert.Equal(t, uidlucky.DefaultPriceTable, negative)
}

func TestPriceOf(t *testing.T) {
	prices := uidlucky.PriceTable{S: 8000, A: 3000, B: 1200, C: 400}

	assert.Equal(t, int64(8000), prices.PriceOf(uidlucky.TierS))
	assert.Equal(t, int64(3000), prices.PriceOf(uidlucky.TierA))
	assert.Equal(t, int64(1200), prices.PriceOf(uidlucky.TierB))
	assert.Equal(t, int64(400), prices.PriceOf(uidlucky.TierC))
	// 非靓号不可购买
	assert.Equal(t, int64(0), prices.PriceOf(uidlucky.TierNone))
}

func TestQuoteFor(t *testing.T) {
	prices := uidlucky.PriceTable{S: 8000, A: 3000, B: 1200, C: 400}

	t.Run("可购买", func(t *testing.T) {
		q := uidlucky.QuoteFor(88888, false, prices)
		assert.True(t, q.Available)
		assert.True(t, q.IsLucky)
		assert.Equal(t, uidlucky.TierS, q.Tier)
		assert.Equal(t, int64(8000), q.Price)
		assert.Empty(t, q.Message)
	})

	t.Run("已被占用", func(t *testing.T) {
		q := uidlucky.QuoteFor(88888, true, prices)
		assert.False(t, q.Available)
		assert.Equal(t, "uid_already_taken", q.Message)
		// 价格仍然报出来，前端可以展示"这个号值多少"
		assert.Equal(t, int64(8000), q.Price)
	})

	t.Run("非靓号不可购买", func(t *testing.T) {
		q := uidlucky.QuoteFor(12346, false, prices)
		assert.False(t, q.Available)
		assert.False(t, q.IsLucky)
		assert.Equal(t, int64(0), q.Price)
		assert.Equal(t, "uid_not_lucky", q.Message)
	})

	t.Run("占用优先于非靓号", func(t *testing.T) {
		// 同时满足两个拒绝条件时，先告诉用户"被占了"更有用
		q := uidlucky.QuoteFor(12346, true, prices)
		assert.False(t, q.Available)
		assert.Equal(t, "uid_already_taken", q.Message)
	})
}
