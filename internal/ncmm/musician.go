// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"time"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/api/eapi"
	"github.com/3899/ncmm/api/weapi"
	"github.com/3899/ncmm/config"
	"github.com/3899/ncmm/pkg/log"

	"github.com/spf13/cobra"
)

type Musician struct {
	root *Root
	cmd  *cobra.Command
	l    *log.Logger
	rng  *rand.Rand
}

func NewMusician(root *Root, l *log.Logger) *Musician {
	c := &Musician{
		root: root,
		l:    l,
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
		cmd: &cobra.Command{
			Use:     "musician",
			Short:   "[need login] Auto-complete musician daily check-ins and VIP tasks",
			Example: "  ncmm musician",
		},
	}
	c.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return c.execute(cmd.Context())
	}
	c.cmd.AddCommand(&cobra.Command{
		Use:   "note",
		Short: "测试单独发布图文笔记",
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.testNoteTask(cmd.Context())
		},
	})
	return c
}

func (c *Musician) validate() error {
	if c.root.Cfg.Musician == nil {
		return fmt.Errorf("musician config is not set in config.yaml")
	}
	return nil
}

func (c *Musician) Add(command ...*cobra.Command) {
	c.cmd.AddCommand(command...)
}

func (c *Musician) Command() *cobra.Command {
	return c.cmd
}

func (c *Musician) execute(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	cfg := c.root.Cfg
	if cfg.Accounts == nil {
		return fmt.Errorf("配置文件中缺少 accounts 账号节点")
	}

	var hasExecuted bool

	// 1. 主账号执行音乐人任务
	if cfg.Musician != nil && cfg.Musician.EnableMain && cfg.Accounts.Main != "" {
		c.cmd.Printf("[musician] >>>>>> 开始主账号音乐人任务 (%s) <<<<<<\n", cfg.Accounts.Main)
		if err := c.runMusicianForCookie(ctx, cfg.Accounts.Main, true); err != nil {
			c.cmd.Printf("[musician] ❌ 主账号任务失败: %s\n", err)
		}
		hasExecuted = true
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableMain {
			c.cmd.Println("[musician] 提示: 主账号音乐人任务已在配置文件中关闭 (enableMain = false)")
		} else if cfg.Accounts == nil || cfg.Accounts.Main == "" {
			c.cmd.Println("[musician] 提示: 主账号音乐人任务未执行，因为未配置主账号 (accounts.main)")
		}
	}

	// 2. 辅助账号执行音乐人任务
	if cfg.Musician != nil && cfg.Musician.EnableSecondaries && len(cfg.Accounts.Secondary) > 0 {
		for _, secCookie := range cfg.Accounts.Secondary {
			c.cmd.Printf("[musician] >>>>>> 开始辅助账号音乐人任务 (%s) <<<<<<\n", secCookie)
			if err := c.runMusicianForCookie(ctx, secCookie, false); err != nil {
				c.cmd.Printf("[musician] ❌ 辅助账号任务失败: %s\n", err)
			}
			hasExecuted = true
		}
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableSecondaries {
			c.cmd.Println("[musician] 提示: 辅助账号音乐人任务已在配置文件中关闭 (enableSecondaries = false)")
		} else if cfg.Accounts == nil || len(cfg.Accounts.Secondary) == 0 {
			c.cmd.Println("[musician] 提示: 辅助账号音乐人任务未执行，因为未配置辅助账号 (accounts.secondary)")
		}
	}

	if !hasExecuted {
		c.cmd.Println("[musician] 未启用或未配置任何账号进行音乐人任务，请检查 config.yaml")
	} else {
		c.cmd.Println("[musician] 所有音乐人日常与 VIP 任务执行完毕！")
	}
	return nil
}

