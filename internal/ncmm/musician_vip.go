// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/api/eapi"
	"github.com/3899/ncmm/config"
	"github.com/3899/ncmm/pkg/log"
	"github.com/3899/ncmm/pkg/utils"

	"github.com/spf13/cobra"
)

type MusicianVip struct {
	root *Root
	cmd  *cobra.Command
	l    *log.Logger
	rng  *rand.Rand
}

func NewMusicianVip(root *Root, l *log.Logger) *MusicianVip {
	c := &MusicianVip{
		root: root,
		l:    l,
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
		cmd: &cobra.Command{
			Use:     "musician-vip",
			Short:   "[need login] Auto-complete musician VIP tasks (publish notes + playids)",
			Example: "  ncmm musician-vip",
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

func (c *MusicianVip) validate() error {
	if c.root.Cfg.MusicianVip == nil {
		return fmt.Errorf("musicianVip config is not set in config.yaml")
	}
	return nil
}

func (c *MusicianVip) Add(command ...*cobra.Command) {
	c.cmd.AddCommand(command...)
}

func (c *MusicianVip) Command() *cobra.Command {
	return c.cmd
}

func (c *MusicianVip) execute(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	cli, err := api.NewClient(c.root.Cfg.Network, c.l)
	if err != nil {
		return fmt.Errorf("NewClient: %w", err)
	}
	defer cli.Close(ctx)

	// 设置cookie
	if err := loadCookies(cli, c.root.Cfg.Network); err != nil {
		log.Warn("[musician-vip] load cookies err: %s", err)
	}

	eapiCli := eapi.New(cli)

	// 获取音乐人黑胶会员任务状态
	c.cmd.Println("[musician-vip] 检查任务状态...")
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

	c.cmd.Printf("[musician-vip] ✅ 已认证音乐人 | 维持天数: %d 天 | 近30天播放: %d 次 | 解锁VIP权益: %v\n",
		resp.Data.MaintainDays, resp.Data.RecentPlayCount30, resp.Data.UnlockVipRight)

	if resp.Data.FurtherTask == nil {
		c.cmd.Println("[musician-vip] 没有进阶任务")
		return nil
	}

	c.cmd.Printf("[musician-vip] 进阶任务进度: %d/%d 完成\n",
		resp.Data.FurtherTask.ProgressRate, resp.Data.FurtherTask.TotalCompleteNum)

	// 遍历子任务，检查并执行
	for _, sub := range resp.Data.FurtherTask.Children {
		// 播放任务使用 recentPlayCount30 作为实际进度（服务端子任务 progressRate 可能不准确）
		progress := sub.ProgressRate
		if sub.MissionCode == "mission_code_recently_play_count" {
			progress = resp.Data.RecentPlayCount30
		}
		c.cmd.Printf("[musician-vip] 任务: %s — 状态: %d, 进度: %d/%d\n",
			sub.Name, sub.MissionStatus, progress, sub.TotalCompleteNum)

		// 任务已完成则跳过
		if sub.MissionStatus == 100 {
			c.cmd.Printf("[musician-vip] ✅ 任务已完成: %s\n", sub.Name)
			continue
		}

		// 根据任务类型执行
		switch sub.MissionCode {
		case "mission_code_musician_notebook_publish":
			// 发布图文笔记任务
			if err := c.handleNoteTask(ctx, cli, eapiCli, sub); err != nil {
				log.Error("[musician-vip] 笔记任务执行失败: %s", err)
				c.cmd.Printf("[musician-vip] ❌ 笔记任务失败: %s\n", err)
			}

		case "mission_code_recently_play_count":
			// 播放任务
			if err := c.handlePlayTask(ctx, cli, sub, resp.Data.RecentPlayCount30); err != nil {
				log.Error("[musician-vip] 播放任务执行失败: %s", err)
				c.cmd.Printf("[musician-vip] ❌ 播放任务失败: %s\n", err)
			}

		default:
			c.cmd.Printf("[musician-vip] ⚠️ 未知任务类型: %s\n", sub.MissionCode)
		}
	}

	return nil
}

// handleNoteTask 处理发布图文笔记任务
func (c *MusicianVip) handleNoteTask(ctx context.Context, cli *api.Client, eapiCli *eapi.Api, sub eapi.MusicianVipSubTask) error {
	c.cmd.Println("[musician-vip] 处理笔记任务...")

	cfg := c.root.Cfg.MusicianVip.Note

	// 检查是否需要发布（进度 < 目标）
	if sub.ProgressRate >= sub.TotalCompleteNum {
		c.cmd.Println("[musician-vip] 笔记任务已完成，无需发布")
		return nil
	}

	// 获取笔记内容 (支持 messagesFile 外部文本拉取与并集合并去重)
	var messages []string
	if len(cfg.Messages) > 0 {
		messages = append(messages, cfg.Messages...)
	}
	if cfg.MessagesFile != "" {
		fileMsgs, err := parseMessagesFromFile(cfg.MessagesFile)
		if err != nil {
			c.cmd.Printf("[musician-vip] [WARN] 读取 messagesFile (%s) 失败: %s，本次将仅使用内置消息库\n", cfg.MessagesFile, err)
		} else {
			messages = append(messages, fileMsgs...)
		}
	}

	seen := make(map[string]bool)
	var uniqueMessages []string
	for _, m := range messages {
		if !seen[m] {
			seen[m] = true
			uniqueMessages = append(uniqueMessages, m)
		}
	}

	// 获取笔记标题 (支持 titlesFile 外部文本拉取与并集合并去重)
	var titles []string
	if len(cfg.Titles) > 0 {
		titles = append(titles, cfg.Titles...)
	}
	if cfg.TitlesFile != "" {
		fileTitles, err := parseMessagesFromFile(cfg.TitlesFile)
		if err != nil {
			c.cmd.Printf("[musician-vip] [WARN] 读取 titlesFile (%s) 失败: %s，本次将仅使用内置标题库\n", cfg.TitlesFile, err)
		} else {
			titles = append(titles, fileTitles...)
		}
	}

	seenTitles := make(map[string]bool)
	var uniqueTitles []string
	for _, t := range titles {
		if !seenTitles[t] {
			seenTitles[t] = true
			uniqueTitles = append(uniqueTitles, t)
		}
	}

	title := c.getRandomMessage(uniqueTitles)
	if title == "" {
		title = "今日音乐分享"
	}

	msg := c.getRandomMessage(uniqueMessages)
	if msg == "" {
		msg = "分享一首好听的歌~"
	}

	var pics string
	if cfg.Type == 39 {
		// 获取图片URL
		imageURL := c.getRandomImageURL(cfg.ImageURLs)
		if imageURL == "" {
			return fmt.Errorf("没有配置图片URL，请在 config.yaml 的 musicianVip.note.imageUrls 中配置")
		}

		c.cmd.Printf("[musician-vip] 发布图文笔记: 标题=%q, 内容=%q, 图片=%s\n", title, msg, imageURL)

		// 下载图片到临时文件
		tmpFile, err := downloadImageToTemp(ctx, imageURL)
		if err != nil {
			return fmt.Errorf("下载图片失败: %w", err)
		}
		defer os.Remove(tmpFile)

		// 上传图片
		c.cmd.Println("[musician-vip] 上传图片...")
		var errUpload error
		pics, errUpload = eapiCli.EventUploadImage(ctx, tmpFile)
		if errUpload != nil {
			return fmt.Errorf("上传图片失败: %w", errUpload)
		}
		c.cmd.Printf("[musician-vip] 图片上传成功: %s\n", pics)
	} else {
		c.cmd.Printf("[musician-vip] 发布普通笔记: 标题=%q, 内容=%q\n", title, msg)
	}

	// 发布动态
	c.cmd.Println("[musician-vip] 发布动态...")
	resp, err := eapiCli.EventPublish(ctx, &eapi.EventPublishReq{
		Title: title,
		Msg:   msg,
		Type:  "noresource",
		Pics:  pics,
	})
	if err != nil {
		return fmt.Errorf("发布动态失败: %w", err)
	}
	if resp.Code != 200 {
		return fmt.Errorf("发布动态失败: code=%d", resp.Code)
	}

	c.cmd.Printf("[musician-vip] ✅ 笔记发布成功! 动态ID: %d\n", resp.Id)

	// 检查是否自动删除发布后的笔记（默认为开启）
	autoDelete := true
	if cfg.AutoDelete != nil {
		autoDelete = *cfg.AutoDelete
	}

	if autoDelete {
		// 5 ~ 30 秒的随机延迟
		delay := 5 + c.rng.Intn(26)
		c.cmd.Printf("[musician-vip] 等待 %d 秒后执行自动删除...\n", delay)
		time.Sleep(time.Duration(delay) * time.Second)
		respDel, err := eapiCli.EventDelete(ctx, &eapi.EventDeleteReq{
			Id: resp.Id,
		})
		if err != nil {
			c.cmd.Printf("[musician-vip] ⚠️ 自动删除动态失败: %s\n", err)
		} else if respDel.Code != 200 {
			c.cmd.Printf("[musician-vip] ⚠️ 自动删除动态失败: code=%d\n", respDel.Code)
		} else {
			c.cmd.Printf("[musician-vip] 🗑️ 笔记已成功自动删除 (动态ID: %d)\n", resp.Id)
		}
	}
	return nil
}

// handlePlayTask 处理播放任务
func (c *MusicianVip) handlePlayTask(ctx context.Context, cli *api.Client, sub eapi.MusicianVipSubTask, recentPlayCount30 int) error {
	c.cmd.Println("[musician-vip] 处理播放任务...")

	cfg := c.root.Cfg.MusicianVip.Play
	rootPlayCfg := c.root.Cfg.PlayIds

	// 1. 确定并集去重候选歌曲 ID 来源
	var idsSource string
	var idsFileSource string

	if cfg.IDs != "" || cfg.IDsFile != "" {
		idsSource = cfg.IDs
		idsFileSource = cfg.IDsFile
	} else if rootPlayCfg != nil {
		idsSource = rootPlayCfg.IDs
		idsFileSource = rootPlayCfg.IDsFile
	}

	if idsSource == "" && idsFileSource == "" {
		return fmt.Errorf("没有配置任何刷歌歌曲ID，请在 config.yaml 的 musicianVip.play 或 playids 中配置 ids / idsFile")
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
	c.cmd.Printf("[musician-vip] 当前进度: %d/%d, 今日尚缺有效播放: %d 次\n", recentPlayCount30, sub.TotalCompleteNum, neededEffective)
	if neededEffective <= 0 {
		c.cmd.Println("[musician-vip] 播放量已达标，无需刷播")
		return nil
	}

	// 4. 获取执行刷歌的辅助账号池 (secondary)
	var playAccounts []string
	if c.root.Cfg.Accounts != nil && len(c.root.Cfg.Accounts.Secondary) > 0 {
		playAccounts = c.root.Cfg.Accounts.Secondary
	} else if c.root.Cfg.Accounts != nil && c.root.Cfg.Accounts.Primary != "" {
		c.cmd.Println("[musician-vip] [WARN] 未配置辅助账号池 (accounts.secondary)，将直接使用主账号自己为自己刷播")
		playAccounts = []string{c.root.Cfg.Accounts.Primary}
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
			// 限制不超过需要的总量 neededTotal，避免刷多余的歌
			if runTarget > neededTotal {
				runTarget = neededTotal
			}
		}

		c.cmd.Printf("[musician-vip] >>>>>> 分摊任务开始: 选用账号 (%s), 本次需刷总数(含日推): %d 首, 尚缺主歌有效数: %d 首 <<<<<<\n", currentAccount, runTarget, neededEffective)

		// 实例化 PlayIds 服务并传入特定分摊参数
		p := NewPlayIds(c.root, c.l)
		p.opts = PlayIdsOpts{
			Ids:        cfg.IDs,         // 如果有局部覆盖，以局部为准
			IdsFile:    cfg.IDsFile,
			RunMin:     runTarget,
			RunMax:     runTarget,
			GapMin:     gapMin,
			GapMax:     gapMax,
			CookieFile: currentAccount,
		}

		// 执行单账号播放打卡，返回该账号实际成功播放的“主歌”数量
		effectivePlayed, err := p.executeForCookie(ctx, currentAccount, playCandidateIdsSource(cfg.IDs, cfg.IDsFile, rootPlayCfg))
		if err != nil {
			c.cmd.Printf("[musician-vip] [WARN] 账号 (%s) 运行异常: %s，正准备交由下一辅助号...\n", currentAccount, err)
		} else {
			c.cmd.Printf("[musician-vip] 账号 (%s) 播放完成，实际贡献主歌有效上报数: %d 次\n", currentAccount, effectivePlayed)
			neededEffective -= effectivePlayed
		}

		// 切换到下一个辅助号
		currentSecondaryIndex++
	}

	if neededEffective <= 0 {
		c.cmd.Println("[musician-vip] ✅ 经过多个辅助账号的接力刷歌，主账号的播放任务已圆满达标！")
	} else {
		c.cmd.Printf("[musician-vip] ⚠️ 所有配置的辅助账号今天均已达到日风控随机上限，播放任务终止。主号今天仍缺有效播放 %d 次。\n", neededEffective)
	}

	return nil
}

// 辅助工具方法：构建并集歌池传递给 executeForCookie 判定有效主歌上报
func playCandidateIdsSource(vipIds, vipIdsFile string, rootPlayCfg *config.PlayIdsConfig) []string {
	var rawIds []string

	if vipIds != "" || vipIdsFile != "" {
		if vipIds != "" {
			parts := strings.Split(vipIds, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					rawIds = append(rawIds, part)
				}
			}
		}
		if vipIdsFile != "" {
			fileIds, err := parseIdsFromFile(vipIdsFile)
			if err == nil {
				rawIds = append(rawIds, fileIds...)
			} else {
				log.Warn("[musician-vip] 读取 VIP idsFile 失败: %s", err)
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
		if rootPlayCfg.IDsFile != "" {
			fileIds, err := parseIdsFromFile(rootPlayCfg.IDsFile)
			if err == nil {
				rawIds = append(rawIds, fileIds...)
			} else {
				log.Warn("[musician-vip] 读取默认 idsFile 失败: %s", err)
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

// getRandomMessage 随机获取一条消息
func (c *MusicianVip) getRandomMessage(messages []string) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[c.rng.Intn(len(messages))]
}

// getRandomImageURL 随机获取一个图片URL
func (c *MusicianVip) getRandomImageURL(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	return urls[c.rng.Intn(len(urls))]
}

// loadCookies 从配置加载cookie
func loadCookies(cli *api.Client, cfg *api.Config) error {
	if cfg.Cookie.Filepath != "" {
		data, err := os.ReadFile(cfg.Cookie.Filepath)
		if err != nil {
			return fmt.Errorf("read cookie file: %w", err)
		}
		cookies := parseCookieString(string(data))
		if len(cookies) > 0 {
			hasDeviceId := false
			for _, c := range cookies {
				if c.Name == "deviceId" {
					hasDeviceId = true
					break
				}
			}
			if !hasDeviceId {
				cookies = append(cookies, &http.Cookie{
					Name:  "deviceId",
					Value: utils.GenerateDeviceId(),
				})
			}
			url := &neturl.URL{
				Scheme: "https",
				Host:   "music.163.com",
			}
			cli.SetCookies(url, cookies)

			// 同时也设置到 eapi 使用的 interface3 域下，以保证 resty 能自动匹配并发送这些 cookie
			urlEapi := &neturl.URL{
				Scheme: "https",
				Host:   "interface3.music.163.com",
			}
			cli.SetCookies(urlEapi, cookies)

			// 设置给 .music.163.com 通配域名
			urlDot := &neturl.URL{
				Scheme: "https",
				Host:   ".music.163.com",
			}
			cli.SetCookies(urlDot, cookies)
		}
	}
	return nil
}

// parseCookieString 解析cookie字符串
func parseCookieString(s string) []*http.Cookie {
	var cookies []*http.Cookie
	parts := strings.Split(s, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name:  strings.TrimSpace(kv[0]),
			Value: strings.TrimSpace(kv[1]),
		})
	}
	return cookies
}

// downloadImageToTemp 下载图片到临时文件
func downloadImageToTemp(ctx context.Context, url string) (string, error) {
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, "./") {
		return url, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: status=%d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "ncm-img-*.jpg")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.ReadFrom(resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("write file: %w", err)
	}

	return tmpFile.Name(), nil
}

// MusicianVipSubTask 子任务信息
type MusicianVipSubTask struct {
	Name             string
	MissionCode      string
	MissionStatus    int
	ProgressRate     int
	TotalCompleteNum int
}

func toSubTask(sub eapi.MusicianVipSubTask) MusicianVipSubTask {
	return MusicianVipSubTask{
		Name:             sub.Name,
		MissionCode:      sub.MissionCode,
		MissionStatus:    sub.MissionStatus,
		ProgressRate:     sub.ProgressRate,
		TotalCompleteNum: sub.TotalCompleteNum,
	}
}

type MusicianVipConf = config.MusicianVipConf
type MusicianVipNoteConf = config.MusicianVipNoteConf
type MusicianVipPlayConf = config.MusicianVipPlayConf

func parseMessagesFromFile(filePath string) ([]string, error) {
	var data []byte
	var err error
	if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
		resp, err := http.Get(filePath)
		if err != nil {
			return nil, fmt.Errorf("下载远程文件失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("下载远程文件失败，状态码: %d", resp.StatusCode)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("读取远程文件内容失败: %w", err)
		}
	} else {
		data, err = os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
	}
	var list []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		list = append(list, line)
	}
	return list, nil
}

// testNoteTask 用于单独运行图文笔记任务的测试
func (c *MusicianVip) testNoteTask(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	cli, err := api.NewClient(c.root.Cfg.Network, c.l)
	if err != nil {
		return fmt.Errorf("NewClient: %w", err)
	}
	defer cli.Close(ctx)

	// 加载cookie
	if err := loadCookies(cli, c.root.Cfg.Network); err != nil {
		log.Warn("[musician-vip] load cookies err: %s", err)
	}

	eapiCli := eapi.New(cli)

	// 构造虚拟子任务，保证 ProgressRate < TotalCompleteNum 以触发执行
	sub := eapi.MusicianVipSubTask{
		Name:             "发布图文笔记(测试)",
		MissionCode:      "mission_code_musician_notebook_publish",
		MissionStatus:    0,
		ProgressRate:     0,
		TotalCompleteNum: 1,
	}

	return c.handleNoteTask(ctx, cli, eapiCli, sub)
}
