package ncmctl

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chaunsin/netease-cloud-music/api"
	"github.com/chaunsin/netease-cloud-music/api/types"
	"github.com/chaunsin/netease-cloud-music/api/weapi"
	"github.com/chaunsin/netease-cloud-music/pkg/database"
	"github.com/chaunsin/netease-cloud-music/pkg/log"
	"github.com/chaunsin/netease-cloud-music/pkg/utils"

	"github.com/spf13/cobra"
)

type PlayIdsOpts struct {
	Ids      string
	IdsFile  string
	DailyMin int64
	DailyMax int64
	RunMin   int64
	RunMax   int64
	GapMin   int64
	GapMax   int64
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
			Example: `  ncmctl playids --ids 3373818852,3373845775`,
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

func (c *PlayIds) addFlags() {
	c.cmd.Flags().StringVar(&c.opts.Ids, "ids", "", "逗号分隔的歌曲 ID 列表")
	c.cmd.Flags().StringVar(&c.opts.IdsFile, "ids-file", "", "包含歌曲 ID 的文件路径")
	c.cmd.Flags().Int64Var(&c.opts.DailyMin, "daily-min", 0, "每日播放目标随机范围的最小值")
	c.cmd.Flags().Int64Var(&c.opts.DailyMax, "daily-max", 0, "每日播放目标随机范围的最大值")
	c.cmd.Flags().Int64Var(&c.opts.RunMin, "run-min", 0, "单次运行播放目标随机范围的最小值")
	c.cmd.Flags().Int64Var(&c.opts.RunMax, "run-max", 0, "单次运行播放目标随机范围的最大值")
	c.cmd.Flags().Int64Var(&c.opts.GapMin, "gap-min", 0, "随机播放间隔秒数的最小值")
	c.cmd.Flags().Int64Var(&c.opts.GapMax, "gap-max", 0, "随机播放间隔秒数的最大值")
}

func (c *PlayIds) execute(ctx context.Context) error {
	// 1. 获取并确定参数配置
	var dailyMin = c.opts.DailyMin
	if dailyMin == 0 && c.root.Cfg.PlayIds != nil {
		dailyMin = c.root.Cfg.PlayIds.DailyMin
	}
	if dailyMin == 0 {
		dailyMin = 50
	}

	var dailyMax = c.opts.DailyMax
	if dailyMax == 0 && c.root.Cfg.PlayIds != nil {
		dailyMax = c.root.Cfg.PlayIds.DailyMax
	}
	if dailyMax == 0 {
		dailyMax = 200
	}

	var runMin = c.opts.RunMin
	if runMin == 0 && c.root.Cfg.PlayIds != nil {
		runMin = c.root.Cfg.PlayIds.RunMin
	}

	var runMax = c.opts.RunMax
	if runMax == 0 && c.root.Cfg.PlayIds != nil {
		runMax = c.root.Cfg.PlayIds.RunMax
	}

	var gapMin = c.opts.GapMin
	if gapMin == 0 && c.root.Cfg.PlayIds != nil {
		gapMin = c.root.Cfg.PlayIds.GapMin
	}
	if gapMin == 0 {
		gapMin = 10
	}

	var gapMax = c.opts.GapMax
	if gapMax == 0 && c.root.Cfg.PlayIds != nil {
		gapMax = c.root.Cfg.PlayIds.GapMax
	}
	if gapMax == 0 {
		gapMax = 30
	}

	// 2. 解析歌曲 ID 池
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
			return fmt.Errorf("读取歌曲ID文件失败: %w", err)
		}
		rawIds = append(rawIds, fileIds...)
	}

	if len(rawIds) == 0 {
		return fmt.Errorf("未指定任何有效的歌曲ID。请使用 --ids 或 --ids-file 参数")
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

	// 3. 网络客户端与登录校验
	cli, err := api.NewClient(c.root.Cfg.Network, c.l)
	if err != nil {
		return fmt.Errorf("创建网络客户端失败: %w", err)
	}
	defer cli.Close(ctx)
	var request = weapi.New(cli)

	user, err := request.GetUserInfo(ctx, &weapi.GetUserInfoReq{})
	if err != nil {
		return fmt.Errorf("验证登录状态失败: %w", err)
	}
	if user.Code != 200 || user.Profile == nil || user.Account == nil {
		return fmt.Errorf("用户未登录或登录态已失效，请使用 ncmctl login 登录")
	}
	var uid = fmt.Sprintf("%v", user.Account.Id)
	c.log("当前账号：uid=%s 昵称=\"%s\"", uid, user.Profile.Nickname)

	// 4. 初始化本地 Badger 数据库
	db, err := database.New(c.root.Cfg.Database)
	if err != nil {
		return fmt.Errorf("本地数据库初始化失败: %w", err)
	}
	defer db.Close(ctx)

	expire, err := utils.TimeUntilMidnight("Local")
	if err != nil {
		return fmt.Errorf("获取午夜过期时间失败: %w", err)
	}

	// 5. 确定今日目标播放上限
	var dailyTarget int64
	targetRecord, err := db.Get(ctx, playIdsDailyTargetKey(uid))
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			// 未设置，随机生成今日目标
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			dailyTarget = dailyMin
			if dailyMax > dailyMin {
				dailyTarget = dailyMin + r.Int63n(dailyMax-dailyMin+1)
			}
			err = db.Set(ctx, playIdsDailyTargetKey(uid), strconv.FormatInt(dailyTarget, 10), expire)
			if err != nil {
				return fmt.Errorf("保存每日播放目标失败: %w", err)
			}
		} else {
			return fmt.Errorf("读取每日目标进度失败: %w", err)
		}
	} else {
		dailyTarget, err = strconv.ParseInt(targetRecord, 10, 64)
		if err != nil {
			return fmt.Errorf("解析每日目标参数错误(%v): %w", targetRecord, err)
		}
	}

	// 读取今日已播放的次数
	var finishedToday int64 = 0
	todayRecord, err := db.Get(ctx, playIdsTodayNumKey(uid))
	if err == nil {
		finishedToday, _ = strconv.ParseInt(todayRecord, 10, 64)
	}

	if finishedToday >= dailyTarget {
		c.log("今日播放任务已达标 (%d/%d)，无需运行，正在退出...", finishedToday, dailyTarget)
		return nil
	}

	leftToday := dailyTarget - finishedToday

	// 6. 确定本次运行的播放目标
	var runTarget int64 = leftToday
	if runMin > 0 || runMax > 0 {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		var target int64 = runMin
		if runMax > runMin {
			target = runMin + r.Int63n(runMax-runMin+1)
		}
		runTarget = target
		if runTarget > leftToday {
			runTarget = leftToday
		}
	}

	c.log("任务开始：歌曲池=%d首，本次目标播放=%d首，今日目标播放=%d首，今日已完成=%d首，今日剩余=%d首，间隔=%ds-%ds", len(uniqueIds), runTarget, dailyTarget, finishedToday, leftToday, gapMin, gapMax)

	// 7. 批量获取歌曲详情（包含5次，每次间隔3秒的重试机制）
	songsDetailMap, err := c.getSongsDetailWithRetry(ctx, request, uniqueIds)
	if err != nil {
		return fmt.Errorf("批量获取歌曲详情失败: %w", err)
	}

	// 打印歌曲池展示
	for idx, id := range uniqueIds {
		if detail, ok := songsDetailMap[id]; ok {
			c.log("歌曲池[%d]: songId=%s 歌名=\"%s\" 时长=%s", idx+1, id, detail.Name, formatDuration(detail.Dt/1000))
		}
	}

	// 8. 构造循环播放队列（每轮随机打乱）
	queue := make([]string, 0, runTarget)
	for int64(len(queue)) < runTarget {
		// 每轮都打乱一次唯一列表
		roundList := shuffleSlice(uniqueIds)
		for _, id := range roundList {
			if int64(len(queue)) >= runTarget {
				break
			}
			// 只加入在 API 详情中能够正常查询到的歌曲
			if _, ok := songsDetailMap[id]; ok {
				queue = append(queue, id)
			}
		}
	}

	if len(queue) == 0 {
		return fmt.Errorf("可供播放的歌曲队列为空，请检查输入的歌曲 ID 是否都有效")
	}

	// 9. 顺序执行播放并间隔静默
	var (
		totalSuccess    int64 = 0
		downloadedSongs       = make(map[string]bool) // 单任务已下载歌曲缓存记录
	)

	for i, songId := range queue {
		songDetail, ok := songsDetailMap[songId]
		if !ok {
			continue
		}

		songTime := songDetail.Dt / 1000 // 毫秒换算成秒
		if songTime <= 0 {
			songTime = 180 // 异常时长兜底
		}

		round := i/len(uniqueIds) + 1
		roundIdx := i%len(uniqueIds) + 1
		c.log("正在播放：第%d/%d首，第%d轮第%d首，songId=%s, 歌名=\"%s\", 时长=%s", i+1, len(queue), round, roundIdx, songId, songDetail.Name, formatDuration(songTime))

		// 网易云音乐 API WebLog 请求构造
		sourceId := strconv.FormatInt(songDetail.Al.Id, 10)
		source := "album"
		if songDetail.Al.Id == 0 {
			source = "toplist"
			sourceId = ""
		}

		// === 第三阶段：请求 SongPlayerV1 并根据单任务缓存进行下载模拟 ===
		songIdInt, err := strconv.ParseInt(songId, 10, 64)
		if err != nil {
			c.log("[ERROR] [%d/%d] 解析歌曲 ID 失败: %s", i+1, len(queue), err)
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
			c.log("[WARN] [%d/%d] 调用 SongPlayerV1 失败: %s", i+1, len(queue), err)
		} else if playerResp.Code == 200 && len(playerResp.Data) > 0 {
			songUrl := playerResp.Data[0].Url
			if songUrl != "" {
				if !downloadedSongs[songId] {
					c.log("开始拉取资源：songId=%s", songId)
					downloadStart := time.Now()
					err = downloadAudioToBuffer(ctx, cli, songUrl)
					downloadDuration = time.Since(downloadStart)
					if err != nil {
						c.log("[WARN] [%d/%d] 下载音频失败: %s", i+1, len(queue), err)
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
		// 为了百分之百真实模拟客户端，等待播放时长必须等于歌曲的完整时长，下载耗时仅作记录，不应从播放时长中扣除
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
			c.log("[ERROR] [%d/%d] 发送播放动作失败: %s", i+1, len(queue), err)
			continue
		}
		if resp.Code != 200 {
			c.log("[ERROR] [%d/%d] 网易云接口返回播放失败，响应数据: %+v", i+1, len(queue), resp)
			time.Sleep(1 * time.Second)
			continue
		}

		// 播放成功记录
		totalSuccess++
		finishedToday++
		c.log("播放上报成功：songId=%s, 上报时长=%ds", songId, songTime)
		c.log("本首结果：第%d/%d首，成功，songId=%s, 歌名=\"%s\"", i+1, len(queue), songId, songDetail.Name)

		// 记录数据库日志
		if err := db.Set(ctx, playIdsRecordKey(uid, songId), fmt.Sprintf("%v", time.Now().UnixMilli())); err != nil {
			c.log("[WARN] 保存歌曲播放记录 %s 至本地数据库失败: %s", songId, err)
		}
		_, err = db.Increment(ctx, playIdsTodayNumKey(uid), 1, expire)
		if err != nil {
			c.log("[WARN] 自增今日已完成次数失败: %s", err)
		}

		// 随机静默间隔
		if i < len(queue)-1 {
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			var gapSeconds = gapMin
			if gapMax > gapMin {
				gapSeconds = gapMin + r.Int63n(gapMax-gapMin+1)
			}
			c.sleepWithProgress(ctx, "播放间隔", time.Duration(gapSeconds)*time.Second)
		}
	}

	c.log("本次实际运行总上报数: %d，成功: %d", len(queue), totalSuccess)
	return nil
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

func shuffleSlice(slice []string) []string {
	shuffled := make([]string, len(slice))
	copy(shuffled, slice)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled
}

func parseIdsFromFile(filePath string) ([]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
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
