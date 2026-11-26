# Artalk 架构与代码写法全解

> 分析对象：`https://github.com/LiuShen-Fork/Artalk`（已 fork 至 `MineXine/Artalk`）
> 版本：`v2.10.1` ｜ 分析时间：2026-11-22 ｜ 代码快照：`master` @ `db22679`
>
> **后续扩展**：本仓库已在原架构上新增「积分 / 徽章 / UID 靓号」三套能力，
> 实现细节与设计取舍见 **[Artalk-徽章与UID系统-实现说明.md](./Artalk-徽章与UID系统-实现说明.md)**。
> 本文档描述的是扩展前的原始架构，两者不冲突。

---

## 0. 先看清这个项目是什么

Artalk 是一个 **Go 编写的自托管评论系统**：后端提供 HTTP API + 静态资源托管，前端是一个可嵌入任意博客的评论组件，另带一套管理后台。

它和常见「前后端分离 SPA」最大的不同是：**前端产物会被打进 Go 二进制**（`go:embed`），最终交付物是**单文件可执行程序**。

### 0.1 fork 现状（重要）

```
ArtalkJS/Artalk (上游)  ←──── ArtalkJS/Artalk/master
                              ↑
                              │ 146 commits ahead / 32 behind（已分叉）
                              │
                       LiuShen-Fork/Artalk  ←──── 你 fork 的源头
                              ↑
                              │
                       MineXine/Artalk      ←──── 已 fork 到这里
```

对比上游 `master`：`{"ahead":146,"behind":32,"status":"diverged"}`

这个 fork **不是简单的镜像**，它在原版基础上加了一整条独立的演进线，主要增量集中在：

| 增量方向 | 具体内容 |
|---|---|
| **安全防护体系** | 全站限频中间件、IP 封禁（含 CIDR）、自动封禁、安全事件报表、可视化封禁管理 |
| **内容审核** | 审核日志表、AI 审核（多协议）、关键词拦截（同步阻断）、腾讯云/阿里云内容安全 |
| **AI 评论助手** | 一个会读页面正文、按父级评论链上下文回复的机器人（`清羽酱`） |
| **身份体系重构** | `AuthIdentity` 表、多身份绑定/解绑、账号合并、SSO token 交换、访客凭证 |
| **管理后台重写** | `artalk-sidebar` 从 Vue 迁移到 **React 19 + HeroUI**，13 个页面 |
| **工程化** | 大量以「行为场景」命名的集成测试、per-app 独立测试数据库、CI 加固 |

提交信息风格统一为 Conventional Commits + **完整英文陈述句**（例：`fix(comments): explain rejections without leaking the rules`），值得借鉴。

### 0.2 代码规模

| 维度 | 数量 |
|---|---|
| Go 源文件 | 447 个（含 181 个 `*_test.go`） |
| Go 总行数 | 53,091（含 swagger 生成文件 7,352 行） |
| **Go 业务代码（排除测试与生成）** | **26,655 行** |
| Go 测试代码 | 约 19,000 行 |
| TS/TSX 源文件 | 333 个 |
| HTTP 路由（`@Router` 注解） | 71 个 |
| 配置项（`koanf` tag） | 343 个 |
| `internal/` 子包 | 22 个 |

**测试密度值得单独提一句**：181 个测试文件对应 266 个非测试文件，测试代码量约为业务代码的 70%。且测试命名几乎全是**行为场景**而非「被测文件名」：`guest_security_test.go`、`comment_write_failure_test.go`、`auth_email_duplicate_test.go`、`database_compatibility_test.go`、`notification_security_test.go`。这是「每个 bug 修复都补一条回归测试」的长期积累结果。

---

## 1. 全局架构

### 1.1 三层拓扑

```
┌─────────────────────────────────────────────────────────────────────┐
│  宿主博客页面（任意静态站）                                            │
│                                                                     │
│   <script src="/dist/Artalk.js">   ← UMD，暴露 window.Artalk         │
│   <link href="/dist/Artalk.css">                                     │
│   Artalk.init({ el, server, site, pageKey })                         │
└───────────────┬─────────────────────────────────────────────────────┘
                │ 纯 DOM 渲染，零框架依赖
                │
                │  ① GET  /api/v2/conf          拉远端配置
                │  ② GET  /api/v2/comments      拉评论列表
                │  ③ POST /api/v2/comments      发评论
                ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Artalk 服务端（单个 Go 二进制）                                       │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ HTTP 层  fiber v2                                             │   │
│  │  reqID → logger → cors → actionLimit → [Guard 安全网关]        │   │
│  │  → 71 个 handler → common.RespSuccess / RespError             │   │
│  └──────────────────────────┬───────────────────────────────────┘   │
│                             │                                       │
│  ┌──────────────────────────▼───────────────────────────────────┐   │
│  │ 应用层  core.App（服务容器）                                    │   │
│  │  EmailService                                             │   │
│  │  IPRegionService                                          │   │
│  │  NotifyService                                            │   │
│  │  AntiSpamService                                          │   │
│  │  AIAssistantService                                       │   │
│  │  SecurityService                                          │   │
│  └──────────────────────────┬───────────────────────────────────┘   │
│                             │                                       │
│  ┌──────────────────────────▼───────────────────────────────────┐   │
│  │ 数据层  dao.Dao（GORM）                                        │   │
│  │  FindComment / FindUser / QueryDBWithCache[T]                │   │
│  │  ↓ 可选缓存适配器（bigcache / redis / memcache）                │   │
│  └──────────────────────────┬───────────────────────────────────┘   │
│                             │                                       │
│  ┌──────────────────────────▼───────────────────────────────────┐   │
│  │ 存储  sqlite / mysql / postgres / sqlserver                    │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  内嵌静态资源（go:embed → internal/pkged）                            │
│   /dist/Artalk.js  /dist/i18n/*.js  /dist/plugins/*.js  /sidebar/    │
└─────────────────────────────────────────────────────────────────────┘
                ▲
                │ iframe（/sidebar/）
                │
┌───────────────┴─────────────────────────────────────────────────────┐
│  管理后台  React 19 SPA（挂在 /sidebar/，base: './'）                  │
│  复用 widget 的 headless 实例调 API，甚至「收养」评论列表 DOM            │
└─────────────────────────────────────────────────────────────────────┘
```

### 1.2 后端目录职责

```
cmd/                    CLI 入口（cobra），8 个子命令
├─ base.go              根命令 + 启动前置（解析 flag、切工作目录、建 App）
├─ server.go            artalk server
├─ admin.go             artalk admin（交互式建管理员）
├─ export.go/import.go  artalk export / import（Artransfer 数据搬运）
├─ gen.go               artalk gen config（生成配置模板）
└─ upgrade.go           artalk upgrade（selfupdate 自更新）

internal/               业务内核（不依赖 HTTP）
├─ config/              343 个配置项 + 模板生成 + 元数据（代码即文档）
├─ core/                App 容器 + 6 个 Service + Hook 事件
├─ dao/                 15 个源文件 + 20 个测试，GORM 查询 + 缓存 + 迁移
├─ entity/              19 个文件，GORM 模型 + DTO
├─ db/                  多数据库方言适配
├─ auth/                社交登录（goth）+ Gitea/Okta 私有 provider
├─ security/            纯内存限频/封禁索引算法
├─ anti_spam/           5 种审核 checker + 同步拦截
├─ captcha/             6 种验证码 provider
├─ cloud/               腾讯云/阿里云手写签名 SDK
├─ limiter/             两套限流器（验证码触发 / 通用固定窗口）
├─ cache/               gocache 封装 + simple_cache
├─ artransfer/          导出/导入格式（全字段 string 的 Artran）
├─ hook/                泛型事件总线
├─ i18n/                go:generate 提取代码里的字符串
├─ notify_pusher/       多通道通知（邮件/钉钉/飞书/Telegram/Bark...）
├─ email/               邮件发送（gomail / 阿里云）
├─ template/            Go html/template 渲染
├─ ip_region/           ip2region 离线库
├─ uidlucky/            （新增）靓号等级判定与定价，纯函数
├─ pkged/               go:embed 虚拟文件系统
├─ log/                 zap 封装
├─ sync/                ⚠️ 名字误导：实际是泛型 KeyMutex
└─ utils/               工具函数

server/                 HTTP 层（依赖 internal）
├─ server.go            fiber 装配 + 71 个路由注册
├─ common/              响应封装、JWT、参数解析、IP 解析、验证码守卫
├─ handler/             76 个源文件（其中 62 个含路由注解），一文件一接口或一组
└─ middleware/          cors / limiter / security(Guard)

ui/                     前端 pnpm monorepo
docs/                   vitepress 文档 + landing + swagger UI
test/                   测试脚手架（sqlite + testfixtures）
```

### 1.3 依赖方向铁律

```
cmd  →  server  →  internal/core  →  internal/dao  →  internal/db  →  DB
                       ↓
                  internal/*（叶子能力包）

ui/artalk（内核）  ←──  ui/plugin-*（插件，peer dep）
ui/artalk          ←──  ui/artalk-sidebar（后台）
ui/plugin-kit      ←──  所有 plugin-*（仅 devDependency，构建期）
```

