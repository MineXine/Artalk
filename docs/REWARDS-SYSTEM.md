# Artalk 徽章系统与 UID 系统 · 实现说明

> 基于 `MineXine/Artalk`（fork 自 `LiuShen-Fork/Artalk`），在原有评论系统上新增的三块能力：
> **积分账户**、**徽章系统**、**UID 靓号系统**。
> 所有代码已通过 `go build ./...`、`go vet` 与测试验证。

---

## 0. 先说明一个前提：为什么多了「积分」这一层

需求里说的是「通过积分兑换」和「用积分兑换靓号」，但原项目**完全没有积分概念**。
所以先补了一层最小可用的积分账户作为基础设施，否则「兑换」无从谈起。

积分只做三件事，不做更多：

| 能力 | 说明 |
|---|---|
| 入账 | 发评论、评论被赞、每日签到 |
| 出账 | 兑换徽章、兑换靓号 |
| 流水 | 每笔变动都落一条记录，含变动后余额快照 |

**没有做的**：等级、经验值、排行榜、积分过期、提现。这些都可以在现有结构上叠加，但当前需求用不到。

---

## 1. 数据模型

### 1.1 `users` 表新增两列

```go
// internal/entity/user.go
UID    uint64 `gorm:"not null;default:0"` // 业务编号（门牌号），从 10001 起
Points int64  `gorm:"not null;default:0"` // 积分余额
```

**为什么 UID 不直接复用主键 `id`？**
主键被 `comments.user_id`、`notifies.user_id`、`votes.user_id`、`auth_identities.user_id` 引用。
如果把主键当门牌号出售，兑换靓号就得改主键，进而要级联改四张表。所以拆成两个值：

- `id` — 内部主键，永不变，从 1 开始
- `uid` — 对外门牌号，可从 10001 起分配，可兑换

### 1.2 四张新表

**`badge_categories`（徽章分类）**

| 列 | 说明 |
|---|---|
| `key` | 稳定标识，kebab-case，创建后不可改 |
| `name` | 展示名，如「成长足迹」 |
| `sort_order` | 徽章墙上的分组顺序 |
| `enabled` | 停用后不再出现在前台的分类里；管理端仍可见 |

**`badges`（徽章库）**

| 列 | 说明 |
|---|---|
| `key` | 唯一标识，kebab-case，创建后不可改（外部引用它） |
| `name` / `color` / `icon` / `description` | 展示信息（icon 接受 emoji 或图片 URL） |
| `category_id` | 所属分类，`0` 表示未分类 |
| `grant_type` | `condition` / `exchange` / `manual` |
| `condition_kind` | `comment_count` / `vote_received` / `register_days` / `points` / `none` |
| `condition_value` | 阈值 |
| `price` | 兑换所需积分，0 表示非卖品 |
| `sort_order` / `enabled` | 排序与上下架 |

**`user_badges`（持有关系）**

```go
UserID  uint `gorm:"uniqueIndex:idx_user_badge_pair,priority:1"`
BadgeID uint `gorm:"uniqueIndex:idx_user_badge_pair,priority:2"`
Source   BadgeSource // condition / exchange / manual / import
Equipped bool        // 是否佩戴中
GrantedAt time.Time
```

`(user_id, badge_id)` 唯一索引让**重复发放天然幂等**——并发发放时后到的那次拿到唯一约束错误，代码把它当作「已持有」处理。

**`point_transactions`（积分流水）**

```go
UserID  uint
Amount  int64  // 正数入账，负数出账
Balance int64  // 变动后余额快照
Reason  string // comment_create / vote_received / checkin / badge_exchange / uid_exchange / admin_adjust / refund
RefType string // comment / vote / badge / uid / checkin / admin
RefID   uint
DedupKey string // 业务去重键（见 3.2）
Note     string
```

`Balance` 存快照是为了**对账不必重放历史**：对不上时直接看最后一行的 `Balance` 与 `users.points` 是否一致。

---

## 2. 与现有代码的兼容策略

这是本次改造最需要小心的地方。原项目已经有一个「单徽章」字段：

```go
// users 表
BadgeName  string
BadgeColor string
```

它被 `CommentDTO`、`UserDTO`、`AdminUserDTO`、`artransfer` 导入导出、`conf_sync` 配置同步、前端各处引用。

**做法：把它保留为「当前佩戴徽章的冗余快照」。**

