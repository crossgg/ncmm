// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/api/types"
	"github.com/3899/ncmm/api/weapi"
	"github.com/3899/ncmm/pkg/database"
	"github.com/3899/ncmm/pkg/log"
	"github.com/3899/ncmm/pkg/utils"

	"github.com/spf13/cobra"
)

type PlayIdsOpts struct {
	Ids            string
	IdsFile        string
	DailyMin       int64
	DailyMax       int64
	RunMin         int64
	RunMax         int64
	GapMin         int64
	GapMax         int64
	CookieFile     string
	DisableMixPlay bool
}

type PlayIds struct {
	root *Root
	cmd  *cobra.Command
	opts PlayIdsOpts
	l    *log.Logger
}

func NewPlayIds(root *Root, l *log.Logger) *PlayIds {
	c := &PlayIds{
		root: root,
		l:    l,
		cmd: &cobra.Command{
			Use:     "playids",
			Short:   "[need login] 播放指定的歌曲 ID 列表",
			Example: `  ncmm playids --ids 3373818852,3373845775`,
		},
	}
	c.addFlags()
	c.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return c.execute(cmd.Context())
	}
	return c
}

func (c *PlayIds) Command() *cobra.Command {
	return c.cmd
}

func (c *PlayIds) log(format string, args ...interface{}) {
	now := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("[%s] [playids] %s\n", now, msg)
	log.Info("[playids] %s", msg)
}