`internal/` **不导入 `server/`**，这是它能被 CLI 命令（export/import/admin）直接复用的前提。

---

## 2. 启动链路：一次 `artalk server` 到底发生了什么

这是理解全局的最佳切入点。按代码顺序：

### 2.1 `main.go` → `cmd.New()`

```go
// cmd/base.go:50-85
func New() *ArtalkCmd {
    cmd := &ArtalkCmd{RootCmd: &cobra.Command{...}}

    // ① 提前解析 flag —— 因为后面要立刻用 cfgFile / workDir
    cmd.eagerParseFlags()

    // ② 在 chdir 之前解析配置文件路径
    //    否则相对路径 -c ./artalk.yml 会以 data 目录为基准，找不到
    if cfgFile, err := resolveConfigFileBeforeDataDir(cmd.cfgFile, cmd.workDir); err == nil {
        cmd.cfgFile = cfgFile
    }

    // ③ 初始化数据目录并 chdir 进去
    if workDir, err := initDataDir(cmd.workDir); err == nil {
        cmd.workDir = workDir
    }

    return cmd
}
```

**关键设计点**：cobra 的 `PreRun` 通常是懒执行的，但 Artalk 需要**在解析子命令之前就拿到配置**（因为 `App` 要用它）。所以有了 `eagerParseFlags()` —— 直接 `ParseFlags(os.Args[1:])`。

数据目录解析优先级（`initDataDir`）：
1. `-w` 显式指定
2. `./data` 存在 → 就地不动（向后兼容老版本）
3. `config.RetrieveDataDir()` → `~/.local/share/artalk` 或 `/var/lib/artalk`
4. 都没有 → 创建 `XDG_DATA_HOME/artalk` 并 chdir

### 2.2 命令挂载与 PreRun 拦截

```go
// cmd/base.go:117-146
func (atk *ArtalkCmd) addCommand(cmd *cobra.Command) {
    originalPreRunFunc := cmd.PreRun
    cmd.PreRun = func(cmd *cobra.Command, args []string) {
        // MINI_BOOT 命令跳过整个 App 初始化
        if cmd.Annotations[BootModeKey] != string(MODE_MINI_BOOT) {
            config, err := getConfig(atk.cfgFile)
            atk.App = core.NewApp(config)
            if err := atk.App.Bootstrap(); err != nil { panic(err) }
        }
        if originalPreRunFunc != nil { originalPreRunFunc(cmd, args) }
    }
    atk.RootCmd.AddCommand(cmd)
}
```

`BootMode` 注解是个很实用的模式：`gen` / `config` / `version` 这类命令**不需要数据库**，标记 `MODE_MINI_BOOT` 后跳过 `Bootstrap()`，避免「想生成配置文件却因为没有数据库而报错」。

### 2.3 `core.App.Bootstrap()` 的 7 个阶段

```go
// internal/core/base.go:100-159
func (app *App) Bootstrap() error {
    mutex.Lock()          // 全局锁：Bootstrap 不可并发
    defer mutex.Unlock()

    // ① 时区 —— time.Local 是进程级的，必须在任何服务启动前设好
    processTimezone.init(app.Conf().TimeZone, func(l *time.Location) { time.Local = l })

    // ② i18n —— 从内嵌 FS 读 i18n/xx.yml
    app.initI18n()

    // ③ 日志 —— zap + lumberjack 滚动
    log.Init(log.Options{IsDiscard: !conf.Log.Enabled, IsDebug: conf.Debug, LogFile: conf.Log.Filename})

    // ④ DAO —— 建 GORM 连接 + AutoMigrate
    if app.dao == nil { app.initDao() }

    // ⑤ 缓存 —— 可选的二级缓存，装上后给 dao 挂适配器
    if conf.Cache.Enabled {
        app.initCache()
        app.dao.SetCache(dao.NewCacheAdaptor(app.Cache()))
        if conf.Cache.WarmUp { app.Dao().CacheWarmUp() }
    }

    // ⑥ 配置文件 → 数据库同步（管理员账户等）
    app.syncFromConf()

    // ⑦ 服务初始化 —— 注意：map 遍历无序，所以服务之间不能有 Init 顺序依赖
    for name, s := range *app.service {
        if err := s.Init(); err != nil {
            return fmt.Errorf("Service %s init error: %w", name, err)
        }
    }
}
```

`time.Local` 的初始化用了 `sync.Once` + 名字比对（`internal/core/base.go:63-97`）。这个细节很妙：**同一进程内二次 Bootstrap 时若时区不一致会直接报错**，而不是静默改掉已经运行的服务的时区。

### 2.4 `server.Serve()` 的装配顺序

```go
// server/server.go:46-152
fiberConf := fiber.Config{
    Immutable: true,              // ★ 让 c.Params() 等返回副本，避免并发踩踏
    ErrorHandler: common.ErrorHandler,
    ProxyHeader: app.Conf().HTTP.GetProxyHeader(),
    BodyLimit: app.Conf().HTTP.BodyLimit * 1024 * 1024,
    StreamRequestBody: true,
    EnableIPValidation: true,
}
if proxies := conf.HTTP.TrustedProxies; len(proxies) > 0 {
    fiberConf.EnableTrustedProxyCheck = true
    fiberConf.TrustedProxies = proxies     // ★ 只有可信代理才读 X-Forwarded-For
}

fb := fiber.New(fiberConf)

reqID(fb)                        // 1. X-Request-ID
logger(fb, conf.Debug)           // 2. 访问日志（2xx 只在 debug 打，非 2xx 一律 Error）
swaggerDocs(fb)                  // 3. /swagger/*
cors(app, fb)                    // 4. CORS（基于 trusted_domains）
actionLimit(app, fb)             // 5. 把 limiter 实例塞进 Locals

api := fb.Group("/api/v2")
api.Use(securitymw.Guard(app))   // 6. ★ 安全网关，必须在路由注册前
h.CommentCreate(app, api)        // 7. 各 handler 自行注册路由
...

index(fb)                        // 8. / 重定向到 /sidebar/ 或返回版本 JSON
static(fb)                       // 9. 内嵌 public/ 静态资源
uploadedStatic(app, fb)          // 10. 上传文件目录
```

`Immutable: true` 值得一提：fiber 为性能默认复用请求缓冲区，`c.Params()` 返回的字符串会在下次请求时被覆盖。开了 `Immutable` 后返回副本，代价是分配，换来的是**不会出现"参数串味"的诡异 bug**。

---

## 3. 依赖注入：用泛型做的服务容器

这是全项目最值得学的设计，只有 30 行。

### 3.1 容器本体

```go
// internal/core/base.go:20-28
type App struct {
    conf    *config.Config
    dao     *dao.Dao
    cache   *cache.Cache
    service *map[string]Service      // ★ 指针，允许在 Init 里增删

    onTerminate   *hook.Hook[*TerminateEvent]
    onConfUpdated *hook.Hook[*ConfUpdatedEvent]
}
```

### 3.2 服务名由类型名自动推导

```go
// internal/core/service.go
type Service interface {
    Init() error
    Dispose() error
}

func genServiceName[T any]() string {
    var t T
    name := fmt.Sprintf("%T", t)
    if name != "<nil>" { return name }      // struct 类型
    return fmt.Sprintf("%T", new(T))        // interface 类型
}
```

`AppInject` 是个泛型函数，靠 `genServiceName[T]()` 拿到 `*core.EmailService` 这样的全限定类型名当 key：

```go
// internal/core/base.go:199-224
func AppInject[T Service](app *App, service T) {
    app.Inject(genServiceName[T](), service)
}

func AppService[T Service](app *App) (T, error) {
    if app == nil { var zero T; return zero, fmt.Errorf("app is nil") }
    service, err := app.Service(genServiceName[T]())
    if err != nil { var zero T; return zero, err }
    return service.(T), nil
}
```

### 3.3 使用方式

```go
// 注入（internal/core/base.go:44-52）
func (app *App) injectDefaultServices() {
    AppInject(app, NewEmailService(app))
    AppInject(app, NewIPRegionService(app))
    AppInject(app, NewNotifyService(app))
    AppInject(app, NewAntiSpamService(app))
    AppInject(app, NewAIAssistantService(app))
    AppInject(app, NewSecurityService(app))
}

// 取用（任意位置，典型是 handler）
if svc, err := core.AppService[*core.SecurityService](app); err == nil && svc != nil {
    decision := svc.Check(input)
}
```

### 3.4 这个设计的取舍

**优点**
- 零注册样板：不用写 `container.Register("email", ...)` 的字符串 key
- 类型安全：取出时 `service.(T)` 由泛型约束保证
- 允许后续 `Init` 里互相查找，不会循环依赖编译错误