```
佩戴动作 → 事务内写 user_badges.equipped = true
          → 同时把 badges.name/color 复制到 users.badge_name/badge_color
```

好处：
- 所有读 `BadgeName` 的地方（评论列表、邮件模板、导出）**零改动**就能显示佩戴的徽章
- 老数据、老前端、老 API 消费者全部不受影响
- 卸下或收回徽章时清空这两列，语义保持一致

代价：两处数据可能不一致。所以所有写路径都走 `dao.EquipBadge` / `dao.UnequipBadge` / `dao.RevokeBadge` 三个方法，**没有一个地方直接改 `BadgeName`**（管理后台的用户编辑接口除外，那是管理员显式覆盖，属于预期行为）。

---

## 3. 关键设计决策

### 3.1 UID 分配：为什么用 `MAX+1` 而不是序列

四种数据库（sqlite / mysql / postgres / sqlserver）的「序列 / 自增起始值 / RETURNING」语法各不相同，写一套通用代码要大量方言分支。

所以选最朴素的方案：

```go
next := MAX(uid) + 1        // 候选值
if next < 10001 { next = 10001 }
Create(user)                 // 撞唯一索引就重试，最多 3 次
```

- **并发安全**由 `users.uid` 唯一索引保证，不靠行锁
- 冲突时重试 3 次（`MaxUIDExchangeRetries`），足够吸收并发插入的时序差
- 兑换靓号用 `ChangeUserUID`：先 `COUNT` 查占用给出可读错误，真正的并发保证仍来自唯一索引

### 3.2 防刷：为什么点赞要带去重键

积分可以被刷，最直接的路径是「点赞 → 取消 → 再点赞」。

在 `updateVote` 里，`selectedChoice == "up"` 只说明「这次产生了一条 up 投票」，不说明「这个人第一次赞这条评论」——用户可以无限循环。

所以给流水加了 `DedupKey`：

```go
dedupKey = "vote:{commentID}:{voterKey}"
voterKey = "u:{userID}"  或  "ip:{md5(ip)}"   // 匿名投票退化到 IP 哈希，不明文存 IP
```

`(user_id, reason, dedup_key)` 上建了复合索引，入账前先查重。

同类思路也用在签到：`dedupKey = "checkin:{YYYY-MM-DD}"`，两个并发请求同时通过「今天是否已签到」的检查时，只有一条流水能落库。

### 3.3 兑换：为什么用「先扣后退」而不是单事务

徽章兑换 = 扣积分 + 发放徽章，是两个 DAO 操作。

做成单事务要引入 `AdjustPointsTx` / `GrantBadgeTx` 两套带 `tx` 的变体，让 DAO 层膨胀。

选择补偿式：

```go
扣分 → 发放徽章 → 发放失败则退回积分（记 refund 流水）
```

安全性来自 `GrantBadge` 的幂等性——万一补偿也失败，重试不会造成重复发放。
**退款单独用 `refund` reason**，而不是记一笔负数冲正，这样流水里「花出去又退回来」一眼可辨。

靓号兑换同理：`先扣分 → 再改号 → 改号失败退分`。顺序不能反，否则会在积分不足时留下一个「已经改好但没付钱」的号码。

### 3.4 条件徽章：为什么要先查有没有候选

`EvaluateBadgesForUser` 在每次发评论、每次被赞时都会调用。

如果无脑执行，每次评论都要跑两个聚合查询（评论数、获赞数），对高频站点是浪费。

所以先做一次「有没有待达成的条件徽章」判断：

```go
badges := d.FindAllBadges(false)            // 1 次查询
if 没有任何 condition 类徽章 → return nil   // 零成本
if 用户已持有全部 condition 徽章 → return nil
// 只有确实存在待判定项，才去查统计
```

徽章库通常只有几十条，这个前置判断是廉价的。

### 3.5 异步路径必须容忍应用关闭

积分结算跑在评论创建的后台 goroutine 里。测试中发现一个真实的竞态：

```
测试 cleanup → app.ResetBootstrapState() → app.dao = nil
                                              ↑
后台 goroutine 仍在跑 → app.Dao() 返回 nil → 解引用 panic
```

修复：所有异步入口先判空取 dao，取不到就跳过结算。

```go
d := s.app.Dao()
if d == nil { return }   // 应用正在关闭
```

这比加 `recover()` 干净——关闭时跳过积分结算本来就是正确行为。

---

## 4. 配置