func formatDuration(seconds int64) string {
	m := seconds / 60
	s := seconds % 60
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatDurationMs(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func (c *PlayIds) sleepWithProgress(ctx context.Context, label string, duration time.Duration) {
	totalSeconds := int64(duration.Seconds())
	if totalSeconds <= 0 {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	start := time.Now()
	// 首次打印
	fmt.Printf("[%s] [playids] %s: 0s/%s (0%%) [%s]",
		time.Now().Format("2006-01-02 15:04:05"),
		label,
		formatDuration(totalSeconds),
		strings.Repeat(" ", 20),
	)

	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			return
		case <-ticker.C:
			elapsed := int64(time.Since(start).Seconds())
			if elapsed >= totalSeconds {
				elapsed = totalSeconds
			}
			pct := float64(elapsed) / float64(totalSeconds) * 100

			barLength := 20
			filledLength := int(float64(barLength) * (float64(elapsed) / float64(totalSeconds)))
			if filledLength > barLength {
				filledLength = barLength
			}
			bar := strings.Repeat("=", filledLength) + strings.Repeat(" ", barLength-filledLength)

			fmt.Printf("\r[%s] [playids] %s: %s/%s (%.0f%%) [%s]",
				time.Now().Format("2006-01-02 15:04:05"),
				label,
				formatDuration(elapsed),
				formatDuration(totalSeconds),
				pct,
				bar,
			)

			if elapsed >= totalSeconds {
				fmt.Println()
				return
			}
		}
	}
}

func (c *PlayIds) validate() error {
	return nil
}

func (c *PlayIds) addFlags() {
	c.cmd.Flags().StringVar(&c.opts.Ids, "ids", "", "逗号分隔的歌曲 ID 列表")
	c.cmd.Flags().StringVar(&c.opts.IdsFile, "ids-file", "", "包含歌曲 ID 的文件路径")
	c.cmd.Flags().Int64Var(&c.opts.DailyMin, "daily-min", 0, "每日播放目标随机范围的最小值")
	c.cmd.Flags().Int64Var(&c.opts.DailyMax, "daily-max", 0, "每日播放目标随机范围的最大值")
	c.cmd.Flags().Int64Var(&c.opts.RunMin, "run-min", 0, "单次运行播放目标随机范围的最小值")
	c.cmd.Flags().Int64Var(&c.opts.RunMax, "run-max", 0, "单次运行播放目标随机范围的最大值")
	c.cmd.Flags().Int64Var(&c.opts.GapMin, "gap-min", 0, "随机播放间隔秒数的最小值")
	c.cmd.Flags().Int64Var(&c.opts.GapMax, "gap-max", 0, "随机播放间隔秒数的最大值")
	c.cmd.Flags().StringVar(&c.opts.CookieFile, "cookie-file", "", "指定额外的 cookie 文件路径")
}

func (c *PlayIds) execute(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	// 1. 并集去重歌曲 ID 池解析
	var rawIds []string

	// A. 收集命令行参数指定的歌曲 ID
	if c.opts.Ids != "" {
		parts := strings.Split(c.opts.Ids, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				rawIds = append(rawIds, part)
			}
		}
	}
	if c.opts.IdsFile != "" {
		fileIds, err := parseIdsFromFile(c.opts.IdsFile)
		if err != nil {
			c.log("[WARN] 读取命令行 --ids-file (%s) 失败: %s，该来源将被跳过", c.opts.IdsFile, err)
		} else {
			rawIds = append(rawIds, fileIds...)
		}
	}

	// B. 并集收集配置文件中的歌曲 ID
	if c.root.Cfg.PlayIds != nil {
		if c.root.Cfg.PlayIds.IDs != "" {
			parts := strings.Split(c.root.Cfg.PlayIds.IDs, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					rawIds = append(rawIds, part)
				}
			}
		}
		for _, file := range uniqueStrings(c.root.Cfg.PlayIds.IDsFile) {
			if file != "" {
				fileIds, err := parseIdsFromFile(file)
				if err != nil {
					c.log("[WARN] 读取配置文件 playids.idsFile (%s) 失败: %s，该来源将被跳过", file, err)
				} else {
					rawIds = append(rawIds, fileIds...)
				}
			}
		}
	}

	if len(rawIds) == 0 {
		return fmt.Errorf("未指定任何有效的歌曲ID。请使用 --ids/--ids-file 传参，或在 config.yaml 默认配置中指定")
	}

	// 去重
	uniqueIds := make([]string, 0)
	seen := make(map[string]bool)
	for _, id := range rawIds {
		if !seen[id] {
			seen[id] = true
			uniqueIds = append(uniqueIds, id)
		}
	}

	// 2. 确定执行播放的账号文件列表
	var executeQueue []string

	if c.opts.CookieFile != "" {
		// 命令行显式指定了 --cookie-file，仅针对该账号运行
		executeQueue = append(executeQueue, c.opts.CookieFile)
	} else {
		// 未指定参数，根据配置文件开关选取账号
		cfg := c.root.Cfg
		if cfg.Accounts == nil {
			return fmt.Errorf("配置文件中缺少 accounts 账号节点")
		}
		if cfg.PlayIds != nil && cfg.PlayIds.EnableMain && cfg.Accounts.Main != "" {
			executeQueue = append(executeQueue, cfg.Accounts.Main)
		} else {
			if cfg.PlayIds != nil && !cfg.PlayIds.EnableMain {
				c.log("提示: 主账号模拟播放已在配置文件中关闭 (enableMain = false)")
			} else if cfg.Accounts.Main == "" {
				c.log("提示: 主账号模拟播放未执行，因为未配置主账号 (accounts.main)")
			}
		}
		if cfg.PlayIds != nil && cfg.PlayIds.EnableSecondaries && len(cfg.Accounts.Secondary) > 0 {
			executeQueue = append(executeQueue, cfg.Accounts.Secondary...)
		} else {
			if cfg.PlayIds != nil && !cfg.PlayIds.EnableSecondaries {
				c.log("提示: 辅助账号模拟播放已在配置文件中关闭 (enableSecondaries = false)")
			} else if len(cfg.Accounts.Secondary) == 0 {
				c.log("提示: 辅助账号模拟播放未执行，因为未配置辅助账号 (accounts.secondary)")
			}
		}
	}

	if len(executeQueue) == 0 {
		c.log("未启用或未配置任何账号执行模拟播放打卡，请检查 config.yaml")
		return nil
	}

	// 3. 依次对账号执行播放打卡
	for _, cookieFile := range executeQueue {
		c.log(">>>>>> 开始为账号 (%s) 执行模拟播放 <<<<<<", cookieFile)
		if _, err := c.executeForCookie(ctx, cookieFile, uniqueIds); err != nil {
			c.log("[ERROR] 账号 (%s) 模拟播放失败: %s", cookieFile, err)
		}
		c.log("--------------------------------------------------\n")
	}

	return nil
}