**代价 / 坑**
- **`map` 遍历无序** → 服务之间不能有 `Init()` 顺序依赖，代码注释专门强调了两次（`base.go:45`、`base.go:151`）
- 取不到服务时返回 error 而不是 panic，所以调用点必须写 `if err == nil`，代码里到处是这个模式
- key 是 `*core.XxxService` 全名，重构包路径会静默失效（返回 error，不会编译报错）

**和前端 DI 的呼应**：前端 `ui/artalk` 里有一个 65 行的 `createInjectionContainer`（`src/lib/injection/container.ts`），同样用 `as const` 的 deps 数组做编译期校验。**前后端在同一套哲学下各写了一遍容器**。

---

## 4. 配置系统：代码即文档

343 个配置项，如果用「结构体 + 独立 YAML 模板 + 独立文档」三处维护，必然漂移。Artalk 的做法是**单向生成**。

### 4.1 数据流

```
internal/config/config.go          ← 唯一真相：Go struct + 注释
        │
        │  go:generate  ← internal/config/base.go:3
        │  go run ./meta/gen --format go --locale zh-CN --pkg config -o ./cache.go
        ▼
internal/config/cache.go           ← 生成：配置模板字符串（含中文注释）
        │
        ├──► cmd/gen.go  →  artalk gen config  ← 用户拿到的 artalk.example.yml
        │
        └──► meta/gen --format markdown --locale en -o ./docs/docs/en/guide/env.md
                     ↓
              docs/docs/{en,zh}/guide/env.md   ← 配置文档
                     ↓
              docs/swagger  ← 后端 API 文档（另一条 swag init 链）
                     ↓
              ui/artalk/src/api/v2.ts  ← swagger-typescript-api 生成的 TS 客户端（3402 行）
```

`Makefile` 里对应的目标：

```make
update-conf:        go generate ./internal/config
update-conf-docs:   go run ./internal/config/meta/gen --format markdown --locale en ...
update-swagger:     swag init -g server/server.go && pnpm -r swagger:build-http-client
update-i18n:        go generate ./internal/i18n
```

**这是整个项目最值得复制的工程实践**：注释写在 struct tag 旁边，配置模板、文档、前端类型全部自动派生。

### 4.2 加载流程

```go
// internal/config/base.go:38-69
func NewFromYAML(cfgFile string, data []byte) (*Config, error) {
    kf := koanf.New(".")

    // ① YAML 打底
    kf.Load(rawbytes.Provider(data), yaml.Parser())

    // ② 环境变量覆盖（前缀 ATK_，如 ATK_PORT=23366）
    kf.Load(env_provider.Provider("ATK_", EnvPathMapCache), nil)

    // ③ 解码
    conf := &Config{cfgFile: cfgFile}
    kf.Unmarshal("", conf)

    // ④ 四轮「补丁」
    conf.historyPatch()    // 兼容老配置项（allow_origins → trusted_domains 等）
    conf.normalPatch()     // 填默认值 + 交叉校验
    conf.i18nPatch(data)   // 语言推断（配置里有中文字符 → zh-CN）
    conf.ipRegionPatch()   // 检查 ip2region 数据文件是否存在

    return conf, nil
}
```

### 4.3 `normalPatch` 里几个有价值的设计

**（a）交叉校验 + 降级**

```go
// internal/config/base.go:191-201
if conf.Security.Enabled {
    // 要求验证码但验证码没开 → 死循环，降级为 reject
    if conf.Security.ActionType() == SecurityActionCaptcha && !conf.Captcha.Enabled {
        log.Warn("config `security.action` is \"captcha\" but `captcha.enabled` is false, fallback to \"reject\"")
        conf.Security.Action = SecurityActionReject
    }
    // 开了总开关却什么都没配 → 提示
    if conf.Security.RateLimit.IP.IsUnlimited() && conf.Security.RateLimit.Global.IsUnlimited() &&
        len(conf.Security.Blacklist) == 0 && !conf.Security.AutoBan.Enabled {
        log.Warn("config `security.enabled` is true, but no rate limit, blacklist or auto ban is configured")
    }
}
```

**（b）安全告警写得像威胁建模**

```go
// internal/config/base.go:234-236
if len(trustedProxies) == 0 && conf.HTTP.GetProxyHeader() != "" {
    log.Warn("config `http.trusted_proxies` is not set, the proxy header is accepted from any address. " +
        "A caller can then fake its source IP and step around the IP bans and per-IP rate limit. " +
        "If Artalk sits behind a proxy or CDN, list its addresses here (IP or CIDR).")
}
```

这句话说明了「不配 trusted_proxies 会怎样被绕过」——比「请配置」有用得多。

**（c）指针字段用于区分「未配置」和「配置为空」**

```go
ProxyHeader *string    // nil = 没写；"" = 明确写了空
DisableThinking *bool  // nil = 没写，走默认 true
```

因为老版本默认 `X-Forwarded-For`，直接改成 `string` 会让老配置文件解析出 `""`，造成 BREAKING CHANGE。用指针保住兼容（`base.go:213-224`，注释把这段历史写清楚了）。

---

## 5. 数据层

### 5.1 模型全景（11 张表）

| 表 | 关键字段 | 说明 |
|---|---|---|
| `sites` | `Name`(unique), `Urls` | 多站点隔离的最外层 |
| `pages` | `Key` + `SiteName`, `AdminOnly`, `PV`, `VoteUp/Down` | 页面 = 文章。`Key` 是 URL 或路径 |
| `users` | `Name`, `Email`, `Password`, `IsAdmin`, `IsAIAssistant`, `TokenValidFrom` | 评论者和管理员共表 |
| `comments` | `Content`, `Rid`(父), `RootID`(根), `PageKey`+`SiteName`, `GuestTokenHash` | 树形结构，冗余 RootID 避免递归 |
| `notifies` | `UserID`, `CommentID`, `IsRead`, `Key` | 通知，`Key` 是免登录退订凭证 |
| `votes` | `TargetID`, `Type`, `UserID`, `IP`, `UA` | 4 种投票类型 |
| `auth_identities` | `Provider`, `RemoteUID`, `UserID`, `Token` | 多身份绑定（新） |
| `user_email_verifies` | `Email`, `Code`, `Attempts`, `ExpiresAt` | 邮箱验证码（新，带尝试次数） |
| `moderation_logs` | `CommentID`, `Checker`, `Status`, `Action`, `Message` | 审核日志（新） |
| `ai_assistant_logs` | `CommentID`, `ReplyCommentID`, `Status`, `AIModel` | AI 助手日志（新） |
| `security_events` | `IP`, `Kind`, `Scope`, `Window`, `Action`, `Blocked` | 安全事件（新） |
| `ip_bans` | `IP`(unique), `IsCIDR`, `ExpireAt`, `HitCount` | IP 封禁（新） |

### 5.2 树形评论的两个技巧

**（a）`RootID` 冗余**

`Rid` 是直接父节点，`RootID` 是整条链的根。有了 `RootID`，「取某条评论的整个 subtree」只需 `WHERE root_id = ?`，不用递归。RootID 在创建时计算：

```go
// internal/dao/query_find.go:47-62
func (dao *Dao) FindCommentRootID(rid uint) uint {
    visited := map[uint]bool{}
    rootId := rid
    for rootId != 0 && !visited[rootId] {
        visited[rootId] = true        // ★ 防环：rid = id 的脏数据会让朴素实现死循环
        var comment entity.Comment
        dao.DB().First(&comment, rootId)
        if comment.Rid == 0 { return rootId }
        rootId = comment.Rid
    }
    return rootId
}
```

**（b）复合外键做多站点隔离**

```go
// internal/entity/comment.go:29-33
// Use Composite Foreign Keys for multiple-site support.
Page *Page `gorm:"foreignKey:page_key,site_name;references:key,site_name"`
```

但注意：`AutoMigrate` 之后会**主动删掉所有外键约束**（`dao/migrate.go:57-62`）：

```go
// Delete all foreign key constraints
// Leave relationship maintenance to the program and reduce the difficulty of database management.
// because there are many different DBs and the implementation of foreign keys may be different,
// and the DB may not support foreign keys, so don't rely on the foreign key function of the DB system.
dao.DropConstraintsIfExist()
```

理由：一套代码要兼容 sqlite / mysql / postgres / sqlserver，外键语法和行为差异太大。**一致性交给程序保证**。

### 5.3 泛型查询 + 缓存 + singleflight

```go
// internal/dao/query_find.go:12-28
func QueryDBWithCache[T any](dao *Dao, name string, queryDB func() (T, error)) (T, error) {
    if dao.cache == nil {
        return QueryDBWithoutCache(dao, name, queryDB)
    }
    return cache.QueryDBWithCache(dao.cache.Cache, name, queryDB)
}

var noCacheFindSingleflightGroup = new(singleflight.Group)

func QueryDBWithoutCache[T any](dao *Dao, name string, queryDB func() (T, error)) (T, error) {
    v, err, _ := noCacheFindSingleflightGroup.Do(name, func() (any, error) {
        return queryDB()      // 无缓存时用 singleflight 防击穿
    })
    ...
}
```