```yaml
rewards:
  enabled: false          # 总开关，默认关闭

  points:
    comment_create: 5             # 发一条评论得 5 分
    comment_create_daily_cap: 30  # 每天靠评论最多拿 30 分
    vote_received: 1              # 评论每被赞一次得 1 分
    checkin: 10                   # 每日签到得 10 分

  badges:
    enabled: false
    max_equipped: 1               # 预留：当前只支持佩戴一枚

  uid:
    enabled: false
    price:                        # 各等级靓号价格（积分）
      s: 8000                     # 全位相同，如 88888
      a: 3000                     # 顺子 12345 / 回文 12321
      b: 1200                     # 尾部三位相同，如 12888
      c: 400                      # 尾部两位相同，如 12388
```

**默认值有三处兜底**（`config/rewards.go` 的 `Normalized()`）：

1. 任一项填 0 或缺省 → 用默认值。否则「价格 0」会让靓号免费，「积分 0」会让签到无意义。
2. 总开关打开但两个子开关都没写 → 两个都开启。因为 `bool` 没有 nil 状态，代码无法区分「没写」和「写了 false」。
3. `security.action` 依赖 `captcha.enabled` 的联动检查同理，已有先例。

环境变量同样可用（已补进 `EnvPathMapCache`）：

```bash
ATK_REWARDS_ENABLED=true
ATK_REWARDS_POINTS_CHECKIN=20
ATK_REWARDS_UID_PRICE_S=12000
```

---

## 5. API 清单

### 5.1 用户侧

| 方法 | 路径 | 鉴权 | 作用 |
|---|---|---|---|
| `GET` | `/api/v2/points` | 登录 | 余额 + 今日签到状态 + 流水 + 当前规则 |
| `POST` | `/api/v2/points/checkin` | 登录 | 每日签到（幂等） |
| `GET` | `/api/v2/badges` | 公开 | 徽章墙（含持有状态与达成进度） |
| `POST` | `/api/v2/badges/:id/exchange` | 登录 | 用积分兑换徽章 |
| `POST` | `/api/v2/badges/:id/equip` | 登录 | 佩戴徽章（自动卸下旧的） |
| `DELETE` | `/api/v2/badges/equip` | 登录 | 卸下徽章 |
| `GET` | `/api/v2/uid/quote?uid=88888` | 公开 | 查询某号码是否为靓号、多少钱、是否可买 |
| `POST` | `/api/v2/uid/exchange` | 登录 | 用积分兑换靓号 |

> 徽章墙与靓号报价允许匿名访问：知道「有什么可以拿」和「一个号多少钱」不敏感，
> 而且未注册访客先看到价格再决定注册，转化路径更短。

### 5.2 管理侧

| 方法 | 路径 | 作用 |
|---|---|---|
| `GET` | `/api/v2/badges/all` | 徽章库（含已停用）+ 分类列表 |
| `POST` | `/api/v2/badges` | 创建徽章 |
| `PUT` | `/api/v2/badges/:id` | 更新徽章（`key` 不可改） |
| `DELETE` | `/api/v2/badges/:id` | 删除徽章并清理所有持有关系 |
| `POST` | `/api/v2/badges/seed-defaults` | 导入内置徽章（幂等） |
| `GET` | `/api/v2/badge-categories` | 分类列表（含已停用） |
| `POST` | `/api/v2/badge-categories` | 创建分类 |
| `PUT` | `/api/v2/badge-categories/:id` | 更新分类（`key` 不可改） |
| `DELETE` | `/api/v2/badge-categories/:id` | 删除分类，旗下徽章退回未分类 |
| `POST` | `/api/v2/users/:id/points` | 调整用户积分（正数发放，负数扣除） |
| `GET` | `/api/v2/users/:id/badges` | 查看某用户的徽章 |
| `POST` | `/api/v2/users/:id/badges/:badgeId` | 授予徽章 |
| `DELETE` | `/api/v2/users/:id/badges/:badgeId` | 收回徽章 |

#### 内置徽章

`POST /badges/seed-defaults` 导入一套开箱可用的配置，共 **3 个分类 10 枚徽章**：

| 分类 | 徽章 | 获取方式 |
|---|---|---|
| 成长足迹 | 初来乍到 🌱 / 活跃分子 💬 / 评论老手 🚀 / 元老 🏛️ | 评论 1、10、100 条；注册 365 天 |
| 人气荣誉 | 小有人气 ⭐ / 万众瞩目 🔥 / 众望所归 👑 | 累计获赞 10、100、500 |
| 特殊收藏 | 富甲一方 💰 / 收藏家 🎖️ / 创始人 🏵️ | 1000 积分兑换；管理员授予 ×2 |

