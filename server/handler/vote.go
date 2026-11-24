package handler

import (
	"errors"
	"fmt"
	"strings"

	"github.com/artalkjs/artalk/v2/internal/core"
	"github.com/artalkjs/artalk/v2/internal/dao"
	"github.com/artalkjs/artalk/v2/internal/entity"
	"github.com/artalkjs/artalk/v2/internal/i18n"
	"github.com/artalkjs/artalk/v2/internal/utils"
	"github.com/artalkjs/artalk/v2/server/common"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResponseVote struct {
	Up     int  `json:"up"`
	Down   int  `json:"down"`
	IsUp   bool `json:"is_up"`
	IsDown bool `json:"is_down"`
}

// @Id           GetVote
// @Summary      Get Vote Status
// @Description  Get vote status for a specific comment or page
// @Tags         Vote
// @Param        target_name  path  string  true  "The name of vote target"  Enums(comment, page)
// @Param        target_id    path  int     true  "The target comment or page ID"
// @Accept       json
// @Produce      json
// @Success      200  {object}  ResponseVote
// @Failure      400  {object}  Map{msg=string}
// @Failure      403  {object}  Map{msg=string}
// @Failure      404  {object}  Map{msg=string}
// @Failure      500  {object}  Map{msg=string}
// @Router       /votes/{target_name}/{target_id}  [get]
func VoteGet(app *core.App, router fiber.Router) {
	router.Get("/votes/:target_name/:target_id", func(c *fiber.Ctx) error {
		targetName := c.Params("target_name")
		targetID, err := c.ParamsInt("target_id")
		if err != nil || targetID <= 0 {
			return common.RespError(c, 400, "invalid vote target id")
		}

		switch targetName {
		case "comment":
			if app.Dao().FindComment(uint(targetID)).IsEmpty() {
				return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Comment")}))
			}
		case "page":
			if app.Dao().FindPageByID(uint(targetID)).IsEmpty() {
				return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Page")}))
			}
		default:
			return common.RespError(c, 404, "unknown vote target name")
		}

		var result ResponseVote
		result.Up, result.Down = app.Dao().GetVoteNumUpDown(targetName, uint(targetID))
		exitsVotes := getExistsVotesByIP(app.Dao(), c.IP(), targetName, uint(targetID))
		if len(exitsVotes) > 0 {
			choice := getVoteChoice(string(exitsVotes[0].Type))
			result.IsUp = choice == "up"
			result.IsDown = choice == "down"
		}

		return common.RespData(c, result)
	})
}

type ParamsVoteCreate struct {
	Name  string `json:"name" validate:"optional"`  // The username
	Email string `json:"email" validate:"optional"` // The user email
}

// @Id           CreateVote
// @Summary      Create Vote
// @Description  Create a new vote for a specific comment or page
// @Tags         Vote
// @Param        target_name  path  string            true  "The name of vote target"  Enums(comment, page)
// @Param        target_id    path  int               true  "The target comment or page ID"
// @Param        choice       path  string            true  "The vote choice"          Enums(up, down)
// @Param        vote         body  ParamsVoteCreate  true  "The vote data"
// @Accept       json
// @Produce      json
// @Success      200  {object}  ResponseVote
// @Failure      403  {object}  Map{msg=string}
// @Failure      404  {object}  Map{msg=string}
// @Failure      500  {object}  Map{msg=string}
// @Router       /votes/{target_name}/{target_id}/{choice}  [post]
func VoteCreate(app *core.App, router fiber.Router) {
	router.Post("/votes/:target_name/:target_id/:choice", common.LimiterGuard(app, func(c *fiber.Ctx) error {
		targetName := c.Params("target_name")
		targetID, _ := c.ParamsInt("target_id")
		choice := c.Params("choice")
		ip := c.IP()

		var p ParamsVoteCreate
		if isOK, resp := common.ParamsDecode(c, &p); !isOK {
			return resp
		}

		if choice != "up" && choice != "down" {
			return common.RespError(c, 404, "unknown vote choice")
		}

		// Find the target model
		var (
			comment entity.Comment
			page    entity.Page
			user    entity.User
		)

		switch targetName {
		case "comment":
			comment = app.Dao().FindComment(uint(targetID))
			if comment.IsEmpty() {
				return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Comment")}))
			}
		case "page":
			page = app.Dao().FindPageByID(uint(targetID))
			if page.IsEmpty() {
				return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": i18n.T("Page")}))
			}
		default:
			return common.RespError(c, 404, "unknown vote target name")
		}

		// Find user
		if p.Name != "" && p.Email != "" {
			var err error
			user, err = app.Dao().FindCreateUser(p.Name, p.Email, "")
			if err != nil {
				return common.RespError(c, 500, "Failed to create user")
			}
		}

		result, newUp, err := updateVote(app.Dao(), createNewVoteParams{
			ip:         ip,
			ua:         string(c.Request().Header.UserAgent()),
			userID:     user.ID,
			targetName: targetName,
			targetID:   uint(targetID),
			choice:     choice,
		})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				target := i18n.T("Comment")
				if targetName == "page" {
					target = i18n.T("Page")
				}
				return common.RespError(c, 404, i18n.T("{{name}} not found", Map{"name": target}))
			}
			return common.RespError(c, 500, "Failed to update vote")
		}

		// 只有"新增一条 up 投票"才结算积分。newUp 由事务内判定，
		// 取消赞、改成 down、重复切换都不会重复计分。
		if newUp && targetName == "comment" && !comment.IsEmpty() {
			if rewardsService, err := core.AppService[*core.RewardsService](app); err == nil && rewardsService != nil {
				voterKey := voteVoterKey(user.ID, ip)
				go rewardsService.OnVoteReceived(comment.UserID, comment.ID, voterKey)
			}
		}

		return common.RespData(c, result)
	}))
}

