// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"
	"fmt"
	"strings"

	"github.com/3899/ncmm/pkg/log"

	"github.com/spf13/cobra"
)

type TaskOpts struct {
	Sign         bool
	PlayIds      bool
	MusicianSign bool
	MusicianVip  bool
	Note         bool
	FansGroup    bool
	OnlyFast     bool
	OnlySlow     bool
}

type Task struct {
	root *Root
	cmd  *cobra.Command
	l    *log.Logger
	opts TaskOpts
}

type Account struct {
	Filepath string
	IsMain   bool
}

func NewTask(root *Root, l *log.Logger) *Task {
	c := &Task{
		root: root,
		l:    l,
		cmd: &cobra.Command{
			Use:     "task",
			Short:   "Batch execute configured tasks",
			Example: "  ncmm task\n  ncmm task --sign --playids\n  ncmm task --musician-sign --musician-vip\n  ncmm task --only-fast",
		},
	}
	c.addFlags()
	c.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return c.execute(cmd.Context())
	}
	return c
}

func (c *Task) Command() *cobra.Command {
	return c.cmd
}

func (c *Task) addFlags() {
	c.cmd.Flags().BoolVar(&c.opts.Sign, "sign", false, "执行日常签到任务")
	c.cmd.Flags().BoolVar(&c.opts.PlayIds, "playids", false, "执行播放指定歌曲任务")
	c.cmd.Flags().BoolVar(&c.opts.MusicianSign, "musician-sign", false, "执行音乐人日常签到任务")
	c.cmd.Flags().BoolVar(&c.opts.MusicianVip, "musician-vip", false, "执行音乐人VIP进阶任务")
	c.cmd.Flags().BoolVar(&c.opts.Note, "note", false, "执行笔记发布任务")
	c.cmd.Flags().BoolVar(&c.opts.FansGroup, "fansgroup", false, "执行乐迷团任务")
	c.cmd.Flags().BoolVar(&c.opts.OnlyFast, "only-fast", false, "仅执行快任务组")
	c.cmd.Flags().BoolVar(&c.opts.OnlySlow, "only-slow", false, "仅执行慢任务组")
}

func (c *Task) getAccounts() []Account {
	var accounts []Account
	cfg := c.root.Cfg
	if cfg.Accounts != nil {
		if cfg.Accounts.Main != "" {
			accounts = append(accounts, Account{Filepath: cfg.Accounts.Main, IsMain: true})
		}
		for _, sec := range cfg.Accounts.Secondary {
			if sec != "" {
				accounts = append(accounts, Account{Filepath: sec, IsMain: false})
			}
		}
	}
	return accounts
}

func (c *Task) standardizeActionKey(action string) string {
	keys := []string{
		"VipTask", "Reserve", "ViewVipCenter", "LikeComment", "FollowArtist",
		"LikeSong", "CollectSong", "PublishNote", "ListenIndie", "PlayDailyRecommend",
		"musician-sign", "note", "fansgroup", "playids", "musician-vip",
	}
	for _, k := range keys {
		if strings.EqualFold(k, action) {
			return k
		}
	}
	return ""
}

func (c *Task) isActionActive(stdAction string, activeTasks map[string]bool) bool {
	signSubtasks := []string{
		"VipTask", "Reserve", "ViewVipCenter", "LikeComment", "FollowArtist",
		"LikeSong", "CollectSong", "PublishNote", "ListenIndie", "PlayDailyRecommend",
	}
	for _, sub := range signSubtasks {
		if stdAction == sub {
			return activeTasks["sign"]
		}
	}
	return activeTasks[stdAction]
}

func (c *Task) isActionEnabledForAccount(stdAction string, isMain bool) bool {
	cfg := c.root.Cfg
	actionLower := strings.ToLower(stdAction)

	if actionLower == "note" {
		return isMain
	}

	isSignSubtask := false
	signSubtasks := []string{
		"viptask", "reserve", "viewvipcenter", "likecomment", "followartist",
		"likesong", "collectsong", "publishnote", "listenindie", "playdailyrecommend",
	}
	for _, sub := range signSubtasks {
		if actionLower == sub {
			isSignSubtask = true
			break
		}
	}

	if isSignSubtask || actionLower == "sign" {
		if isMain {
			return cfg.Sign != nil && cfg.Sign.EnableMain
		} else {
			return cfg.Sign != nil && cfg.Sign.EnableSecondaries
		}
	}

	if actionLower == "playids" {
		if isMain {
			return cfg.PlayIds != nil && cfg.PlayIds.EnableMain
		} else {
			return cfg.PlayIds != nil && cfg.PlayIds.EnableSecondaries
		}
	}

	if actionLower == "musician-sign" {
		if isMain {
			return cfg.Musician != nil && cfg.Musician.EnableMain
		} else {
			return cfg.Musician != nil && cfg.Musician.EnableSecondaries
		}
	}

	if actionLower == "musician-vip" {
		if isMain {
			return cfg.Musician != nil && cfg.Musician.EnableMain
		} else {
			return cfg.Musician != nil && cfg.Musician.EnableSecondaries
		}
	}

	if actionLower == "fansgroup" {
		if isMain {
			return cfg.FansGroup != nil && cfg.FansGroup.EnableMain
		} else {
			return cfg.FansGroup != nil && cfg.FansGroup.EnableSecondaries
		}
	}

	return false
}