**注意一个潜在问题**：`noCacheFindSingleflightGroup` 是**包级全局**，所有 `Dao` 实例共享。在测试里同时跑多个 app 时，不同数据库的同一个 key（如 `comment-by-id-1`）会互相串数据。这大概也是 fork 里「每个测试 app 独立 db 目录」这条修复的动因之一。

调用方写法非常统一：

```go
func (dao *Dao) FindUser(name string, email string) entity.User {
    user, _ := QueryDBWithCache(dao, fmt.Sprintf(UserByNameEmailKey, strings.ToLower(name), strings.ToLower(email)),
        func() (user entity.User, err error) {
            dao.DB().Where("LOWER(name) = LOWER(?) AND LOWER(email) = LOWER(?)", name, email).First(&user)
            return user, nil
        })
    return user
}
```

缓存 key 是 `fmt.Sprintf` 拼出来的常量模板，集中定义在 `internal/dao/cache.go`。

### 5.4 缓存失效策略

```go
// internal/dao/base.go:47-56
// Transaction avoids caching uncommitted changes. Callers invalidate caches after commit.
func (dao *Dao) Transaction(fn func(*Dao) error) error {
    return dao.db.Transaction(func(tx *gorm.DB) error {
        return fn(&Dao{db: tx})       // ★ 新 Dao 不带 cache，避免把未提交数据写进缓存
    })
}

func (dao *Dao) CacheAction(fn func(cache *DaoCache)) {
    if dao.cache != nil { fn(dao.cache) }    // 空值安全的缓存操作
}
```

**事务内自动禁用缓存**这个设计避免了「缓存里出现回滚掉的数据」这类经典脏读。

### 5.5 迁移的兼容性处理

`dao/migrate.go` 里全是「线上库已经在跑，怎么安全升级」的问题：

```go
// 列改名：guest_key_hash → guest_token_hash（同时改索引名）
if dao.DB().Migrator().HasColumn(&entity.Comment{}, "guest_key_hash") &&
    !dao.DB().Migrator().HasColumn(&entity.Comment{}, "guest_token_hash") {
    dao.DB().Migrator().RenameColumn(...)
    oldIndex := dao.DB().NamingStrategy.IndexName(table, "guest_key_hash")
    newIndex := dao.DB().NamingStrategy.IndexName(table, "guest_token_hash")
    if dao.DB().Migrator().HasIndex(&entity.Comment{}, oldIndex) {
        dao.DB().Migrator().RenameIndex(...)
    }
}

// 加唯一索引前必须先清重复（老版本没有约束，可能有脏数据）
if dao.DB().Migrator().HasTable(&entity.AuthIdentity{}) {
    if err := dao.dedupeAuthIdentities(); err != nil { log.Fatal(...) }
}
```

后者配套了一条注释说明取舍：

```go
// Keeping the newest row matches what the old "find first match" behavior
// effectively used.
```

**先删重再建约束**，并且删重规则要和旧逻辑的「事实行为」一致——这是数据库迁移里最容易翻车的地方。

---

## 6. HTTP 层

### 6.1 Handler 的固定骨架

71 个接口，写法高度一致。以 `comment_list.go` 为例：

```go
// ① 入参结构体：三个 tag 一一对应
type ParamsCommentList struct {
    PageKey  string `query:"page_key" json:"page_key" validate:"required"`
    SiteName string `query:"site_name" json:"site_name" validate:"optional"`
    Limit    int    `query:"limit" json:"limit" validate:"optional"`
    SortBy   string `query:"sort_by" json:"sort_by" enums:"date_asc,date_desc,vote" validate:"optional"`
}

// ② 出参结构体
type ResponseCommentList struct {
    Comments []entity.CommentDTO `json:"comments"`
    Count    int64               `json:"count"`
    Page     *entity.PageDTO     `json:"page,omitempty"`
}

// ③ swaggo 注解（生成 swagger.json）
// @Id           GetComments
// @Summary      Get Comment List
// @Tags         Comment
// @Security     ApiKeyAuth
// @Param        options  query  ParamsCommentList  true  "The options"
// @Success      200  {object}  ResponseCommentList
// @Router       /comments  [get]

// ④ 注册函数：接收 app + router，内部闭包处理
func CommentList(app *core.App, router fiber.Router) {
    router.Get("/comments", func(c *fiber.Ctx) error {
        var p ParamsCommentList
        if isOK, resp := common.ParamsDecode(c, &p); !isOK { return resp }

        user := common.GetUserByReq(app, c)
        // ... 业务

        return common.RespData(c, ResponseCommentList{...})
    })
}
```

**为什么用「注册函数」而不是「方法 + 结构体」？**
因为 handler 需要 `*core.App`。fiber 没有原生 DI，用闭包捕获 `app` 是零成本方案，且每个 handler 可以自由注册任意数量的路由（`auth.go` 就注册了 3 条）。

### 6.2 响应封装

```go
// server/common/resp.go
func RespSuccess(c *fiber.Ctx, msg ...string) error   // {"msg":"Success"}
func RespError(c *fiber.Ctx, code int, msg string, data ...Map) error
func RespData(c *fiber.Ctx, data interface{}) error   // 裸数据
```

`RespError` 有个细节：它会把错误同时**写进结构化日志**，带上 request id / path / ip / ua / status：

```go
func RespError(c *fiber.Ctx, code int, msg string, data ...Map) error {
    respData := Map{}
    if len(data) > 0 { respData = data[0] }
    respData["msg"] = msg
    c.Status(code)
    LogWithHttpInfo(c, func(l *zap.SugaredLogger) { l.Error(msg) })
    return c.JSON(respData)
}
```

日志的 `AddCallerSkip(2)` 让 caller 指向真正的业务代码而不是这个封装函数。

### 6.3 认证：JWT + 三重校验

```go
// server/common/auth.go:14-17
type jwtCustomClaims struct {
    UserID uint `json:"user_id"`
    jwt.RegisteredClaims      // 含 IssuedAt / ExpiresAt
}
```

Token 从三个位置依次尝试（`GetTokenByReq`）：`?token=` → form `token` → `Authorization: Bearer`。

校验时的**双重防线**：

```go
// ① 验签 + 过期
_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (interface{}, error) {
    if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {   // ★ 拒绝 alg=none 攻击
        return nil, fmt.Errorf("unexpected jwt signing method=%v", t.Header["alg"])
    }
    return []byte(key), nil
})

// ② 比对 user.TokenValidFrom —— 改密码/强制下线时把这一列推到当前时刻，
//    之前所有 token 全部失效（无需黑名单）
```

这是 Artalk 实现「改密码后旧 token 立即失效」的方式：**不维护黑名单，只比签发时间戳**。配套的 `LoginGetUserTokenAt` 允许精确到秒地签发，避免「同一秒内签发的旧 token 仍然有效」的窗口期：

```go
// LoginGetUserTokenAt issues a token with an explicit issuance time. Callers
// that rotate user.TokenValidFrom use this to mint a token at the exact second
// that becomes valid, without leaving a same-second window for older tokens.
```

### 6.4 密码的三代兼容

```go
// internal/entity/user.go:47-88
const BcryptPrefix = "(bcrypt)"
const MD5Prefix = "(md5)"

switch {
case strings.HasPrefix(password, BcryptPrefix):
    bcrypt.CompareHashAndPassword(...)
case strings.HasPrefix(password, MD5Prefix):
    strings.EqualFold(password[len(MD5Prefix):], fmt.Sprintf("%x", md5.Sum([]byte(input))))
default:
    password == input      // 明文（历史遗留，仅兼容老库）
}
```

用**自描述前缀**区分算法，避免加字段迁移。新密码一律 bcrypt（`SetPasswordEncrypt`）。

---

## 7. 安全防护体系（fork 核心增量）

### 7.1 分层

```
internal/security/          纯算法 + 内存索引（不知道 HTTP 存在）
├─ bans.go                  IPMatcher（白名单）、BanStore（封禁索引）
├─ counter.go               Counter：三级固定窗口计数（分/时/天）
└─ violations.go            ViolationTracker：违规时间戳滑动窗口

internal/core/service_security.go   编排层（落库、自动封禁、定期清理）
server/middleware/security/         Guard 中间件（HTTP 决策）
```

### 7.2 请求进入 Guard 后的 10 步

```go
// server/middleware/security/security.go:24-79
1. conf.Security.Enabled == false                → 放行
2. GET / HEAD / OPTIONS                          → 放行（只拦写请求）
3. isExcludedPath(path)                          → 放行（默认 /captcha/verify）
4. common.CheckIsAdminReq(app, c)                → 放行（管理员不受限）
5. AppService[*SecurityService] 取失败            → 放行（fail-open，不影响业务）
6. input.IP == ""                                → 放行（拿不到 IP 就不限制）
7. svc.IsWhitelisted(ip)                         → 放行，且不计数
8. svc.BanMatch(ip) 命中                          → 403 + {"ip_banned": true}
9. decision := svc.Check(input); Allowed          → 放行
10. 按 decision.Action 分派：
    reject  → 429 + Retry-After
    ban     → 403 + {"ip_banned": true}
    default → 403 + {"need_captcha": true, "img_data": "..."}
```

