// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"

	"github.com/3899/ncmm/pkg/log"

	"github.com/spf13/cobra"
)

type TaskOpts struct {
	Sign     bool
	PlayIds  bool
	Musician bool
	Note     bool
}

type Task struct {
	root *Root
	cmd  *cobra.Command
	l    *log.Logger
	opts TaskOpts
}

func NewTask(root *Root, l *log.Logger) *Task {
	c := &Task{
		root: root,
		l:    l,
		cmd: &cobra.Command{
			Use:     "task",
			Short:   "Batch execute configured tasks",
			Example: "  ncmm task\n  ncmm task --sign --playids",
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
	c.cmd.Flags().BoolVar(&c.opts.Musician, "musician", false, "执行音乐人日常与VIP进阶任务")
	c.cmd.Flags().BoolVar(&c.opts.Note, "note", false, "执行笔记发布任务")
}

func (c *Task) execute(ctx context.Context) error {
	// 确定是否有命令行参数指定了特定任务
	hasFlags := c.cmd.Flags().Changed("sign") ||
		c.cmd.Flags().Changed("playids") ||
		c.cmd.Flags().Changed("musician") ||
		c.cmd.Flags().Changed("note")

	var runSign, runPlayIds, runMusician, runNote bool

	if hasFlags {
		runSign = c.opts.Sign
		runPlayIds = c.opts.PlayIds
		runMusician = c.opts.Musician
		runNote = c.opts.Note
	} else {
		// 从配置文件读取配置开关
		if c.root.Cfg.Task != nil {
			runSign = c.root.Cfg.Task.Sign
			runPlayIds = c.root.Cfg.Task.PlayIds
			runMusician = c.root.Cfg.Task.Musician
			runNote = c.root.Cfg.Task.Note
		} else {
			c.cmd.Println("[task] 提示: 配置文件中未定义 task 节点且未传递任何命令行标志，默认不执行任何任务")
			return nil
		}
	}

	// 依次执行任务
	if runSign {
		c.cmd.Println("[task] >>> 开始执行 [日常签到] 任务 <<<")
		s := NewSign(c.root, c.l)
		if err := s.execute(ctx); err != nil {
			c.cmd.Printf("[task] ❌ [日常签到] 执行失败: %s\n", err)
		} else {
			c.cmd.Println("[task] ✅ [日常签到] 执行成功")
		}
		c.cmd.Println()
	} else {
		if hasFlags {
			c.cmd.Println("[task] >>> [日常签到] 任务未在命令行中指定，跳过执行 <<<")
		} else {
			c.cmd.Println("[task] >>> [日常签到] 任务已在配置文件中关闭 (sign = false)，跳过执行 <<<")
		}
		c.cmd.Println()
	}

	if runPlayIds {
		c.cmd.Println("[task] >>> 开始执行 [播放指定歌曲] 任务 <<<")
		p := NewPlayIds(c.root, c.l)
		if err := p.execute(ctx); err != nil {
			c.cmd.Printf("[task] ❌ [播放指定歌曲] 执行失败: %s\n", err)
		} else {
			c.cmd.Println("[task] ✅ [播放指定歌曲] 执行成功")
		}
		c.cmd.Println()
	} else {
		if hasFlags {
			c.cmd.Println("[task] >>> [播放指定歌曲] 任务未在命令行中指定，跳过执行 <<<")
		} else {
			c.cmd.Println("[task] >>> [播放指定歌曲] 任务已在配置文件中关闭 (playIds = false)，跳过执行 <<<")
		}
		c.cmd.Println()
	}

	if runMusician {
		c.cmd.Println("[task] >>> 开始执行 [音乐人任务] 任务 <<<")
		m := NewMusician(c.root, c.l)
		if err := m.execute(ctx); err != nil {
			c.cmd.Printf("[task] ❌ [音乐人任务] 执行失败: %s\n", err)
		} else {
			c.cmd.Println("[task] ✅ [音乐人任务] 执行成功")
		}
		c.cmd.Println()
	} else {
		if hasFlags {
			c.cmd.Println("[task] >>> [音乐人任务] 任务未在命令行中指定，跳过执行 <<<")
		} else {
			c.cmd.Println("[task] >>> [音乐人任务] 任务已在配置文件中关闭 (musician = false)，跳过执行 <<<")
		}
		c.cmd.Println()
	}

	if runNote {
		c.cmd.Println("[task] >>> 开始执行 [发布图文动态] 任务 <<<")
		n := NewNote(c.root, c.l)
		if err := n.execute(ctx); err != nil {
			c.cmd.Printf("[task] ❌ [发布图文动态] 执行失败: %s\n", err)
		} else {
			c.cmd.Println("[task] ✅ [发布图文动态] 执行成功")
		}
		c.cmd.Println()
	} else {
		if hasFlags {
			c.cmd.Println("[task] >>> [发布图文动态] 任务未在命令行中指定，跳过执行 <<<")
		} else {
			c.cmd.Println("[task] >>> [发布图文动态] 任务已在配置文件中关闭 (note = false)，跳过执行 <<<")
		}
		c.cmd.Println()
	}

	c.cmd.Println("[task] 所有任务批量执行完毕！")
	return nil
}