func (c *Task) executeAction(ctx context.Context, stdAction string, account Account, queue []string, activeTasks map[string]bool, signRunMap map[string]bool) {
	isSignSubtask := false
	signSubtasks := []string{
		"VipTask", "Reserve", "ViewVipCenter", "LikeComment", "FollowArtist",
		"LikeSong", "CollectSong", "PublishNote", "ListenIndie", "PlayDailyRecommend",
	}
	for _, sub := range signSubtasks {
		if stdAction == sub {
			isSignSubtask = true
			break
		}
	}

	if isSignSubtask {
		if !signRunMap[account.Filepath] {
			allowedTasks := make(map[string]bool)
			for _, qItem := range queue {
				stdQItem := c.standardizeActionKey(qItem)
				for _, sub := range signSubtasks {
					if stdQItem == sub && c.isActionActive(stdQItem, activeTasks) {
						allowedTasks[stdQItem] = true
					}
				}
			}
			if len(allowedTasks) > 0 {
				c.cmd.Printf("[task] >>> 账号 (%s) 开始执行 [日常签到] 子任务组 <<<\n", account.Filepath)
				s := NewSign(c.root, c.l)
				if err := s.RunSignForCookie(ctx, account.Filepath, account.IsMain, allowedTasks); err != nil {
					c.cmd.Printf("[task] ❌ 账号 (%s) [日常签到] 执行失败: %s\n", account.Filepath, err)
				} else {
					c.cmd.Printf("[task] ✅ 账号 (%s) [日常签到] 执行完毕\n", account.Filepath)
				}
			}
			signRunMap[account.Filepath] = true
		}
		return
	}

	if stdAction == "playids" {
		c.cmd.Printf("[task] >>> 账号 (%s) 开始执行 [播放指定歌曲] 任务 <<<\n", account.Filepath)
		p := NewPlayIds(c.root, c.l)
		if err := p.RunForCookie(ctx, account.Filepath); err != nil {
			c.cmd.Printf("[task] ❌ 账号 (%s) [播放指定歌曲] 执行失败: %s\n", account.Filepath, err)
		} else {
			c.cmd.Printf("[task] ✅ 账号 (%s) [播放指定歌曲] 执行完毕\n", account.Filepath)
		}
		return
	}

	if stdAction == "musician-sign" {
		c.cmd.Printf("[task] >>> 账号 (%s) 开始执行 [音乐人日常签到] 任务 <<<\n", account.Filepath)
		m := NewMusician(c.root, c.l)
		if err := m.RunSignForCookie(ctx, account.Filepath); err != nil {
			c.cmd.Printf("[task] ❌ 账号 (%s) [音乐人日常签到] 执行失败: %s\n", account.Filepath, err)
		} else {
			c.cmd.Printf("[task] ✅ 账号 (%s) [音乐人日常签到] 执行完毕\n", account.Filepath)
		}
		return
	}

	if stdAction == "musician-vip" {
		c.cmd.Printf("[task] >>> 账号 (%s) 开始执行 [音乐人VIP进阶] 任务 <<<\n", account.Filepath)
		m := NewMusician(c.root, c.l)
		if err := m.RunVipForCookie(ctx, account.Filepath); err != nil {
			c.cmd.Printf("[task] ❌ 账号 (%s) [音乐人VIP进阶] 执行失败: %s\n", account.Filepath, err)
		} else {
			c.cmd.Printf("[task] ✅ 账号 (%s) [音乐人VIP进阶] 执行完毕\n", account.Filepath)
		}
		return
	}

	if stdAction == "note" {
		c.cmd.Printf("[task] >>> 账号 (%s) 开始执行 [发布图文动态] 任务 <<<\n", account.Filepath)
		n := NewNote(c.root, c.l)
		if _, err := n.ExecuteForCookie(ctx, account.Filepath); err != nil {
			c.cmd.Printf("[task] ❌ 账号 (%s) [发布图文动态] 执行失败: %s\n", account.Filepath, err)
		} else {
			c.cmd.Printf("[task] ✅ 账号 (%s) [发布图文动态] 执行完毕\n", account.Filepath)
		}
		return
	}

	if stdAction == "fansgroup" {
		c.cmd.Printf("[task] >>> 账号 (%s) 开始执行 [乐迷团任务] <<<\n", account.Filepath)
		f := NewFansGroup(c.root, c.l)
		if err := f.ExecuteForCookie(ctx, account.Filepath); err != nil {
			c.cmd.Printf("[task] ❌ 账号 (%s) [乐迷团任务] 执行失败: %s\n", account.Filepath, err)
		} else {
			c.cmd.Printf("[task] ✅ 账号 (%s) [乐迷团任务] 执行完毕\n", account.Filepath)
		}
		return
	}
}