第 3 步的路径匹配写得很克制：

```go
// server/middleware/security/security.go:92-105
// 后缀匹配便于忽略 `/api/v2` 前缀；前缀匹配必须落在路径边界上，
// 否则 `/captcha/verify` 会连带放过 `/captcha/verify-anything`。
if strings.HasSuffix(path, item) || strings.HasPrefix(path, item+"/") {
```

第 10 步返回的字段名 `need_captcha` / `img_data` / `iframe` **刻意与旧版 limiter 保持一致**，这样官方前端不用改一行代码就能弹验证码。

### 7.3 计数算法

```go
// internal/security/counter.go:55-66
type Counter struct {
    mu        sync.Mutex
    ipStats   map[string]*windowStat    // 每个 IP 一个结构
    global    windowStat                // 全局一份
    lastSweep time.Time
}

type windowStat struct {
    lastSeen                                  time.Time
    minuteStart, hourStart, dayStart          time.Time
    minute, hour, day                          int
}
```

- 固定窗口，不是滑动窗口
- 一次 `Allow()` 同时推进三个窗口的计数
- 注释坦白了这个取舍：

```go
// 固定窗口的实现允许在窗口边界处出现最多两倍于配额的突发请求，
// 换来 O(1) 的读写与极低的内存占用
```

- 计数是**先自增再比较**：配额 30 时第 31 次被拒
- 24 小时无人访问的 IP 条目会被 `sweepLocked` 清掉（每分钟最多扫一次）

### 7.4 自动封禁的真实逻辑

不是"风控评分"，只是**阈值计数**：

```go
// internal/core/service_security.go:426-454
autoBan := conf.Security.AutoBan.Normalized()
count := s.violations.Add(ip, now, time.Duration(autoBan.Window)*time.Second)
if count < autoBan.Threshold { return false }
ban, err := s.ban(ip, reason, "", entity.IPBanSourceAuto, autoBan.Duration, 0, now)
```

默认 `5 次 / 3600s` 触发，封 `86400s`。

有个刻意的降级：**全局超限不能封单个 IP**（无法归因），会退化成 `reject`：

```go
// internal/core/service_security.go:205-208
if action == config.SecurityActionBan && trip.Scope != security.ScopeIP {
    // 全局超限无法归咎于单个 IP，退化为直接拒绝
    action = config.SecurityActionReject
}
```

### 7.5 状态存储的真实分布

| 状态 | 位置 | 说明 |
|---|---|---|
| IP 窗口计数 | 内存 map | 24h 无请求清理 |
| 违规时间戳 | 内存 map | 封禁成功或验证码通过时 Reset |
| 封禁索引 | 内存 `BanStore` + DB `ip_bans` | 每小时重载一次 |
| 安全事件 | DB `security_events` | 保留 90 天 |
| 验证码答案 | 内存 `simple_cache` | 5 分钟 TTL，按 IP 单槽位 |

**完全没有用 Redis**。这意味着：3 副本部署时 `minute: 30` 实际等于 90；A 副本签发的验证码到 B 副本校验会失败（除非做会话粘滞）。这是刻意的「用简单换性能」，但部署前必须知道。

事件写入有节流（`securityEventThrottle = time.Minute`），避免被打时把事件表刷爆。

### 7.6 已知的薄弱点（从代码里能直接读出来）

1. **只按 IP 封禁**，无用户/邮箱/指纹维度。换 IP 即绕过；NAT 出口会连坐无辜用户。
2. **`SecurityCheckInput.UserID/UserName` 在 Guard 里从未被赋值**（`newCheckInput` 只填 IP/Method/Path/UA/SiteName），所以 `security_events` 的 user 维度恒为空，报表里"按用户"维度失效。
3. **`SiteName` 取的是 query 参数**，而评论创建把 `site_name` 放在 JSON body 里 → 该字段通常为空。
4. `Counter` 用一把全局 `sync.Mutex` 串行化所有 IP，不是分片设计。评论场景够用，但不是高并发设计。

---

## 8. 内容审核与 AI 助手

### 8.1 反垃圾的两级拦截

```
POST /api/v2/comments
  │
  ├─ CheckIntercept（保存前，同步）
  │    命中关键词 → 400，评论根本不入库
  │    ★ 响应里绝不回显命中的词，只在 moderation_logs 留证据
  │    // Never name the matched keyword: the sender would learn
  │    // exactly which word to obfuscate next time.
  │
  └─ CreateComment（入库）
        │
        └─ go commentCreatedJobs（异步）
              ├─ CheckAndBlock → 5 个 checker 全部跑（不短路）
              │     任一不过 → OnBlockComment → IsPending = true
              ├─ NotifyService.Push（被审核结果阻塞：blocked 的不通知）
              └─ AIAssistantService.ReplyToComment
```

**为什么不短路**：`// All enabled checkers are executed so every stage can be recorded.` —— 日志要完整。

**失败策略可配置**：

```go
pass = lo.If(as.conf.ApiFailBlock, false).Else(true)   // block if api fail
```

云 API 挂了 → 默认放行（fail-open），可配成转待审（fail-closed）。

### 8.2 Checker 接口

```go
type Checker interface {
    Name() string
    Check(p *CheckerParams) (bool, error)
}
// (true, nil)  = 通过
// (false, nil) = 判定为垃圾
// (x, err)     = 调用失败
```

`CheckerParams` 准备了三种文本形态，用途不同：

| 字段 | 内容 | 给谁用 |
|---|---|---|
| `RawContent` | 原始 Markdown/HTML | replace 回写 |
| `ReviewContent` | 剥离 URL 后的可见文本 | 关键词匹配 |
| `ReviewText` | `"昵称: X\n评论: Y\n链接: ..."` | 云端 / LLM |
| `Links` | 单独抽取的外链（≤5 条） | 云端 |

分开的原因很实际：**可见文本把 URL 都剥掉了，而广告垃圾恰恰藏在这里**。

### 8.3 AI 助手的完整流程

这是 fork 里最"有个性"的模块（806 行，自带人格设定「清羽酱」）。

```go
// internal/core/service_ai_assistant.go:82-119
func (s *AIAssistantService) ReplyToComment(comment *entity.Comment) {
    // ① 开关
    if !conf.Enabled { return }

    // ② 判断是否 @ 了助手（含旧库兼容：匹配完整身份而非仅 email）
    targeted := commentTargetsAssistant(latest.Content, trigger, parentUser)
    if !targeted { targeted = legacyAssistantIdentityMatches(parentUser, conf) }
    if !targeted { return }

    // ③ 回复
    if err := s.reply(comment, conf, trigger); err != nil {
        s.record(comment, nil, trigger, StatusError, "", err.Error())
    }
}
```

`reply()` 的 8 个前置条件（每一个都对应一次踩坑）：

```go
1. latest.IsEmpty()                           → 触发评论已被删，返回
2. latest.IsPending && !conf.ReplyToPending   → 待审评论不回复（可配）
3. assistantUser(conf) 失败                    → 助手账号准备失败
4. latest.UserID == assistant.ID              → 助手不回复自己
5. 已有助手对这条评论的回复                     → 幂等保护（防重复回复）
6. reserveRateLimit() 失败                     → 超配额，回一句限流文案
7. fetchPageTextWithSelectors() 失败           → 回「获取不到页面内容」
8. request(prompt) 失败                        → 回「AI 脑子烧掉了」
```

**注意第 6-8 步**：所有失败都**回复一条友好文案**而不是静默失败。用户体验上「助手说话但答不出」比「助手装死」好。

**上下文构造**只取祖先链，不取兄弟分支：

```go
// assistantThreadContext returns the triggering comment's ancestor chain.
// Sibling branches are deliberately excluded so unrelated conversations do
// not enter the prompt. Results are ordered oldest-first and the current
// message remains the dynamic suffix for stable provider prefix caching.
```

最后一句注释点出了 prompt 工程细节：**当前消息放最后**，是为了让 provider 的 prefix caching 命中（前半段稳定不变）。

**限频是双层的**：全局日限（`DailyLimit: 40`）+ 单用户小时限（`UserHourlyLimit: 5`），用 `rateMu` 一把锁同时检查并消费，避免 TOCTOU。

**AI 协议适配**支持 5 种，且对「关闭 thinking」的处理非常细：

```go
// aiAssistantReasoningEffortThinkingOn is sent while thinking stays enabled.
// "low" sits at the bottom of every provider's effort enum...
// The effort knob only tunes how deep the thinking goes; it cannot switch it off.
const aiAssistantReasoningEffortThinkingOn = "low"

// The switch that actually turns thinking off for chat completions / Anthropic.
const aiAssistantThinkingTypeDisabled = "disabled"

// The Responses API spelling of the same switch, where "none" is documented.
const aiAssistantReasoningEffortDisabled = "none"
```