func (c *PlayIds) RunForCookie(ctx context.Context, cookieFile string) error {
	var rawIds []string
	if c.opts.Ids != "" {
		parts := strings.Split(c.opts.Ids, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				rawIds = append(rawIds, part)
			}
		}
	}
	if c.opts.IdsFile != "" {
		fileIds, err := parseIdsFromFile(c.opts.IdsFile)
		if err != nil {
			c.log("[WARN] 读取命令行 --ids-file (%s) 失败: %s，该来源将被跳过", c.opts.IdsFile, err)
		} else {
			rawIds = append(rawIds, fileIds...)
		}
	}
	if c.root.Cfg.PlayIds != nil {
		if c.root.Cfg.PlayIds.IDs != "" {
			parts := strings.Split(c.root.Cfg.PlayIds.IDs, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					rawIds = append(rawIds, part)
				}
			}
		}
		for _, file := range uniqueStrings(c.root.Cfg.PlayIds.IDsFile) {
			if file != "" {
				fileIds, err := parseIdsFromFile(file)
				if err != nil {
					c.log("[WARN] 读取配置文件 playids.idsFile (%s) 失败: %s，该来源将被跳过", file, err)
				} else {
					rawIds = append(rawIds, fileIds...)
				}
			}
		}
	}
	if len(rawIds) == 0 {
		return fmt.Errorf("未指定任何有效的歌曲ID。请使用 --ids/--ids-file 传参，或在 config.yaml 默认配置中指定")
	}
	uniqueIds := make([]string, 0)
	seen := make(map[string]bool)
	for _, id := range rawIds {
		if !seen[id] {
			seen[id] = true
			uniqueIds = append(uniqueIds, id)
		}
	}
	c.log(">>>>>> 开始为账号 (%s) 执行模拟播放 <<<<<<", cookieFile)
	if _, err := c.executeForCookie(ctx, cookieFile, uniqueIds); err != nil {
		c.log("[ERROR] 账号 (%s) 模拟播放失败: %s", cookieFile, err)
		return err
	}
	c.log("--------------------------------------------------\n")
	return nil
}