图标用 emoji 而不是图片：不需要静态资源，任何主题下都能渲染。`icon` 字段同时接受图片 URL，想换成自制图标直接填地址即可。

导入按 `key` 判断，已存在就跳过——管理员改过阈值或名字之后重复导入不会覆盖他的调整。

> 刻意**不做**迁移时自动导入：空库是合法状态，静默塞进去会让"我明明清空了"的人困惑。管理端空库时会显示一个导入按钮。

### 5.3 响应示例

`GET /api/v2/uid/quote?uid=88888`

```json
{
  "uid": 88888,
  "available": true,
  "is_lucky": true,
  "tier": "s",
  "reason": "repeated",
  "price": 8000,
  "current_uid": 10005,
  "points": 200,
  "is_login": true
}
```

`message` 字段只在不可购买时出现：`uid_already_taken`（被占）或 `uid_not_lucky`（不是靓号）。
**两者同时成立时报「被占」**——对用户更有用的信息优先。

---

## 6. 靓号规则

纯函数在 `internal/uidlucky`，与数据库、配置完全解耦，可独立单测。

| 等级 | 规则 | 例子 |
|---|---|---|
| S | 所有位相同（≥4 位） | `88888`、`7777` |
| A | 严格连续递增或递减（≥4 位） | `12345`、`54321` |
| A | 回文（≥5 位） | `12321`、`10001` |
| B | 尾部三位相同 | `12888`、`12000` |
| C | 尾部两位相同 | `12388`、`12300` |

规则按稀缺度**从高到低短路匹配**，一个号只命中一条。

两个刻意的边界：

- **长度不足 4 位不参与判定**：`1000` 以下的号要么在保留段，要么「尾两位重复」几乎是常态。
- **回文要求 5 位起**：起手编号 `10001` 恰好是回文，若回文从 4 位起算，`1000`/`1001` 这类号会被大量误判。

> 副作用：第一个注册的用户会白拿一个 A 级回文号 `10001`。这是「先到先得」的合理结果——
> 定价只在**主动兑换**时生效，被动分配不收费。

---

## 7. 前端接入

### 7.1 类型层

`ui/artalk/src/types/data.ts`（widget 与 sidebar 共享）：

```ts
export interface UserData {
  id: number
  uid?: number      // 业务编号
  points?: number   // 积分，仅本人可见
  ...
}
```

`ui/artalk-sidebar/src/lib/api.ts` 新增 `rewardsApi`，共 14 个方法。
**不依赖生成的 swagger client**：后台有通用 `api<T>(path, options)` 通道，
新增接口不必重新跑 `make update-swagger` 就能用。

### 7.2 页面

| 位置 | 改动 |
|---|---|
| 用户列表 | 用户名后显示 `#UID`；评论数列下方显示 `123p`（积分） |
| 账户页 | 顶部新增身份摘要条：`UID #10005 · 积分 320` |
| `/badges`（管理视角） | 徽章库：按分类分组、分类增删改、徽章 CRUD、空库一键导入内置徽章 |
| `/badges`（用户视角） | **我的徽章**：UID / 积分 / 签到 + 按分类分组的徽章墙 |
| 评论列表 | 佩戴的徽章通过 `badge_name` / `badge_color` 直接显示在昵称旁（原有能力，无需改动） |

**一个路由两种形态**：`/badges` 在管理视角是徽章库，在用户视角是"我的徽章"，由 `session.view` 决定渲染哪个组件。导航项通过 `userTitleKey` 自动切换标题，权限判断用 `requireAdminOrVisitor()`——管理视角要求管理员，用户视角对所有登录用户开放。

我的徽章页的三类卡片状态：

| 状态 | 展示与操作 |
|---|---|
| 已获得 | 彩色图标 + 「佩戴」/「卸下」 |
| 未获得 · 条件型 | 灰度图标 + 进度条（`进度 / 阈值`）+ 达标提示 |
| 未获得 · 兑换型 | 灰度图标 + 价格按钮，积分不足时禁用 |
| 未获得 · 手动型 | 灰度图标 + 「由管理员授予」 |

导航里 `badges` 归入 `community` 组，与「用户」相邻——它回答的是同一类问题：
用户持有徽章，管理员定义什么徽章可被持有。