这三条常量是「读文档读到能区分三个协议语义差异」的产物。

---

## 9. 前端：三套技术栈并存

这是本项目最容易让人困惑的部分。`ui/` 下有 7 个包，用了 **3 种框架**。

### 9.1 包清单

| 包 | 版本 | 栈 | 产物 | 用途 |
|---|---|---|---|---|
| `ui/artalk` | 2.10.1 | **原生 TS + SCSS**（无框架） | ESM/UMD/CJS/IIFE 四格式 + `.css` + `.d.ts` | 嵌入博客的评论组件 |
| `ui/artalk-sidebar` | 2.0.0 | **React 19 + HeroUI + Tailwind 4** | 普通 SPA（hash 路由，`base:'./'`） | 管理后台 |
| `ui/plugin-kit` | 1.0.9 | tsdown（构建工具） | ESM/CJS + dts | 插件开发脚手架（**构建期**） |
| `ui/plugin-auth` | 0.0.3 | **Solid** | 四格式 UMD | 登录弹窗插件 |
| `ui/plugin-katex` | 0.2.5 | 纯 TS | 四格式 UMD | 公式渲染 |
| `ui/plugin-lightbox` | 0.2.5 | 纯 TS | 四格式 UMD | 图片灯箱 |
| `ui/eslint-plugin-artalk` | 1.0.3 | tsdown | ESM/CJS | 自定义 lint 规则 |

**为什么核心不用框架？** 见 `ui/artalk` 的设计取向：零框架运行时，换来的是在任何博客上都能跑、体积可控；代价是手写 DOM 与手写状态同步。

### 9.2 `ui/artalk` 的源码组织

```
src/
├─ main.ts          库入口（导出 init/use/loadCountWidget/setLocale）
├─ artalk.ts        主类 Artalk（生命周期门面，约 150 行）
├─ mount.ts         挂载流程（四阶段异步）
├─ context.ts       Context：DI + 事件 + 配置 + i18n 四合一门面
├─ config.ts        配置合并 / getRootEl / convertApiOptions
├─ defaults.ts      默认配置
├─ data.ts          DataManager（comments / notifies / page / loading）
├─ api/
│   ├─ v2.ts        3402 行 ★ swagger 自动生成的 API 客户端
│   ├─ index.ts     class Api extends ApiV2
│   ├─ fetch.ts     自定义 fetch（注入 Bearer / X-Artalk-Guest）
│   └─ handler.ts   错误动作处理器注册表（need_captcha / need_auth_login）
├─ services/        13 个服务注册器（ArtalkPlugin，其中 11 个走 Services 集合）
├─ comment/         单条评论：CommentNode + 7 个渲染切片 + actions
├─ list/            列表：flat/nest 布局 + 两种分页器
├─ editor/          编辑器：UI + 状态机（normal/reply/edit）
├─ layer/           弹层系统 + SidebarLayer（iframe 装后台）
├─ components/      dialog / pagination / action-btn / checker(验证码)
├─ i18n/            内置 en/zh-CN + 外置语言包注册
├─ lib/             DI 容器 / 事件管理器 / marked / sanitizer / storage
├─ plugins/         内置插件（11 服务 + 10 全局 + 15 编辑器 + 17 列表 = 50+ 个）
├─ style/           SCSS（_color / _variables / _extends + 各模块）
└─ types/           按领域拆分的类型定义
```

### 9.3 挂载四阶段（`mount.ts`）

```ts
1. loadPlugins(Services)              // 只装"拿到远端配置所需"的服务
2. GET /api/v2/conf                   // 拉远端配置
     失败 → MountError(ctx, { onRetry: () => mount(localConf, ctx) })
3. conf = preferRemoteConf
     ? mergeDeep(conf, remoteConf)
     : mergeDeep(remoteConf, conf)
   ctx.updateConf(conf); setLocale(...)
4. loadNetworkPlugins(conf.pluginURLs)   // 动态 <script> 注入
   loadPlugins(GlobalPlugins)
   loadPlugins(networkPlugins)
   trigger('created') → trigger('mounted')
```

幂等处理做得很细：`instances: WeakMap<Context, {loaded: Set<ArtalkPlugin>, mounted}>` 记录已加载插件，`scriptLoads: WeakMap<HTMLScriptElement, Promise<void>>` 复用进行中的脚本加载，失败的脚本会 `script.remove()` 以便重试。

### 9.4 前端 DI 容器（65 行）

```ts
// src/lib/injection/container.ts:45-62
const inject = (key) => {
  const provider = providers.get(key)
  if (!provider) throw new Error(`No provide for ${String(key)}`)
  if (provider.lifecycle === 'singleton' && initializedDeps.has(key)) {
    return initializedDeps.get(key)
  }
  const { impl, deps } = provider
  const params = deps.map((d) => inject(d))     // 递归解析依赖
  const resolved = impl(...params)
  initializedDeps.set(key, resolved)
  return resolved
}
```

服务清单由接口强制约束（`src/types/service.ts`）：

```ts
export interface Services {
  config; events; data; api; apiHandlers
  comments; markdown            // @internal
  editor; editorPlugs; list; sidebar; checkers; layers; user
}
```

注册时依赖用 `as const` 声明，让 `Constructor<T, S, D>` 能推导出参数类型：

```ts
ctx.provide('api',
  (user, config, apiHandlers) => new Api({...}),
  ['user', 'config', 'apiHandlers'] as const,
  { lifecycle: 'transient' },
)
```

### 9.5 插件系统

**运行时接口只有 3 行**：

```ts
// src/types/plugin.ts
export type ArtalkPlugin<T = any> = (ctx: Context, options?: T) => void
```

注册与自动加载：

```ts
// 手动注册
Artalk.use(MyPlugin, options)

// UMD 脚本自动注册（plugin-kit 的 transform 注入）
if (typeof window !== 'undefined') {
  !window.ArtalkPlugins && (window.ArtalkPlugins = {})
  window.ArtalkPlugins.ArtalkXxxPlugin = ArtalkXxxPlugin
  window.Artalk?.use(ArtalkXxxPlugin)
}
```

**生命周期钩子就是事件名**（`src/types/event.ts`）：

```
created / mounted / updated / unmounted
list-fetch / list-loaded / list-failed / list-reach-bottom
comment-inserted / comment-updated / comment-deleted / comment-rendered
notifies-updated / page-loaded / editor-submit / user-changed
sidebar-show / sidebar-hide / ...
```

**第二套插件体系是编辑器插件（class 继承）**：

```ts
export class EditorPlugin {
  contentTransformer?(rawContent: string): string
  editorStateEffectWhen?: EditorState
  editorStateEffect?(comment: CommentData): () => void

  useBtn(html)                                  // 底部加按钮
  usePanel(html)                                // 按钮点击后的面板
  useContentTransformer(fn)                     // 提交前改写内容
  useEditorStateEffect(state, effectFn)         // 绑定 reply/edit 状态
}
```

共有 15 个内置编辑器插件，按配置项（`imgUpload` / `emoticons` / `preview` / `editorTravel`）动态启用。

### 9.6 `artalk-sidebar`：React 19 后台

**技术栈**：React 19.2.5 + HeroUI 3.2.6 + Tailwind 4 + TanStack Router 1.168（hash 路由）+ TanStack Query 5.99 + lucide-react。

**13 个页面**：`login / dashboard / comments / moderation / pages / users / sites / transfer / settings / security / console / activity / account`

**最巧妙的一处设计**：后台不重新实现评论交互，而是**开一个隐藏的 headless Artalk 实例**调所有 API：

```ts
// src/lib/artalk.ts
export function bootArtalk(...) {
  const el = document.createElement('div')
  el.style.display = 'none'
  document.body.append(el)

  instance = Artalk.init({
    el, server: '../', pageKey, site, pvAdd: false,
    noComment: '<div></div>', flatMode: true,
    fetchCommentsOnInit: false,
  })
}
```

更进一步，`comments.tsx` 直接把 widget 的列表 DOM **"收养"**进 React 页面：

```tsx
/**
 * The comment list is the only page that renders the public Artalk widget: the
 * console keeps a headless instance for every API call, and here its list
 * element is adopted into the page so moderation actions, replies and the
 * editor all keep working exactly as on the host site.
 */
```

这样回复、编辑、审核这些交互**只维护一份代码**。

**权限守卫**是纯函数 + `throw redirect()`：

```ts
requireLogin()      // 无 token → /login
hasAccount()        // 有 email 无 account token 的访客 → 可以登录
requireAdmin()      // 非 admin → /comments；admin 但处于 user 视图 → /activity
```

**启动参数清洗**（防 token 泄漏到 history / Referer）：

```ts
// src/lib/boot.ts
if ((search.has('user') || fragment.has('token') || guestToken) && window.history.replaceState) {
  window.history.replaceState({}, '', window.location.pathname)
}
```

### 9.7 `plugin-kit`：构建期能力注入

它不是运行时 API，而是**插件开发者用的 Vite 插件**：