func (c *Task) runQueue(ctx context.Context, queue []string, accounts []Account, activeTasks map[string]bool) {
	signRunMap := make(map[string]bool)

	for _, account := range accounts {
		for _, action := range queue {
			stdAction := c.standardizeActionKey(action)
			if stdAction == "" {
				continue
			}

			if !c.isActionActive(stdAction, activeTasks) {
				continue
			}

			if !c.isActionEnabledForAccount(stdAction, account.IsMain) {
				continue
			}

			c.executeAction(ctx, stdAction, account, queue, activeTasks, signRunMap)
		}
	}
}

func (c *Task) execute(ctx context.Context) error {
	hasFlags := c.cmd.Flags().Changed("sign") ||
		c.cmd.Flags().Changed("playids") ||
		c.cmd.Flags().Changed("musician-sign") ||
		c.cmd.Flags().Changed("musician-vip") ||
		c.cmd.Flags().Changed("note") ||
		c.cmd.Flags().Changed("fansgroup")

	var runSign, runPlayIds, runMusicianSign, runMusicianVip, runNote, runFansGroup bool
	cfg := c.root.Cfg

	if hasFlags {
		runSign = c.opts.Sign
		runPlayIds = c.opts.PlayIds
		runMusicianSign = c.opts.MusicianSign
		runMusicianVip = c.opts.MusicianVip
		runNote = c.opts.Note
		runFansGroup = c.opts.FansGroup
	} else {
		if cfg.Task != nil {
			runSign = cfg.Task.Sign
			runPlayIds = cfg.Task.PlayIds
			runMusicianSign = cfg.Task.MusicianSign
			runMusicianVip = cfg.Task.MusicianVip
			runNote = cfg.Task.Note
			runFansGroup = cfg.Task.FansGroup
		} else {
			c.cmd.Println("[task] 提示: 配置文件中未定义 task 节点且未传递任何命令行标志，默认不执行任何任务")
			return nil
		}
	}

	if !runSign && !runPlayIds && !runMusicianSign && !runMusicianVip && !runNote && !runFansGroup {
		c.cmd.Println("[task] 没有需要执行的任务")
		return nil
	}

	activeTasks := make(map[string]bool)
	if runSign {
		activeTasks["sign"] = true
	}
	if runPlayIds {
		activeTasks["playids"] = true
	}
	if runMusicianSign {
		activeTasks["musician-sign"] = true
	}
	if runMusicianVip {
		activeTasks["musician-vip"] = true
	}
	if runNote {
		activeTasks["note"] = true
	}
	if runFansGroup {
		activeTasks["fansgroup"] = true
	}

	accounts := c.getAccounts()
	if len(accounts) == 0 {
		c.cmd.Println("[task] 未配置任何账号，请检查 config.yaml")
		return nil
	}

	runFast := !c.opts.OnlySlow
	runSlow := !c.opts.OnlyFast

	mode := "by-task-group"
	if cfg.Task != nil && cfg.Task.Mode != "" {
		mode = cfg.Task.Mode
	}

	fastQueue := cfg.Task.FastTasks
	slowQueue := cfg.Task.SlowTasks

	if mode == "by-task-group" {
		if runFast {
			c.cmd.Println("[task] >>>>>> 开始执行 [快任务组] (跨账号串行) <<<<<<")
			c.runQueue(ctx, fastQueue, accounts, activeTasks)
			c.cmd.Println("[task] >>>>>> [快任务组] 执行完毕 <<<<<<\n")
		}

		if runSlow {
			c.cmd.Println("[task] >>>>>> 开始执行 [慢任务组] (跨账号串行) <<<<<<")
			c.runQueue(ctx, slowQueue, accounts, activeTasks)
			c.cmd.Println("[task] >>>>>> [慢任务组] 执行完毕 <<<<<<\n")
		}
	} else if mode == "by-account" {
		for _, account := range accounts {
			c.cmd.Printf("[task] >>>>>> 开始执行账号 (%s) <<<<<<\n", account.Filepath)
			if runFast {
				c.cmd.Println("  --- [快任务组] ---")
				c.runQueue(ctx, fastQueue, []Account{account}, activeTasks)
			}
			if runSlow {
				c.cmd.Println("  --- [慢任务组] ---")
				c.runQueue(ctx, slowQueue, []Account{account}, activeTasks)
			}
			c.cmd.Printf("[task] >>>>>> 账号 (%s) 执行完毕 <<<<<<\n\n", account.Filepath)
		}
	} else {
		return fmt.Errorf("未知的任务执行模式: %s，仅支持 by-task-group 和 by-account", mode)
	}

	c.cmd.Println("[task] 所有任务批量执行完毕！")
	return nil
}