### 7.3 文案

新增 24 个词条，9 个语言文件全部补齐。
英文是 `MessageSchema` 的唯一真相（`i18n.ts` 只有 `type MessageSchema = typeof en`），
其余语言文件声明为 `MessageSchema`，缺 key 会直接导致 tsc 报错——这个约束保证了「加词条必须翻译」。

---

## 8. 迁移

### 8.1 三步走，顺序不可换

```
1. 加列     ALTER TABLE <prefix>users ADD COLUMN uid BIGINT NOT NULL DEFAULT 0
            （裸 SQL，避开 GORM 把字段 tag 上的约束一并带上）
2. 回填     所有 uid = 0 的行，从 MAX(uid)+1 开始连续分配，起点 10001
            （用 Unscoped，软删除的行也要给号，否则它们共享 0 会挡住唯一索引）
3. 建索引   CREATE UNIQUE INDEX ... ON users (uid)
```

**索引必须在 AutoMigrate 之后建**：全新库在第 1 步时表还不存在。
这个顺序问题是实测发现的——最初把建索引写在回填后面，
结果新部署的库压根没有唯一索引。

### 8.2 全新库 vs 老库

| 场景 | 行为 |
|---|---|
| 全新部署 | `migrateUserUID` 直接返回；`AutoMigrate` 建表；随后补建唯一索引 |
| 老库升级 | 加列 → 回填（按 id 升序，编号不跳号）→ AutoMigrate → 建索引 |
| 重复执行 | 回填只处理 `uid = 0` 的行，已分配的号不会被打乱 |
| 导入数据（artransfer） | 走 `CreateUser`，自动分配；若导入文件带了 UID 则保留 |

### 8.3 一个必须知道的约束

`users.uid` 有唯一索引后，**任何绕过 `CreateUser` 的写入都必须自己给 UID**。

本次因此改了两处：
- `test/fixtures/atk_users.yml`：三个种子用户补上 `uid: 10001/10002/10003`
- `server/handler/account_flow_test.go`：从 `DB().Create(&user)` 改为 `CreateUser(&user)`

如果你写新的导入脚本或用 SQL 直接灌数据，请记得这一点。

---

## 9. 测试

新增 3 个文件：

| 文件 | 覆盖 |
|---|---|
| `internal/uidlucky/uidlucky_test.go` | 18 个靓号分级用例（含边界）、价格表默认值兜底、报价三态 |
| `server/handler/rewards_test.go` | 5 个端到端流程（见下） |
| （改动）`server/handler/account_flow_test.go` | 改走 CreateUser |

`rewards_test.go` 的五个场景：

1. **UID 分配** — 起点 10001、连续递增、幂等（同名同邮箱再查不重发）、全库不重复
2. **徽章兑换** — 建库 → 权限（非管理员 403）→ 积分不足 400 → 补分 → 兑换成功 → 重复兑换 400 → 佩戴同步 `badge_name` → 幂等 → 卸下清空 → 未持有不能佩戴
3. **条件徽章** — 达标前不发 → 造评论 → 自动发放 → 再判不重复发
4. **签到幂等** — 首次成功 → 二次 no-op → 概览标记已签到
5. **靓号兑换** — 匿名可查价 → 普通号不可买 → 被占号不可买 → 积分不足不动 UID → 补分后兑换成功 → 旧号释放
6. **内置徽章导入** — 首次导入 3 分类 10 徽章 → 再次导入零新增（幂等）→ 匿名可见全墙 → 每枚都有归属分类 → 分类按 sort_order 排列
7. **分类 CRUD** — 建分类 → key 重复被拒 → 徽章挂到分类下 → 指向不存在分类被拒 → 删分类后徽章退回未分类且本身还在

---

## 10. 已知限制

| 限制 | 原因 | 影响 |
|---|---|---|
| 限频与签到上限是「每进程」的 | 积分/风控状态不落 Redis | 多副本部署时实际额度 = 配置 × 副本数 |
| 一次只能佩戴一枚徽章 | `MaxEquipped` 只是预留字段 | 想展示多枚需要改 `EquipBadge` 与前端 |
| 徽章硬删除 | 软删除会让 `key` 唯一索引无法复用 | 误删无法恢复，需重建 |
| 靓号只能「换」，不能「买断保留」 | 旧号立即释放 | 换走的号别人可以再买 |
| 条件徽章只做阈值判定 | 没有加权、组合、时间窗口条件 | 复杂规则需要扩展 `BadgeConditionKind` |
| 非中英繁的 6 种语言，本轮新增词条为英文占位 | 翻译量优先让位于功能 | 界面可用，文案待补 |