1. 强制 `build.lib` 配置：四格式 + `external: ['artalk']` + `globals: { artalk: 'Artalk' }`
2. 包名校验：必须匹配 `/^(@artalk\/|artalk-)(plugin-[a-z0-9-]+)$/`
3. 构建前三项 lint（exports / manifest / entry 导出名），失败 `exit(1)`
4. `transform` 自动注入 `window.ArtalkPlugins` 注册代码
5. 劫持 `index.html` 用 `ui/artalk/index.html` 做 dev 调试页
6. `writeBundle` 里用 TS compiler API + api-extractor 生成 d.ts

---

## 10. 代码写法与风格约定

这一节是最实用的部分——如果你要往这个项目提 PR，这些是明确写死或强约束的。

### 10.1 Go 侧

| 约定 | 说明 |
|---|---|
| **错误返回 `(T, error)`** | 不用 panic 做控制流；`log.Fatal` 只在 CLI 入口用 |
| **注释用完整句子 + 说明「为什么」** | 例：`// unlock is not deferred to avoid deadlocks in case Trigger is called recursively by the handlers` |
| **中英混排** | 老代码/配置注释中文，新增代码英文；同一文件里可能都有 |
| **接口优先窄** | `Service` 只有 `Init/Dispose`；`Checker` 只有 `Name/Check` |
| **泛型用于容器与工具** | `AppService[T]`、`QueryDBWithCache[T]`、`Hook[T]`、`KeyMutex[T]` |
| **函数选项极少** | 更倾向显式传 struct（`log.Options{}`、`ExportParams{}`） |
| **`lo` 库（samber/lo）** | `lo.If(...).Else(...)`、`lo.Must` 等函数式工具用得很多 |
| **不用 DI 框架** | 自建 30 行容器 |
| **`errors.Is/As` 而非字符串比较** | `hook.ErrStopPropagation` 是标准用法 |

### 10.2 前端侧（Prettier 强约束）

```json
{
  "printWidth": 100,
  "semi": false,           // 无分号
  "singleQuote": true,
  "tabWidth": 2,
  "trailingComma": "all",
  "arrowParens": "always"
}
```

命名约定：
- **DOM 引用统一 `$` 前缀**：`$root` / `$el` / `$btn` / `$textarea`
- **私有成员 `_` 前缀 + TS `private`**：`_deps` / `_$root` / `_destroyed`
- **class 用于有状态主体**（Artalk / Context / List / Editor），**函数式用于服务注册器**（`export const XxxService: ArtalkPlugin = (ctx) => {}`）
- **`@internal` 标注内部 API**
- **分区注释横幅** `// ===========================`

### 10.3 自研 lint 规则把架构约定固化

`eslint-plugin-artalk` 的 `artalk/artalk-plugin` 规则，定义了 6 条硬约束：

```ts
noLifeCycleEventInNestedBlocks   // ctx.on() 只能写插件顶层
noEventInWatchConf               // watchConf 回调里不许绑定/解绑事件
noInjectInNestedBlocks           // ctx.inject() 只能在顶层
noInjectOutsidePlugin            // ctx.inject() 只能在插件函数内
noCycleDeps                      // provide 依赖不能成环（Tarjan SCC）
onePluginPerFile                 // 一个文件一个插件
```

这解释了为什么核心代码全写成：

```ts
export const XxxService: ArtalkPlugin = (ctx) => {
  ctx.provide('xxx', ..., ['dep'] as const)   // 顶层
  ctx.on('mounted', () => {...})              // 顶层
}
```

**约定被 lint 强制，而不是靠 review。**

---

## 11. 工程化

### 11.1 构建产物流向

```
pnpm build:all
  ├─ ui/artalk      → dist/{Artalk,ArtalkLite}.{js,mjs,cjs,iife.js,css}
  │                 → dist/i18n/*.js（每个语言单独构建一次）
  ├─ ui/artalk-sidebar → dist/{index.html, assets/*}
  └─ ui/plugin-*    → dist/artalk-plugin-*.js

scripts/build-frontend.sh
  ├─ ui/artalk/dist/*          → public/dist/
  ├─ ui/artalk/dist/i18n/*.js  → public/dist/i18n/
  ├─ ui/artalk-sidebar/dist/*  → public/sidebar/
  └─ ui/plugin-*/dist/artalk-plugin-*.js → public/dist/plugins/

make pack-frontend → internal/pkged（go:embed 打包）

go build -ldflags "-s -w -X .../config.Version=v2.10.1" → 单文件二进制
```

### 11.2 测试体系

| 层级 | 位置 | 工具 | 特点 |
|---|---|---|---|
| Go 单元测试 | `*_test.go` 与源码同目录 | testify | 覆盖率目标见 `codecov.yml` |
| Go 集成测试 | `server/handler/*_test.go` | testfixtures + sqlite | 文件名是**行为场景**（`guest_security_test.go`、`comment_write_failure_test.go`） |
| 前端单元 | `ui/*/src/**/*.test.ts` | vitest + jsdom | 与源码同目录 |
| 前端集成 | `ui/artalk/tests/*.test.ts` | vitest | 命名是场景（`instance-isolation.test.ts`、`resource-cleanup.test.ts`） |
| E2E | `ui/artalk/tests/e2e/*.spec.ts` | Playwright | 起真实后端 |

**Go 测试脚手架**（`test/app.go`）：

```go
func NewTestApp() (*TestApp, error) {
    // 每个 app 独立 db 目录 —— 注释解释了为什么：
    // A single shared temp file meant every test recreated the previous test's
    // path right after Cleanup removed it, so a failure to close or to drop the
    // -wal/-shm sidecars leaked into the next database.
    dbDir, _ := os.MkdirTemp("", "atk_test_db")

    dbInstance, _ := gorm.Open(sqlite.Open(filepath.Join(dbDir, "atk_test.db")), &gorm.Config{
        NamingStrategy: schema.NamingStrategy{TablePrefix: "atk_"},
        DisableForeignKeyConstraintWhenMigrating: true,
    })

    dao := dao.NewDao(dbInstance)           // 自动迁移建表
    fixtures, _ := testfixtures.New(         // 灌入 test/fixtures/*.yml
        testfixtures.Database(sqlDB),
        testfixtures.Dialect("sqlite"),
        testfixtures.Directory("./test/fixtures"),
    )
}
```

fixtures 有 6 个：`atk_users / atk_sites / atk_pages / atk_comments / atk_notifies / atk_votes`。

### 11.3 CI

`.github/workflows/`：`build.yml` / `build-app.yml` / `build-docker.yml` / `build-rpm.yml` / `build-tagging.yml` / `build-done.yml` / `release.yml` / `test-app.yml`

发布链路：`goreleaser`（多平台 linux/darwin/windows × amd64/arm64）+ Docker（多架构，按 digest 推送）+ npm（OIDC trusted publishing）。

---

## 12. 如何扩展：三条常见路径

### 12.1 加一个配置项（4 步，全自动派生）

```
1. internal/config/config.go  → 在对应 Conf struct 里加字段 + 注释
2. internal/config/base.go    → 如需默认值，加到 normalPatch()
3. make update-conf            → 重新生成 config/cache.go（配置模板）
4. make update-conf-docs       → 重新生成 docs/docs/{en,zh}/guide/env.md
```

前端后台会自动读到（`setting` 接口返回完整 config），如果需要自定义 UI，改 `ui/artalk-sidebar/src/lib/settings-option.ts`。

### 12.2 加一个 API（5 步）

```
1. server/handler/xxx.go
   - type ParamsXxx struct {...}       // query/json/validate/enums tag
   - type ResponseXxx struct {...}
   - swaggo 注解（@Router 等）
   - func Xxx(app *core.App, router fiber.Router) { router.Post("/xxx", ...) }

2. server/server.go   → 在 admin() 或 api 分组里加 h.Xxx(app, api)

3. make update-swagger
   → docs/swagger/{docs.go,swagger.json,swagger.yaml}
   → pnpm -r swagger:build-http-client
   → ui/artalk/src/api/v2.ts（自动加方法）

4. 前端调用：ctx.getApi().xxx.yyy()
5. 加测试：server/handler/xxx_test.go
```

### 12.3 加一个前端插件（3 步）

```
1. pnpm create vite ui/plugin-xxx    （或用现有 plugin 复制）
2. package.json:
   "name": "@artalk/plugin-xxx"      ← 必须匹配 /^(@artalk\/|artalk-)(plugin-[a-z0-9-]+)$/
   peerDependencies: { "artalk": "^2.10.0" }
   devDependencies: { "@artalk/plugin-kit": "workspace:^" }
3. vite.config.ts:
   import { ViteArtalkPluginKit } from '@artalk/plugin-kit'
   export default defineConfig({ plugins: [ViteArtalkPluginKit()] })
   src/main.ts: export const ArtalkXxxPlugin: ArtalkPlugin = (ctx) => {...}
```

`plugin-kit` 会自动处理四格式打包、`external: artalk`、`window.ArtalkPlugins` 自动注册、d.ts 生成、dev 调试页。

---