func (c *Musician) runMusicianForCookie(ctx context.Context, cookieFile string, isPrimary bool) error {
	absPath, err := filepath.Abs(cookieFile)
	if err != nil {
		return fmt.Errorf("解析 cookie 路径失败: %w", err)
	}

	// 复制并重构 Cookie 路径
	networkCfg := *c.root.Cfg.Network
	networkCfg.Cookie.Filepath = absPath

	cli, err := api.NewClient(&networkCfg, c.l)
	if err != nil {
		return fmt.Errorf("实例化客户端失败: %w", err)
	}
	defer cli.Close(ctx)

	eapiCli := eapi.New(cli)

	// 获取音乐人黑胶会员任务状态
	c.cmd.Println("  👉 检查任务状态...")
	resp, err := eapiCli.MusicianVipTasks(ctx, &eapi.MusicianVipTasksReq{ER: false})
	if err != nil {
		return fmt.Errorf("MusicianVipTasks: %w", err)
	}
	if resp.Code != 200 {
		return fmt.Errorf("MusicianVipTasks error: code=%d msg=%s", resp.Code, resp.Message)
	}
	if !resp.Data.IsMusician {
		return fmt.Errorf("当前账号不是音乐人")
	}

	c.cmd.Printf("  ✅ 已认证音乐人 | 维持天数: %d 天 | 近30天播放: %d 次 | 解锁VIP权益: %v\n",
		resp.Data.MaintainDays, resp.Data.RecentPlayCount30, resp.Data.UnlockVipRight)

	// ==================== 第一阶段：音乐人日常任务 ====================
	c.cmd.Println("  👉 [第一阶段] 开始执行音乐人日常任务 (日常签到与云豆领奖)...")
	weapiApi := weapi.New(cli)

	// 1. 音乐人日常签到
	c.cmd.Println("    👉 开始音乐人日常签到...")
	signResp, err := weapiApi.MusicianSign(ctx, &weapi.MusicianSignReq{})
	if err != nil {
		c.cmd.Printf("    ❌ 音乐人日常签到失败: %v\n", err)
	} else if signResp.Code == 200 {
		c.cmd.Println("    ✅ 音乐人日常签到成功")
	} else {
		c.cmd.Printf("    ℹ️ 音乐人日常签到提示: code=%d msg=%s\n", signResp.Code, signResp.Message)
	}

	// 2. 领取音乐人周期/阶段任务云豆奖励
	var allTasks []weapi.MusicianTask
	cycleTasks, err := weapiApi.MusicianTasks(ctx, &weapi.MusicianTasksReq{})
	if err == nil && cycleTasks.Code == 200 {
		allTasks = append(allTasks, cycleTasks.Data.TaskList...)
	}
	stageTasks, err := weapiApi.MusicianTasksNew(ctx, &weapi.MusicianTasksNewReq{})
	if err == nil && stageTasks.Code == 200 {
		allTasks = append(allTasks, stageTasks.Data.TaskList...)
	}

	if len(allTasks) > 0 {
		var claimCount int
		for _, task := range allTasks {
			c.cmd.Printf("    - 任务: %-15s | 状态: %d | 进度: %d/%d\n",
				task.Name, task.Status, task.CurrentProgress, task.TargetWorth)
			if task.Status == 2 || (task.UserMissionId > 0 && task.CurrentProgress >= task.TargetWorth && task.TargetWorth > 0) {
				id := fmt.Sprintf("%d", task.UserMissionId)
				period := fmt.Sprintf("%d", task.Period)
				reward, err := weapiApi.MusicianCloudbeanObtain(ctx, &weapi.MusicianCloudbeanObtainReq{UserMissionId: id, Period: period})
				if err != nil {
					c.cmd.Printf("      ❌ 领取云豆失败 [%s]: %v\n", task.Name, err)
				} else if reward.Code == 200 {
					c.cmd.Printf("      🎉 成功领取云豆奖励 [%s] (UserMissionId: %s)\n", task.Name, id)
					claimCount++
				}
			}
		}
		if claimCount == 0 {
			c.cmd.Println("    ℹ️ 没有可领取的云豆奖励")
		}
	} else {
		c.cmd.Println("    ℹ️ 暂无音乐人周期/阶段任务奖励")
	}

	// ==================== 第二阶段：音乐人 VIP 任务 ====================
	if resp.Data.FurtherTask == nil {
		c.cmd.Println("  👉 [第二阶段] 提示: 没有音乐人 VIP 进阶任务 (跳过)")
		return nil
	}

	c.cmd.Printf("  👉 [第二阶段] 开始执行音乐人 VIP 任务 (发布笔记与接力刷歌)... 进度: %d/%d 完成\n",
		resp.Data.FurtherTask.ProgressRate, resp.Data.FurtherTask.TotalCompleteNum)

	// 遍历子任务，检查并执行
	for _, sub := range resp.Data.FurtherTask.Children {
		progress := sub.ProgressRate
		if sub.MissionCode == "mission_code_recently_play_count" {
			progress = resp.Data.RecentPlayCount30
		}
		c.cmd.Printf("    - 任务: %-15s — 状态: %d, 进度: %d/%d\n",
			sub.Name, sub.MissionStatus, progress, sub.TotalCompleteNum)

		if sub.MissionStatus == 100 {
			c.cmd.Printf("    ✅ 任务已完成: %s\n", sub.Name)
			continue
		}

		switch sub.MissionCode {
		case "mission_code_musician_notebook_publish":
			if sub.ProgressRate >= sub.TotalCompleteNum {
				c.cmd.Println("    ℹ️ 笔记任务已完成，无需发布")
			} else {
				c.cmd.Println("    👉 处理笔记任务...")
				n := NewNote(c.root, c.l)
				_, err := n.ExecuteForCookie(ctx, cookieFile)
				if err != nil {
					log.Error("    ❌ 笔记任务执行失败: %s", err)
					c.cmd.Printf("    ❌ 笔记任务失败: %s\n", err)
				}
			}

		case "mission_code_recently_play_count":
			if err := c.handlePlayTask(ctx, cli, sub, resp.Data.RecentPlayCount30); err != nil {
				log.Error("    ❌ 播放任务执行失败: %s", err)
				c.cmd.Printf("    ❌ 播放任务失败: %s\n", err)
			}

		default:
			c.cmd.Printf("    ⚠️ 未知任务类型: %s\n", sub.MissionCode)
		}
	}

	return nil
}