func (c *PlayIds) executeForCookie(ctx context.Context, cookieFile string, uniqueIds []string) (int64, error) {
	// 1. 网络客户端与登录校验
	networkCfg := c.root.Cfg.Network
	absPath, err := filepath.Abs(cookieFile)
	if err != nil {
		return 0, fmt.Errorf("解析 cookie 文件路径失败: %w", err)
	}

	// 复制并覆盖 Cookie 文件路径
	networkCfgCopy := *networkCfg
	networkCfgCopy.Cookie.Filepath = absPath
	networkCfg = &networkCfgCopy

	cli, err := api.NewClient(networkCfg, c.l)
	if err != nil {
		return 0, fmt.Errorf("创建网络客户端失败: %w", err)
	}
	defer cli.Close(ctx)
	var request = weapi.New(cli)

	user, err := request.GetUserInfo(ctx, &weapi.GetUserInfoReq{})
	if err != nil {
		return 0, fmt.Errorf("验证登录状态失败: %w", err)
	}
	if user.Code != 200 || user.Profile == nil || user.Account == nil {
		return 0, fmt.Errorf("用户未登录或登录态已失效")
	}
	var uid = fmt.Sprintf("%v", user.Account.Id)
	c.log("当前账号：uid=%s 昵称=\"%s\"", uid, user.Profile.Nickname)

	// 2. 初始化本地 Badger 数据库
	db, err := database.New(c.root.Cfg.Database)
	if err != nil {
		return 0, fmt.Errorf("本地数据库初始化失败: %w", err)
	}
	defer db.Close(ctx)

	syncSessionConfig(ctx, cli, cookieFile, user.Account.Id, db, nil)

	expire, err := utils.TimeUntilMidnight("Local")
	if err != nil {
		return 0, fmt.Errorf("获取午夜过期时间失败: %w", err)
	}

	// 3. 确定今日目标播放上限 (根据 daily_min 和 daily_max 生成)
	var dailyTarget int64
	targetRecord, err := db.Get(ctx, playIdsDailyTargetKey(uid))
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			// 未设置，随机生成今日目标
			dailyMin := c.opts.DailyMin
			if dailyMin == 0 && c.root.Cfg.PlayIds != nil {
				dailyMin = c.root.Cfg.PlayIds.DailyMin
			}
			if dailyMin == 0 {
				dailyMin = 50
			}

			dailyMax := c.opts.DailyMax
			if dailyMax == 0 && c.root.Cfg.PlayIds != nil {
				dailyMax = c.root.Cfg.PlayIds.DailyMax
			}
			if dailyMax == 0 {
				dailyMax = 200
			}

			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			if dailyMax > dailyMin {
				dailyTarget = dailyMin + r.Int63n(dailyMax-dailyMin+1)
			} else {
				dailyTarget = dailyMin
			}
			err = db.Set(ctx, playIdsDailyTargetKey(uid), strconv.FormatInt(dailyTarget, 10), expire)
			if err != nil {
				c.log("[WARN] 写入今日随机目标失败: %s", err)
			}
		} else {
			return 0, fmt.Errorf("读取今日播放目标失败: %w", err)
		}
	} else {
		dailyTarget, _ = strconv.ParseInt(string(targetRecord), 10, 64)
	}

	// 4. 读取今日已播数量
	var dailyCompleted int64 = 0
	completedRecord, err := db.Get(ctx, playIdsTodayNumKey(uid))
	if err == nil {
		dailyCompleted, _ = strconv.ParseInt(string(completedRecord), 10, 64)
	}

	c.log("今日风控目标: 已完成=%d首, 今日随机上限=%d首", dailyCompleted, dailyTarget)
	if dailyCompleted >= dailyTarget {
		c.log("今日已播放总数达到上限 (%d首)，优雅退出播放打卡", dailyTarget)
		return 0, nil
	}

	// 5. 确定本次单次运行的播放目标数 (runTarget)
	runMin := c.opts.RunMin
	if runMin == 0 && c.root.Cfg.PlayIds != nil {
		runMin = c.root.Cfg.PlayIds.RunMin
	}
	runMax := c.opts.RunMax
	if runMax == 0 && c.root.Cfg.PlayIds != nil {
		runMax = c.root.Cfg.PlayIds.RunMax
	}

	var runTarget int64
	if runMin == 0 && runMax == 0 {
		// 单次不设限，直到完成今日上限
		runTarget = dailyTarget - dailyCompleted
	} else {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		if runMax > runMin {
			runTarget = runMin + r.Int63n(runMax-runMin+1)
		} else {
			runTarget = runMin
		}
		// 校验是否超过今日剩余
		if runTarget > (dailyTarget - dailyCompleted) {
			runTarget = dailyTarget - dailyCompleted
		}
	}

	if runTarget <= 0 {
		c.log("本次运行剩余播放目标为 0，无需播放")
		return 0, nil
	}

	// 6. 批量获取候选歌曲详情
	songsDetailMap, err := c.getSongsDetailWithRetry(ctx, request, uniqueIds)
	if err != nil {
		return 0, fmt.Errorf("批量获取歌曲详情失败: %w", err)
	}

	// 7. 跳过听歌账号本人歌曲的比对过滤
	var validCandidateIds []string
	for _, id := range uniqueIds {
		detail, ok := songsDetailMap[id]
		if !ok {
			continue
		}

		isSelf := false
		for _, ar := range detail.Ar {
			if ar.Name == user.Profile.Nickname {
				isSelf = true
				break
			}
		}
		if detail.DjId > 0 && detail.DjId == user.Account.Id {
			isSelf = true
		}

		if isSelf {
			c.log("⚠️ 检测到候选歌曲 \"%s\" (ID: %s) 歌手或上传者为当前听歌账号本人，自动跳过该首候选歌曲", detail.Name, id)
			continue
		}
		validCandidateIds = append(validCandidateIds, id)
	}

	if len(validCandidateIds) == 0 {
		c.log("没有可播放的候选歌曲（全部为本人歌曲或详情获取失败），无需播放")
		return 0, nil
	}

	// 8. 掺杂混入网易云日推干扰风控策略
	var (
		allRecommendIds    []string
		targetNumRecommend int
	)
	mixCfg := c.root.Cfg.MixPlay

	if !c.opts.DisableMixPlay && mixCfg != nil && mixCfg.Enabled && mixCfg.DailyRecommendRatio > 0 {
		c.log("启用混听干扰风控：自动拉取辅助账号每日推荐...")
		recommendResp, err := request.RecommendSongs(ctx, &weapi.RecommendSongsReq{})
		if err != nil {
			c.log("[WARN] 拉取日推失败，本次播放将仅使用候选歌池: %s", err)
		} else if recommendResp.Code != 200 || len(recommendResp.Data.DailySongs) == 0 {
			c.log("[WARN] 接口返回暂无推荐歌曲，本次播放将仅使用候选歌池")
		} else {
			// 获取日推歌曲 ID 列表
			var recommendIds []string
			for _, item := range recommendResp.Data.DailySongs {
				// 跳过过滤：同样不能播放当前账号本人的日推歌
				isSelf := false
				for _, ar := range item.Ar {
					if ar.Name == user.Profile.Nickname {
						isSelf = true
						break
					}
				}
				if item.DjId > 0 && item.DjId == user.Account.Id {
					isSelf = true
				}
				if isSelf {
					continue
				}
				recommendIds = append(recommendIds, strconv.FormatInt(item.Id, 10))
			}

			// 我们将日推也批量加到 songsDetailMap 缓存中，防止二次查询
			for _, item := range recommendResp.Data.DailySongs {
				songIdStr := strconv.FormatInt(item.Id, 10)
				var artists []types.Artist
				for _, ar := range item.Ar {
					artists = append(artists, types.Artist{
						Id:    ar.Id,
						Name:  ar.Name,
						Tns:   ar.Tns,
						Alias: ar.Alias,
					})
				}
				album := types.Album{
					Id:     item.Al.Id,
					Name:   item.Al.Name,
					PicUrl: item.Al.PicUrl,
					Pic:    item.Al.Pic,
					PicStr: item.Al.PicStr,
					Tns:    item.Al.Tns,
				}
				songsDetailMap[songIdStr] = &weapi.SongDetailRespSongs{
					Id:   item.Id,
					Name: item.Name,
					Dt:   item.Dt,
					Ar:   artists,
					Al:   album,
				}
			}

			// 混入计算：主歌占 (1 - ratio)，日推占 ratio
			ratio := mixCfg.DailyRecommendRatio
			if ratio >= 1.0 {
				ratio = 0.5 // 防止占比大于 100%
			}
			numRecommend := int(float64(len(validCandidateIds)) * ratio / (1.0 - ratio))
			if numRecommend <= 0 {
				numRecommend = 1
			}
			if numRecommend > len(recommendIds) {
				numRecommend = len(recommendIds)
			}

			allRecommendIds = recommendIds
			targetNumRecommend = numRecommend
			c.log("混听风控设置：成功获取到 %d 首可用日推，每轮将随机选出 %d 首掺杂播放", len(allRecommendIds), targetNumRecommend)
		}
	}

	// 9. 确定播放间隔时间
	gapMin := c.opts.GapMin
	if gapMin == 0 && c.root.Cfg.PlayIds != nil {
		gapMin = c.root.Cfg.PlayIds.GapMin
	}
	if gapMin == 0 {
		gapMin = 10
	}
	gapMax := c.opts.GapMax
	if gapMax == 0 && c.root.Cfg.PlayIds != nil {
		gapMax = c.root.Cfg.PlayIds.GapMax
	}
	if gapMax == 0 {
		gapMax = 30
	}

	var (
		totalSuccess     int64 = 0
		candidateSuccess int64 = 0
		downloadedSongs        = make(map[string]bool)
		totalPlayedCount int64 = 0
		round            int   = 1
	)

	c.log("播放任务启动：本次目标刷播=%d首, 今日已播累计=%d首, 今日随机上限=%d首", runTarget, dailyCompleted, dailyTarget)

	// 外层大循环：按轮次播放，直到播完本日单次目标 runTarget
	for totalPlayedCount < runTarget {
		// A. 组装并打乱本轮歌池
		var roundList []string
		if len(allRecommendIds) > 0 && targetNumRecommend > 0 {
			shuffledRecommends := shuffleSlice(allRecommendIds)
			nRec := targetNumRecommend
			if nRec > len(shuffledRecommends) {
				nRec = len(shuffledRecommends)
			}
			selectedRecommends := shuffledRecommends[:nRec]
			roundList = append(roundList, validCandidateIds...)
			roundList = append(roundList, selectedRecommends...)
		} else {
			roundList = append(roundList, validCandidateIds...)
		}

		roundList = shuffleSlice(roundList)

		// 过滤在详情获取中失败或不存在的 ID
		var validRoundList []string
		for _, id := range roundList {
			if _, ok := songsDetailMap[id]; ok {
				validRoundList = append(validRoundList, id)
			}
		}

		if len(validRoundList) == 0 {
			break
		}

		// 限制本轮大小，防止超出剩余需要的总播放数
		remaining := runTarget - totalPlayedCount
		if int64(len(validRoundList)) > remaining {
			validRoundList = validRoundList[:remaining]
		}

		// B. 日志输出打乱后的本轮结果
		c.log("====== 开始第 %d 轮播放 (本轮共 %d 首) ======", round, len(validRoundList))
		for idx, id := range validRoundList {
			if detail, ok := songsDetailMap[id]; ok {
				c.log("  [%d]: songId=%s 歌名=\"%s\" 时长=%s", idx+1, id, detail.Name, formatDuration(detail.Dt/1000))
			}
		}

		// C. 内层小循环：顺序播放当前轮次的每首歌曲
		var isInterrupted = false
		for roundIdx, songId := range validRoundList {
			// 校验当前是否达到了今日最大上限
			var currentCompleted int64 = 0
			currentRecord, err := db.Get(ctx, playIdsTodayNumKey(uid))
			if err == nil {
				currentCompleted, _ = strconv.ParseInt(string(currentRecord), 10, 64)
			}
			if currentCompleted >= dailyTarget {
				c.log("  [playids] ⚠️ 触发今日风控随机播放总上限 (%d首)，优雅退出当前运行", dailyTarget)
				isInterrupted = true
				break
			}

			songDetail := songsDetailMap[songId]
			songTime := songDetail.Dt / 1000
			if songTime <= 0 {
				songTime = 180
			}

			c.log("正在播放：第%d/%d首，第%d轮第%d首，songId=%s, 歌名=\"%s\", 时长=%s", totalPlayedCount+1, runTarget, round, roundIdx+1, songId, songDetail.Name, formatDuration(songTime))

			sourceId := strconv.FormatInt(songDetail.Al.Id, 10)
			source := "album"
			if songDetail.Al.Id == 0 {
				source = "toplist"
				sourceId = ""
			}

			// 请求 SongPlayerV1 并进行下载模拟
			songIdInt, err := strconv.ParseInt(songId, 10, 64)
			if err != nil {
				c.log("[ERROR] [%d/%d] 解析歌曲 ID 失败: %s", totalPlayedCount+1, runTarget, err)
				continue
			}

			playerReq := &weapi.SongPlayerV1Req{
				Ids:   types.IntsString([]int64{songIdInt}),
				Level: types.LevelStandard,
			}

			var downloadDuration time.Duration
			var apiSuccess = false
			var isCached = false
			playerResp, err := request.SongPlayerV1(ctx, playerReq)
			if err != nil {
				c.log("[WARN] [%d/%d] 调用 SongPlayerV1 失败: %s", totalPlayedCount+1, runTarget, err)
			} else if playerResp.Code == 200 && len(playerResp.Data) > 0 {
				songUrl := playerResp.Data[0].Url
				if songUrl != "" {
					if !downloadedSongs[songId] {
						c.log("开始拉取资源：songId=%s", songId)
						downloadStart := time.Now()
						err = downloadAudioToBuffer(ctx, cli, songUrl)
						downloadDuration = time.Since(downloadStart)
						if err != nil {
							c.log("[WARN] [%d/%d] 下载音频失败: %s", totalPlayedCount+1, runTarget, err)
						} else {
							downloadedSongs[songId] = true
							apiSuccess = true
						}
					} else {
						isCached = true
						apiSuccess = true
						downloadDuration = 0
					}
				}
			}

			// 补等待播放时长
			sleepDuration := time.Duration(songTime) * time.Second
			if apiSuccess {
				if isCached {
					c.log("拉取完成：songId=%s, 来源=缓存, 已耗时=0ms, 补等待=%s", songId, formatDuration(int64(sleepDuration.Seconds())))
				} else {
					c.log("拉取完成：songId=%s, 来源=CDN, 已耗时=%s, 补等待=%s", songId, formatDurationMs(downloadDuration), formatDuration(int64(sleepDuration.Seconds())))
				}
			} else {
				c.log("拉取失败：songId=%s, 将直接等待完整时长=%s", songId, formatDuration(songTime))
			}

			// 真实进行播放补等待
			if sleepDuration > 0 {
				c.sleepWithProgress(ctx, "播放进度", sleepDuration)
			}

			var req = &weapi.WebLogReq{CsrfToken: "", Logs: []map[string]interface{}{
				{
					"action": "play",
					"json": map[string]interface{}{
						"type":     "song",
						"wifi":     0,
						"download": 0,
						"id":       songId,
						"time":     songTime,
						"end":      "playend",
						"source":   source,
						"sourceId": sourceId,
						"mainsite": "1",
						"content":  fmt.Sprintf("id=%v", sourceId),
					},
				},
			}}

			resp, err := request.WebLog(ctx, req)
			if err != nil {
				c.log("[ERROR] [%d/%d] 发送播放动作失败: %s", totalPlayedCount+1, runTarget, err)
				continue
			}
			if resp.Code != 200 {
				c.log("[ERROR] [%d/%d] 网易云接口返回播放失败，响应数据: %+v", totalPlayedCount+1, runTarget, resp)
				time.Sleep(1 * time.Second)
				continue
			}

			// 判定这首歌是不是主账号候选歌曲（排除干扰的日推歌曲）
			isCandidate := false
			for _, cid := range validCandidateIds {
				if cid == songId {
					isCandidate = true
					break
				}
			}
			if isCandidate {
				candidateSuccess++
			}

			// 判断本首歌曲是否计入播放进度目标 (若是主歌，或者开启了 countTarget 开关)
			shouldCount := isCandidate
			if !shouldCount && c.root.Cfg.MixPlay != nil {
				shouldCount = c.root.Cfg.MixPlay.CountTarget
			}

			// 播放成功记录
			totalSuccess++
			if shouldCount {
				totalPlayedCount++
			}

			c.log("播放上报成功：songId=%s, 上报时长=%ds", songId, songTime)
			if shouldCount {
				c.log("本首结果：第%d/%d首，成功，songId=%s, 歌名=\"%s\"", totalPlayedCount, runTarget, songId, songDetail.Name)
			} else {
				c.log("本首结果：成功 (日推混听，不占任务额度)，songId=%s, 歌名=\"%s\"", songId, songDetail.Name)
			}

			// 记录数据库日志
			if err := db.Set(ctx, playIdsRecordKey(uid, songId), fmt.Sprintf("%v", time.Now().UnixMilli())); err != nil {
				c.log("[WARN] 保存歌曲播放记录 %s 至本地数据库失败: %s", songId, err)
			}

			// 只有计入目标的歌曲，才去自增今日已完成计数
			if shouldCount {
				_, err = db.Increment(ctx, playIdsTodayNumKey(uid), 1, expire)
				if err != nil {
					c.log("[WARN] 自增今日已完成次数失败: %s", err)
				}
			}

			// 随机静默间隔 (只要不是本次运行的最后一首)
			if totalPlayedCount < runTarget {
				r := rand.New(rand.NewSource(time.Now().UnixNano()))
				var gapSeconds = gapMin
				if gapMax > gapMin {
					gapSeconds = gapMin + r.Int63n(gapMax-gapMin+1)
				}
				c.sleepWithProgress(ctx, "播放间隔", time.Duration(gapSeconds)*time.Second)
			}
		}

		if isInterrupted {
			break
		}
		round++
	}

	c.log("本次实际运行总上报数: %d，成功: %d", totalPlayedCount, totalSuccess)
	return candidateSuccess, nil
}