---

## 11. 扩展点

想继续加东西时，按这些位置下手：

**加一种积分来源**
1. `entity.PointReason` 加枚举
2. 在对应业务点调 `rewardsService.AddPoints(...)`
3. `config/rewards.go` 加配置项 + `Normalized()` 默认值
4. `make update-conf` 重新生成配置模板

**加一种徽章条件**
1. `entity.BadgeConditionKind` 加枚举 + `Valid()` 放行
2. `entity.Badge.IsConditionMet` 加分支
3. `entity.BadgeStat` 加字段
4. `core.RewardsService.BadgeStat` 填充新字段
5. `core.RewardsService.conditionProgress` 加映射（前端进度条用）
6. `ui/artalk-sidebar/src/lib/api.ts` 的 `BadgeConditionKind` 联合类型同步

**加靓号规则**
只在 `internal/uidlucky/uidlucky.go` 的 `Evaluate` 里加分支，按稀缺度插入到合适位置。
纯函数，改完跑单测即可，不碰数据库。

---

## 附：文件清单

```
新增
  internal/entity/rewards.go              BadgeCategory / Badge / UserBadge / PointTransaction + 常量
  internal/config/rewards.go              RewardsConf + 默认值兜底
  internal/uidlucky/uidlucky.go           靓号判定与定价（纯函数）
  internal/uidlucky/uidlucky_test.go
  internal/dao/rewards.go                 徽章与分类 CRUD / 积分原子增减 / 统计
  internal/dao/uid.go                     UID 分配、回填、唯一索引、换号
  internal/core/service_rewards.go        编排：积分结算、条件发放、兑换、报价、徽章墙
  internal/core/rewards_defaults.go       内置 3 分类 + 10 枚徽章的声明与幂等导入
  server/handler/points.go                GET /points, POST /points/checkin
  server/handler/badge.go                 GET /badges, exchange, equip, unequip
  server/handler/badge_admin.go           徽章 CRUD + seed + 用户积分/徽章管理
  server/handler/badge_category.go        分类 CRUD
  server/handler/uid.go                   GET /uid/quote, POST /uid/exchange
  server/handler/rewards_test.go
  ui/artalk-sidebar/src/pages/badges.tsx  徽章库管理（分类分组 + CRUD + 导入）
  ui/artalk-sidebar/src/pages/my-badges.tsx   用户侧徽章墙（佩戴 / 进度 / 兑换 / 签到）
  ui/artalk-sidebar/src/lib/rewards-labels.ts 枚举文案与图标判定的共享模块

修改
  internal/entity/user.go                 +UID +Points
  internal/entity/user_dto.go             +uid +points
  internal/entity/comment_dto.go          +uid
  internal/config/config.go               +Rewards 配置组
  internal/config/base.go                 normalPatch 里的默认值与联动
  internal/config/cache.go                环境变量映射（建议用 make update-conf 重新生成）
  internal/dao/query_new.go               CreateUser 分配 UID + 冲突重试
  internal/dao/dto.go                     DTO 填充 uid/points
  internal/dao/migrate.go                 迁移顺序
  internal/core/base.go                   注入 RewardsService
  server/server.go                        16 条路由
  server/handler/comment_create.go        评论积分结算
  server/handler/vote.go                  updateVote 返回 newUp + 积分结算
  server/handler/user_info.go             匿名视图带 uid
  server/handler/account_flow_test.go     改用 CreateUser
  test/fixtures/atk_users.yml             种子用户补 uid
  docs/swagger/*                          重新生成
  ui/artalk/src/types/data.ts             UserData +uid +points
  ui/artalk-sidebar/src/lib/api.ts        rewardsApi + 类型
  ui/artalk-sidebar/src/lib/account-api.ts
  ui/artalk-sidebar/src/app/nav.ts        +badges 导航
  ui/artalk-sidebar/src/router.tsx        +/badges 路由
  ui/artalk-sidebar/src/pages/users.tsx   UID + 积分列
  ui/artalk-sidebar/src/pages/account.tsx 身份摘要条
  ui/artalk-sidebar/src/i18n*.ts          24 个词条 × 9 语言
```