// handlePlayTask 处理播放任务
func (c *Musician) handlePlayTask(ctx context.Context, cli *api.Client, sub eapi.MusicianVipSubTask, recentPlayCount30 int) error {
	c.cmd.Println("    👉 处理播放任务...")

	cfg := c.root.Cfg.Musician.Play
	rootPlayCfg := c.root.Cfg.PlayIds

	// 1. 确定并集去重候选歌曲 ID 来源
	var idsSource string
	var idsFileSource config.StringOrSlice

	if cfg.IDs != "" || len(cfg.IDsFile) > 0 {
		idsSource = cfg.IDs
		idsFileSource = cfg.IDsFile
	} else if rootPlayCfg != nil {
		idsSource = rootPlayCfg.IDs
		idsFileSource = rootPlayCfg.IDsFile
	}

	if idsSource == "" && len(idsFileSource) == 0 {
		return fmt.Errorf("没有配置任何刷歌歌曲ID，请在 config.yaml 的 musician.play 或 playids 中配置 ids / idsFile")
	}

	// 2. 确定播放时间间隔参数
	gapMin := cfg.GapMin
	if gapMin <= 0 && rootPlayCfg != nil {
		gapMin = rootPlayCfg.GapMin
	}
	if gapMin <= 0 {
		gapMin = 5
	}

	gapMax := cfg.GapMax
	if gapMax <= 0 && rootPlayCfg != nil {
		gapMax = rootPlayCfg.GapMax
	}
	if gapMax <= 0 {
		gapMax = 20
	}

	// 3. 计算还缺少的有效播放数 N
	neededEffective := int64(sub.TotalCompleteNum - recentPlayCount30)
	c.cmd.Printf("    - 当前进度: %d/%d, 今日尚缺有效播放: %d 次\n", recentPlayCount30, sub.TotalCompleteNum, neededEffective)
	if neededEffective <= 0 {
		c.cmd.Println("    ℹ️ 播放量已达标，无需刷播")
		return nil
	}

	// 4. 获取执行刷歌的辅助账号池 (secondary)
	var playAccounts []string
	if c.root.Cfg.Accounts != nil && len(c.root.Cfg.Accounts.Secondary) > 0 {
		playAccounts = c.root.Cfg.Accounts.Secondary
	} else if c.root.Cfg.Accounts != nil && c.root.Cfg.Accounts.Main != "" {
		c.cmd.Println("    [WARN] 未配置辅助账号池 (accounts.secondary)，将直接使用主账号自己为自己刷播")
		playAccounts = []string{c.root.Cfg.Accounts.Main}
	} else {
		return fmt.Errorf("未配置任何账号，请在 config.yaml 的 accounts 节点下配置 primary 或 secondary")
	}

	// 5. 循环分配与上限回滚重平衡算法
	var currentSecondaryIndex = 0

	for neededEffective > 0 && currentSecondaryIndex < len(playAccounts) {
		currentAccount := playAccounts[currentSecondaryIndex]

		// 根据干扰比例 R 计算当前所需总播放数 T = N / 0.7
		ratio := 0.3
		if c.root.Cfg.MixPlay != nil && c.root.Cfg.MixPlay.Enabled {
			ratio = c.root.Cfg.MixPlay.DailyRecommendRatio
		}
		if ratio >= 1.0 {
			ratio = 0.5
		}
		neededTotal := int64(float64(neededEffective) / (1.0 - ratio))
		if neededTotal < neededEffective {
			neededTotal = neededEffective
		}

		// 确立此次刷歌数量 (支持随机与继承关系)
		var runTarget int64
		runMin := cfg.RunMin
		runMax := cfg.RunMax
		if runMin == 0 && runMax == 0 && rootPlayCfg != nil {
			runMin = rootPlayCfg.RunMin
			runMax = rootPlayCfg.RunMax
		}

		if runMin == 0 && runMax == 0 {
			runTarget = neededTotal
		} else {
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			if runMax > runMin {
				runTarget = runMin + r.Int63n(runMax-runMin+1)
			} else {
				runTarget = runMin
			}
			if runTarget > neededTotal {
				runTarget = neededTotal
			}
		}

		c.cmd.Printf("    ⏳ 分摊任务开始: 选用账号 (%s), 本次需刷总数(含日推): %d 首, 尚缺主歌有效数: %d 首\n", currentAccount, runTarget, neededEffective)

		// 实例化 PlayIds 服务并传入特定分摊参数
		p := NewPlayIds(c.root, c.l)
		p.opts = PlayIdsOpts{
			Ids:        cfg.IDs,
			IdsFile:    "",
			RunMin:     runTarget,
			RunMax:     runTarget,
			GapMin:     gapMin,
			GapMax:     gapMax,
			CookieFile: currentAccount,
		}

		// 执行单账号播放打卡，返回该账号实际成功播放的“主歌”数量
		effectivePlayed, err := p.executeForCookie(ctx, currentAccount, playCandidateIdsSource(cfg.IDs, cfg.IDsFile, rootPlayCfg))
		if err != nil {
			c.cmd.Printf("    [WARN] 账号 (%s) 运行异常: %s，正准备交由下一辅助号...\n", currentAccount, err)
		} else {
			c.cmd.Printf("    ✅ 账号 (%s) 播放完成，实际贡献主歌有效上报数: %d 次\n", currentAccount, effectivePlayed)
			neededEffective -= effectivePlayed
		}

		// 切换到下一个辅助号
		currentSecondaryIndex++
	}

	if neededEffective <= 0 {
		c.cmd.Println("    ✅ 经过多个辅助账号的接力刷歌，主账号的播放任务已圆满达标！")
	} else {
		c.cmd.Printf("    ⚠️ 所有配置的辅助账号今天均已达到日风控随机上限，播放任务终止。主号今天仍缺有效播放 %d 次。\n", neededEffective)
	}

	return nil
}