func (c *PlayIds) getSongsDetailWithRetry(ctx context.Context, request *weapi.Api, songIds []string) (map[string]*weapi.SongDetailRespSongs, error) {
	reqList := make([]weapi.SongDetailReqList, 0, len(songIds))
	for _, id := range songIds {
		reqList = append(reqList, weapi.SongDetailReqList{Id: id, V: 0})
	}

	var details *weapi.SongDetailResp
	var err error

	c.log("正在调用接口获取歌曲详情...")

	for attempt := 1; attempt <= 5; attempt++ {
		details, err = request.SongDetail(ctx, &weapi.SongDetailReq{C: reqList})
		if err == nil && details != nil && details.Code == 200 {
			break
		}
		if attempt < 5 {
			var errMsg = "未知错误"
			if err != nil {
				errMsg = err.Error()
			} else if details != nil {
				errMsg = fmt.Sprintf("API Code: %d", details.Code)
			}
			c.log("[WARN] 获取歌曲详情失败: %s。正在等待 3 秒后重试...", errMsg)
			time.Sleep(3 * time.Second)
		}
	}

	if err != nil || details == nil || details.Code != 200 {
		var detailErr = fmt.Errorf("无异常详情")
		if err != nil {
			detailErr = err
		} else if details != nil {
			detailErr = fmt.Errorf("接口状态码错误: %d", details.Code)
		}
		return nil, fmt.Errorf("获取歌曲详情在 5 次重试后仍然失败: %w", detailErr)
	}

	result := make(map[string]*weapi.SongDetailRespSongs)
	for i := range details.Songs {
		s := &details.Songs[i]
		result[strconv.FormatInt(s.Id, 10)] = s
	}
	return result, nil
}

func parseIdsFromFile(filePath string) ([]string, error) {
	var data []byte
	var err error
	if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
		resp, err := httpClient.Get(filePath)
		if err != nil {
			return nil, fmt.Errorf("下载远程歌曲ID文件失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("下载远程歌曲ID文件失败，状态码: %d", resp.StatusCode)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("读取远程歌曲ID内容失败: %w", err)
		}
	} else {
		data, err = os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
	}
	var ids []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				ids = append(ids, part)
			}
		}
	}
	return ids, nil
}

func downloadAudioToBuffer(ctx context.Context, cli *api.Client, songUrl string) error {
	resp, err := cli.Download(ctx, songUrl, nil, nil, io.Discard, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func playIdsRecordKey(uid string, songId string) string {
	return fmt.Sprintf("playids:record:%v:%v", uid, songId)
}

func playIdsTodayNumKey(uid string) string {
	return fmt.Sprintf("playids:today:%v", uid)
}

func playIdsDailyTargetKey(uid string) string {
	return fmt.Sprintf("playids:target:%v", uid)
}