// voteVoterKey 生成"谁投了这一票"的去重键。
//
// 优先用登录用户 ID；匿名投票退化为 IP 的哈希，避免在积分流水里
// 明文落 IP。同一个投票者对同一条评论只会计一次分，这是防止
// "取消赞再点赞"刷分的关键。
func voteVoterKey(userID uint, ip string) string {
	if userID != 0 {
		return fmt.Sprintf("u:%d", userID)
	}
	return "ip:" + utils.GetMD5Hash(ip)
}

// VoteChoice is `up` or `down`
func getVoteChoice(voteType string) string {
	choice := strings.TrimPrefix(strings.TrimPrefix(voteType, "comment_"), "page_")
	if choice != "up" && choice != "down" {
		return ""
	}
	return choice
}

// VoteTarget is `comment` or `page`
func getVoteTargetName(voteType string) string {
	return strings.TrimSuffix(strings.TrimSuffix(voteType, "_up"), "_down")
}

func getExistsVotesByIP(dao *dao.Dao, ip string, targetName string, targetID uint) []entity.Vote {
	var existsVotes []entity.Vote
	dao.DB().Where("type LIKE ? AND target_id = ? AND ip = ?", targetName+"%", uint(targetID), ip).Find(&existsVotes)
	return existsVotes
}

type createNewVoteParams struct {
	ip         string
	ua         string
	userID     uint
	targetName string
	targetID   uint
	choice     string
}

// updateVote 更新投票计数。
//
// 第二个返回值表示"本次是否新增了一条针对评论的 up 投票"，供积分结算使用：
// 取消赞、改成 down 都不会让它为 true，因此可以安全地用它作为发分条件。
func updateVote(d *dao.Dao, opts createNewVoteParams) (ResponseVote, bool, error) {
	var (
		result  ResponseVote
		comment entity.Comment
		page    entity.Page
		newUp   bool
	)

	err := d.DB().Transaction(func(tx *gorm.DB) error {
		switch opts.targetName {
		case "comment":
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&comment, opts.targetID).Error; err != nil {
				return fmt.Errorf("lock vote comment: %w", err)
			}
		case "page":
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&page, opts.targetID).Error; err != nil {
				return fmt.Errorf("lock vote page: %w", err)
			}
		default:
			return fmt.Errorf("unknown vote target %q", opts.targetName)
		}

		voteTypes := []entity.VoteType{
			entity.VoteType(opts.targetName + "_up"),
			entity.VoteType(opts.targetName + "_down"),
		}
		var existingVotes []entity.Vote
		if err := tx.Where("type IN ? AND target_id = ? AND ip = ?", voteTypes, opts.targetID, opts.ip).Find(&existingVotes).Error; err != nil {
			return fmt.Errorf("find existing votes: %w", err)
		}

		selectedChoice := opts.choice
		if len(existingVotes) > 0 {
			existingChoice := getVoteChoice(string(existingVotes[0].Type))
			if err := tx.Unscoped().Delete(&existingVotes).Error; err != nil {
				return fmt.Errorf("delete existing votes: %w", err)
			}
			if selectedChoice == existingChoice {
				selectedChoice = ""
			}
		}

		if selectedChoice != "" {
			vote := entity.Vote{
				TargetID: opts.targetID,
				Type:     entity.VoteType(opts.targetName + "_" + selectedChoice),
				UserID:   opts.userID,
				UA:       opts.ua,
				IP:       opts.ip,
			}
			if err := tx.Create(&vote).Error; err != nil {
				return fmt.Errorf("create vote: %w", err)
			}
			// 走到这里说明之前没有 up 投票（否则 selectedChoice 会被置空或改向），
			// 所以这是一次真正的"新增赞"。
			if opts.targetName == "comment" && selectedChoice == "up" {
				newUp = true
			}
		}

		var up, down int64
		if err := tx.Model(&entity.Vote{}).Where("target_id = ? AND type = ?", opts.targetID, voteTypes[0]).Count(&up).Error; err != nil {
			return fmt.Errorf("count up votes: %w", err)
		}
		if err := tx.Model(&entity.Vote{}).Where("target_id = ? AND type = ?", opts.targetID, voteTypes[1]).Count(&down).Error; err != nil {
			return fmt.Errorf("count down votes: %w", err)
		}

		updates := map[string]any{"vote_up": int(up), "vote_down": int(down)}
		switch opts.targetName {
		case "comment":
			comment.VoteUp, comment.VoteDown = int(up), int(down)
			if err := tx.Model(&comment).Updates(updates).Error; err != nil {
				return fmt.Errorf("update comment vote counts: %w", err)
			}
		case "page":
			page.VoteUp, page.VoteDown = int(up), int(down)
			if err := tx.Model(&page).Updates(updates).Error; err != nil {
				return fmt.Errorf("update page vote counts: %w", err)
			}
		}

		result = ResponseVote{
			Up:     int(up),
			Down:   int(down),
			IsUp:   selectedChoice == "up",
			IsDown: selectedChoice == "down",
		}
		return nil
	})
	if err != nil {
		return ResponseVote{}, false, err
	}

	d.CacheAction(func(cache *dao.DaoCache) {
		if opts.targetName == "comment" {
			_ = cache.CommentCacheSave(&comment)
		} else {
			_ = cache.PageCacheSave(&page)
		}
	})

	return result, newUp, nil
}