// testNoteTask 用于单独运行图文笔记任务的测试
func (c *Musician) testNoteTask(ctx context.Context) error {
	n := NewNote(c.root, c.l)
	_, err := n.ExecuteForCookie(ctx, c.root.Cfg.Accounts.Main)
	return err
}

// playCandidateIdsSource 构建并集歌池传递给 executeForCookie 判定有效主歌上报
func playCandidateIdsSource(vipIds string, vipIdsFile config.StringOrSlice, rootPlayCfg *config.PlayIdsConfig) []string {
	var rawIds []string

	if vipIds != "" || len(vipIdsFile) > 0 {
		if vipIds != "" {
			parts := strings.Split(vipIds, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					rawIds = append(rawIds, part)
				}
			}
		}
		for _, file := range uniqueStrings(vipIdsFile) {
			if file != "" {
				fileIds, err := parseIdsFromFile(file)
				if err == nil {
					rawIds = append(rawIds, fileIds...)
				} else {
					log.Warn("[musician] 读取 VIP idsFile (%s) 失败: %s", file, err)
				}
			}
		}
	} else if rootPlayCfg != nil {
		if rootPlayCfg.IDs != "" {
			parts := strings.Split(rootPlayCfg.IDs, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					rawIds = append(rawIds, part)
				}
			}
		}
		for _, file := range uniqueStrings(rootPlayCfg.IDsFile) {
			if file != "" {
				fileIds, err := parseIdsFromFile(file)
				if err == nil {
					rawIds = append(rawIds, fileIds...)
				} else {
					log.Warn("[musician] 读取默认 idsFile (%s) 失败: %s", file, err)
				}
			}
		}
	}

	uniqueIds := make([]string, 0)
	seen := make(map[string]bool)
	for _, id := range rawIds {
		if !seen[id] {
			seen[id] = true
			uniqueIds = append(uniqueIds, id)
		}
	}
	return uniqueIds
}