## 13. 架构评价：好在哪，坑在哪

### 13.1 值得学习的设计

| 设计 | 价值 |
|---|---|
| **泛型 DI 容器（30 行）** | 零注册样板 + 类型安全，前后端各实现一遍 |
| **配置即文档（单向生成）** | 343 个配置项不漂移，配置模板/文档/前端类型全自动 |
| **`go:embed` 单文件交付** | 部署体验极好，一个二进制 + 一个 yml |
| **Handler 注册函数模式** | 闭包捕获 app，避开 fiber 无 DI 的问题 |
| **自描述密码前缀** | `(bcrypt)` / `(md5)` 免加字段迁移 |
| **TokenValidFrom 代替黑名单** | 改密码即全端下线，无需额外存储 |
| **事务内禁用缓存** | 消灭「缓存脏读」类 bug |
| **后台「收养」widget DOM** | 评论交互只维护一份代码 |
| **lint 固化架构约定** | Tarjan 检测依赖环，约定不靠 review |
| **提交信息 = 完整英文句子** | 读 git log 就能懂为什么改 |

### 13.2 真实存在的坑（读代码得出，非猜测）

| 问题 | 位置 | 影响 |
|---|---|---|
| `noCacheFindSingleflightGroup` 是包级全局 | `dao/query_find.go:19` | 多 app 实例（测试）同 key 会串数据 |
| 限频状态全在进程内存 | `security/*.go` | 多副本部署配额翻倍；验证码跨副本失效 |
| 封禁只有 IP 维度 | `entity/security.go` | 换 IP 绕过；NAT 连坐 |
| `SecurityCheckInput.UserID` 从未赋值 | `middleware/security/security.go:81` | 事件报表 user 维度为空 |
| `SiteName` 从 query 取 | 同上 | 评论接口把 site_name 放 body → 该字段常空 |
| 腾讯/阿里云 HTTP 客户端无超时 | `cloud/qcloud/api.go:57`、`aliyun/api.go:98` | 云侧挂起会拖住评论异步 goroutine |
| Akismet 客户端也无超时 | `anti_spam/akismet.go:43` | 同上 |
| 审核 checker 不短路 | `anti_spam/base.go:79` | 一次评论最多 4 次外部 HTTP |
| `internal/sync` 名不副实 | 全包 51 行 | 实际是 `KeyMutex`，且 key 永不回收 |
| `internal/config/cache.go` 是生成文件 | `go:generate` | 手改会被覆盖 |
| `ArtalkLite` 的 `define: { ARTALK_LITE: false }` 硬编码 | `ui/artalk/vite.config.ts:66` | Lite 实际只是 marked 外置，非功能裁剪 |
| `sidebar/README.md` 仍写 Vue 3 | `ui/artalk-sidebar/README.md` | 文档残留，实际是 React 19 |
| `crypto-js` 在 devDependencies 但运行时代码 import | `ui/artalk-sidebar/package.json` | 语义错误，靠 Vite 打包掩盖 |
| `Counter` 单锁串行所有 IP | `security/counter.go:140` | 非高并发设计 |

### 13.3 一句话总结这个项目的架构哲学

> **内核极简，能力外置，约定靠工具固化。**

- 后端：`internal/` 只提供容器 + 数据 + 6 个服务，其余全是可选启用
- 前端：内核只有 Context + DI + 事件，连评论列表都是插件
- 工程：配置、文档、API 类型、lint 规则全部单向生成或自动校验

这个取向的代价是**读代码的入口成本高**（要知道 DI 怎么用、事件有几个、插件分几类），收益是**每一层都可以被替换或关闭**。

---

## 附录 A：关键文件索引

```
启动链路
  main.go
  cmd/base.go                              ← eagerParseFlags / BootMode / initDataDir
  internal/core/base.go                    ← App / Bootstrap / 泛型 DI
  server/server.go                          ← fiber 装配 + 路由注册

配置
  internal/config/config.go                ← 唯一真相（343 项）
  internal/config/base.go                  ← koanf 加载 + 4 轮 patch
  internal/config/security.go               ← 安全配置 + Normalized 默认值
  internal/config/meta/gen/main.go         ← 配置元数据生成器

数据
  internal/dao/base.go                     ← Dao / Transaction（禁缓存）
  internal/dao/query_find.go               ← 泛型查询 + singleflight
  internal/dao/migrate.go                  ← 兼容性迁移 + 去外键
  internal/entity/*.go                     ← 11 张表模型

关键能力
  internal/security/{bans,counter,violations}.go
  server/middleware/security/security.go   ← Guard 10 步
  internal/core/service_security.go        ← 编排 + 自动封禁 + 清理
  internal/anti_spam/base.go               ← Checker 装配
  internal/core/service_ai_assistant.go    ← AI 助手（806 行）
  internal/hook/hook.go                    ← 泛型事件总线

前端
  ui/artalk/src/main.ts                    ← 库入口
  ui/artalk/src/artalk.ts                  ← 主类
  ui/artalk/src/mount.ts                   ← 四阶段挂载
  ui/artalk/src/context.ts                 ← 四合一门面
  ui/artalk/src/lib/injection/container.ts ← 前端 DI（65 行）
  ui/artalk/src/types/plugin.ts            ← 插件接口（3 行）
  ui/plugin-kit/src/plugin/main.ts         ← 构建期注入
  ui/artalk-sidebar/src/lib/artalk.ts      ← headless 实例
  ui/artalk-sidebar/src/router.tsx         ← 路由 + 权限守卫

工程
  Makefile                                  ← 所有生成目标
  scripts/build-frontend.sh                 ← 产物归位
  test/app.go                               ← 测试脚手架
  .prettierrc / eslint.config.mjs           ← 代码风格
  ui/eslint-plugin-artalk/src/artalk-plugin.ts  ← 自研规则
```

## 附录 B：环境要求与本机现状

| 依赖 | 项目要求 | 本机现状 | 结论 |
|---|---|---|---|
| **Go** | `go.mod` 声明 **1.26.5** | ❌ **未安装** | **必须先装 Go 才能编译后端** |
| Node | `>= 22.19.0` | ✅ v22.22.2 | 满足 |
| pnpm | `>= 10.33.2` | ✅ 11.4.0 | 满足 |
| CGO | 不需要（`CGO_ENABLED=0`） | — | sqlite 用纯 Go 实现（`libtnb/sqlite`） |

**Go 1.26.5 这个版本号需要特别注意**：它比 `go.mod` 里其他依赖的发布日期都新，说明这个 fork 在跟随最新工具链。如果本机装的是老版本 Go，`go build` 会直接因 `go` 指令版本过高而拒绝编译（提示 `go.mod requires go >= 1.26.5`）。要么装对应版本，要么手动下调 `go.mod` 第一行的版本号（有兼容风险）。

### B.1 环境准备（Windows）

```bash
# 1. 安装 Go（版本需 >= 1.26.5）
winget install GoLang.Go
# 或从 https://go.dev/dl/ 下载 msi

# 2. 验证
go version

# 3. 拉依赖（国内建议配代理）
go env -w GOPROXY=https://goproxy.cn,direct
```

### B.2 网络限制说明（本次实际遇到）

本机 git 走 `http://127.0.0.1:54710` 代理时，`git clone` 报 `CONNECT tunnel failed, response 502`；绕过代理直连则 `Failed to connect to github.com:443`。

最终通过 **GitHub API 下载 tarball** 拿到了完整代码，并已 `git init` 重建本地仓库、配好 `origin` / `upstream`。

**影响**：本地仓库只有一个 `chore: import snapshot` 提交，**没有完整的 Git 历史**。如果需要历史（`git log` / `git blame` / 合并上游），需要解决 git 协议的网络问题后重新 fetch：

```bash
# 排查方向（按顺序试）
git -c http.proxy= -c https.proxy= fetch --depth 1 origin master   # 完全绕代理
git -c http.sslVerify=false fetch origin master                    # 关证书校验
# 或用 SSH（若已配 key）
git remote set-url origin git@github.com:MineXine/Artalk.git && git fetch origin
```

### B.3 常用命令

```bash
# 后端开发（热 reload 需自己配 air）
make dev                    # 编译 + 启动，放开 localhost CORS

# 只跑后端
make build && ./bin/artalk server
./bin/artalk gen config ./artalk.yml
./bin/artalk admin

# 前端
pnpm install
pnpm dev                    # ui/artalk 调试页
pnpm dev:sidebar            # 后台开发（23367，代理到 23366）
pnpm build:all              # 核心 + 后台
pnpm build:plugins
pnpm test                   # vitest workspace
pnpm lint

# 联调全量
make build-frontend
make pack-frontend
make build

# 代码生成（改配置/接口后必跑）
make update-conf
make update-conf-docs
make update-swagger
make update-i18n

# 测试
make test                   # go test ./...
make test-coverage-html
make test-frontend-e2e
```

---

*本文档基于 `master` @ `db22679` 的静态代码阅读生成。所有结论均可回溯到文中标注的文件路径与行号。*
