// Package uidlucky 判定一个业务编号（UID）的"靓号"等级。
//
// 这里只有纯函数：输入一个数字，输出它的等级和命中的规则。定价、权限、
// 占用检查都不在这里，因此可以脱离数据库单测，也方便以后调整评分规则。
package uidlucky

import (
	"strconv"
)

// Tier 是靓号等级，等级越高越贵。
type Tier string

const (
	// TierNone 表示不是靓号。
	TierNone Tier = ""
	// TierC 是入门级：尾部两位重复（如 12388）。
	TierC Tier = "c"
	// TierB 是进阶级：尾部三位重复（如 12888）。
	TierB Tier = "b"
	// TierA 是稀缺级：顺子（12345）或回文（12321）。
	TierA Tier = "a"
	// TierS 是顶级：所有位相同（88888）。
	TierS Tier = "s"
)

// Reason 说明命中了哪条规则，会展示给用户，让他知道钱花在哪。
type Reason string

const (
	ReasonNone       Reason = ""
	ReasonRepeated   Reason = "repeated"    // 全位相同
	ReasonSequential Reason = "sequential"  // 连续递增或递减
	ReasonPalindrome Reason = "palindrome"  // 回文
	ReasonTailTriple Reason = "tail_triple" // 尾部三位相同
	ReasonTailDouble Reason = "tail_double" // 尾部两位相同
)

// Evaluation 是一次判定的结果。
type Evaluation struct {
	Tier    Tier   `json:"tier"`
	Reason  Reason `json:"reason"`
	IsLucky bool   `json:"is_lucky"`
}

// minLength 是参与判定的最短长度。
//
// 四位以下不判：1000 以下的号要么在保留段，要么短到"尾两位重复"几乎是
// 常态，判成靓号只会制造噪音。
const minLength = 4

// minPalindromeLength 让回文规则比其它规则更严格。
//
// 起手编号 10001 恰好是回文，若回文从四位起算，1000/1001 这类号会被
// 大量误判。五位起算后正常号段里只有真正的对称号会命中。
const minPalindromeLength = 5

// Evaluate 判定 UID 的靓号等级。
//
// 规则按稀缺度从高到低短路匹配，一个号只会命中一条规则。
func Evaluate(uid uint64) Evaluation {
	s := strconv.FormatUint(uid, 10)
	n := len(s)
	if n < minLength {
		return Evaluation{}
	}

	if allSame(s) {
		return Evaluation{Tier: TierS, Reason: ReasonRepeated, IsLucky: true}
	}

	if isSequential(s) {
		return Evaluation{Tier: TierA, Reason: ReasonSequential, IsLucky: true}
	}

	if n >= minPalindromeLength && isPalindrome(s) {
		return Evaluation{Tier: TierA, Reason: ReasonPalindrome, IsLucky: true}
	}

	if n >= 5 && allSame(s[n-3:]) {
		return Evaluation{Tier: TierB, Reason: ReasonTailTriple, IsLucky: true}
	}

	if allSame(s[n-2:]) {
		return Evaluation{Tier: TierC, Reason: ReasonTailDouble, IsLucky: true}
	}

	return Evaluation{}
}

// allSame 判断字符串的每个字符是否都相同。
func allSame(s string) bool {
	if s == "" {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}

// isPalindrome 判断字符串是否左右对称。
func isPalindrome(s string) bool {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		if s[i] != s[j] {
			return false
		}
	}
	return true
}

// isSequential 判断是否为连续递增或递减的数字串（如 12345、9876）。
//
// 只认步长为 1：1122 这种"每两位递增"不算顺子，它更像普通号。
func isSequential(s string) bool {
	if len(s) < minLength {
		return false
	}

	asc, desc := true, true
	for i := 1; i < len(s); i++ {
		diff := int(s[i]) - int(s[i-1])
		if diff != 1 {
			asc = false
		}
		if diff != -1 {
			desc = false
		}
		if !asc && !desc {
			return false
		}
	}
	return asc || desc
}

// PriceTable 是各等级的价格表，单位与积分一致。
//
// 这里同时带 koanf tag，是为了让 config.RewardsConf 能直接把这个类型
// 嵌进配置结构体、由 koanf 完成解码，不必在 config 包里再写一个镜像结构
// 和一层转换。这个包本身不依赖配置，只是恰好被配置复用。
type PriceTable struct {
	S int64 `koanf:"s" json:"s"`
	A int64 `koanf:"a" json:"a"`
	B int64 `koanf:"b" json:"b"`
	C int64 `koanf:"c" json:"c"`
}

// DefaultPriceTable 是配置缺失时的兜底价格。
//
// 取值刻意让相邻等级差三倍左右：S 级要攒很久，C 级随手就能换一个，
// 这样兑换对普通用户才有意义。
var DefaultPriceTable = PriceTable{S: 8000, A: 3000, B: 1200, C: 400}

// Normalized 用默认值补齐非正数项，避免配置写 0 导致靓号免费。
func (p PriceTable) Normalized() PriceTable {
	out := p
	if out.S <= 0 {
		out.S = DefaultPriceTable.S
	}
	if out.A <= 0 {
		out.A = DefaultPriceTable.A
	}
	if out.B <= 0 {
		out.B = DefaultPriceTable.B
	}
	if out.C <= 0 {
		out.C = DefaultPriceTable.C
	}
	return out
}

// PriceOf 返回某等级的价格。非靓号返回 0，表示不可购买。
func (p PriceTable) PriceOf(t Tier) int64 {
	switch t {
	case TierS:
		return p.S
	case TierA:
		return p.A
	case TierB:
		return p.B
	case TierC:
		return p.C
	}
	return 0
}

// Quote 是对外报价的完整结果。
type Quote struct {
	UID       uint64 `json:"uid"`
	Available bool   `json:"available"`
	IsLucky   bool   `json:"is_lucky"`
	Tier      Tier   `json:"tier"`
	Reason    Reason `json:"reason"`
	Price     int64  `json:"price"`
	// Message 是给用户看的说明，例如"该号码已被占用"。
	Message string `json:"message,omitempty"`
}

// QuoteFor 生成一个号码的报价。occupied 由调用方查询数据库得到。
//
// 非靓号不允许购买：否则用户可以花同样的价钱把 UID 改成任意数字，
// 靓号定价就失去了意义。
func QuoteFor(uid uint64, occupied bool, prices PriceTable) Quote {
	eval := Evaluate(uid)
	q := Quote{
		UID:       uid,
		Available: !occupied && eval.IsLucky,
		IsLucky:   eval.IsLucky,
		Tier:      eval.Tier,
		Reason:    eval.Reason,
		Price:     prices.Normalized().PriceOf(eval.Tier),
	}

	switch {
	case occupied:
		q.Message = "uid_already_taken"
	case !eval.IsLucky:
		q.Message = "uid_not_lucky"
	}

	return q
}
