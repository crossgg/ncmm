// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/api/weapi"
	"github.com/3899/ncmm/pkg/log"

	"github.com/spf13/cobra"
)

type SignInOpts struct {
	Automatic bool
}

type SignIn struct {
	root *Root
	cmd  *cobra.Command
	l    *log.Logger
	opts SignInOpts
}

func NewSign(root *Root, l *log.Logger) *SignIn {
	c := &SignIn{
		root: root,
		l:    l,
		cmd: &cobra.Command{
			Use:     "sign",
			Short:   "[need login] Sign perform daily cloud shell check-in",
			Example: `  ncmm sign`,
		},
	}
	c.addFlags()
	c.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return c.execute(cmd.Context())
	}
	return c
}

func (c *SignIn) addFlags() {
	c.cmd.Flags().BoolVarP(&c.opts.Automatic, "automatic", "a", false, "automatically claim sign-in rewards")
}

func (c *SignIn) validate() error {
	return nil
}

func (c *SignIn) Add(command ...*cobra.Command) {
	c.cmd.AddCommand(command...)
}

func (c *SignIn) Command() *cobra.Command {
	return c.cmd
}

func (c *SignIn) execute(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	cfg := c.root.Cfg
	if cfg.Accounts == nil {
		return fmt.Errorf("配置文件中缺少 accounts 账号节点")
	}

	var hasExecuted bool

	// 1. 主账号一键签到
	if cfg.Sign != nil && cfg.Sign.EnablePrimary && cfg.Accounts.Primary != "" {
		c.cmd.Printf("[sign] >>>>>> 开始主账号签到 (%s) <<<<<<\n", cfg.Accounts.Primary)
		if err := c.runSignForCookie(ctx, cfg.Accounts.Primary, true); err != nil {
			c.cmd.Printf("[sign] ❌ 主账号签到失败: %s\n", err)
		}
		hasExecuted = true
	}

	// 2. 辅助账号一键签到
	if cfg.Sign != nil && cfg.Sign.EnableSecondaries && len(cfg.Accounts.Secondary) > 0 {
		for _, secCookie := range cfg.Accounts.Secondary {
			c.cmd.Printf("[sign] >>>>>> 开始辅助账号签到 (%s) <<<<<<\n", secCookie)
			if err := c.runSignForCookie(ctx, secCookie, false); err != nil {
				c.cmd.Printf("[sign] ❌ 辅助账号签到失败: %s\n", err)
			}
			hasExecuted = true
		}
	}

	if !hasExecuted {
		c.cmd.Println("[sign] 未启用或未配置任何账号进行日常签到，请检查 config.yaml")
	}
	return nil
}

func (c *SignIn) runSignForCookie(ctx context.Context, cookieFile string, isPrimary bool) error {
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
	request := weapi.New(cli)

	// 判断是否需要登录
	if request.NeedLogin(ctx) {
		return fmt.Errorf("Cookie 已失效，需要登录 (文件: %s)", cookieFile)
	}

	// 尝试读取个人信息友好提示
	vipPoint, err := request.VipGrowPoint(ctx, &weapi.VipGrowPointReq{})
	if err == nil && vipPoint.Code == 200 {
		c.cmd.Printf("  [当前账号信息] Uid: %d | 等级: %s (Lv.%d)\n", vipPoint.Data.UserLevel.UserId, vipPoint.Data.UserLevel.LevelName, vipPoint.Data.UserLevel.Level)
	}

	// 1. 音乐人签到 + 领取云豆
	c.cmd.Println("  --- 音乐人任务 ---")
	signResp, err := request.MusicianSign(ctx, &weapi.MusicianSignReq{})
	if err != nil {
		log.Warn("MusicianSign err: %s", err)
	} else if signResp.Code == 200 {
		c.cmd.Println("  ✅ 音乐人签到成功")
	} else {
		c.cmd.Printf("  提示: code=%d msg=%s\n", signResp.Code, signResp.Message)
	}

	// 获取音乐人任务列表并领取云豆
	var allTasks []weapi.MusicianTask

	// 1. 获取音乐人周期任务列表
	cycleTasks, err := request.MusicianTasks(ctx, &weapi.MusicianTasksReq{})
	if err != nil {
		c.cmd.Printf("  获取音乐人周期任务失败: %s\n", err)
	} else if cycleTasks.Code == 200 {
		allTasks = append(allTasks, cycleTasks.Data.TaskList...)
	} else {
		c.cmd.Printf("  获取音乐人周期任务提示: code=%d msg=%s\n", cycleTasks.Code, cycleTasks.Message)
	}

	// 2. 获取音乐人阶段任务列表
	stageTasks, err := request.MusicianTasksNew(ctx, &weapi.MusicianTasksNewReq{})
	if err != nil {
		c.cmd.Printf("  获取音乐人阶段任务失败: %s\n", err)
	} else if stageTasks.Code == 200 {
		allTasks = append(allTasks, stageTasks.Data.TaskList...)
	} else {
		c.cmd.Printf("  获取音乐人阶段任务提示: code=%d msg=%s\n", stageTasks.Code, stageTasks.Message)
	}

	if len(allTasks) == 0 {
		c.cmd.Println("  暂无音乐人任务")
	} else {
		var claimCount int
		for _, task := range allTasks {
			c.cmd.Printf("  任务: %s | 状态: %d | 进度: %d/%d\n",
				task.Name, task.Status, task.CurrentProgress, task.TargetWorth)
			if task.Status == 2 || (task.UserMissionId > 0 && task.CurrentProgress >= task.TargetWorth && task.TargetWorth > 0) {
				id := fmt.Sprintf("%d", task.UserMissionId)
				period := fmt.Sprintf("%d", task.Period)
				reward, err := request.MusicianCloudbeanObtain(ctx, &weapi.MusicianCloudbeanObtainReq{UserMissionId: id, Period: period})
				if err != nil {
					c.cmd.Printf("  ❌ 领取云豆失败 [%s]: %s\n", task.Name, err)
				} else if reward.Code == 200 {
					c.cmd.Printf("  ✅ 领取云豆成功 [%s] (id=%s)\n", task.Name, id)
					claimCount++
				} else {
					c.cmd.Printf("  ❌ 领取云豆失败 [%s]: code=%d msg=%s\n", task.Name, reward.Code, reward.Message)
				}
			}
		}
		if claimCount > 0 {
			c.cmd.Printf("  共完成 %d 个云豆奖励的领取\n", claimCount)
		}
	}

	// 2. 执行云贝签到
	c.cmd.Println("  --- 云贝任务 ---")
	yunbeiResp, err := request.YunBeiSignIn(ctx, &weapi.YunBeiSignInReq{})
	if err != nil {
		c.cmd.Printf("  云贝签到接口错误: %s\n", err)
	} else if yunbeiResp.Code != 200 {
		c.cmd.Printf("  云贝签到失败: %+v\n", yunbeiResp)
	} else {
		if yunbeiResp.Data.Sign {
			c.cmd.Println("  ✅ 云贝签到成功")
		} else {
			c.cmd.Println("  云贝今天已签到过")
		}
	}

	// 获取签到进度与自动领取
	if c.opts.Automatic {
		progress, err := request.YunBeiSignInProgress(ctx, &weapi.YunBeiSignInProgressReq{})
		if err == nil && progress.Code == 200 {
			for _, v := range progress.Data.LotteryConfig {
				if v.BaseLotteryId <= 0 && v.ExtraLotteryId <= 0 {
					continue
				}
				reply, err := request.YunBeiSignLottery(ctx, &weapi.YunBeiSignLotteryReq{
					UserLotteryId: fmt.Sprintf("%d", v.BaseLotteryId),
				})
				if err != nil {
					log.Error("YunBeiSignLottery(%v): %v", v.BaseLotteryId, err)
				}
				if reply.Data {
					c.cmd.Printf("  云贝连续签到 [%v天], 额外奖励 [%v] 领取成功\n", v.SignDay, v.BaseGrant.Name)
				}
			}
		}

		// 完成当前可领的任务云贝
		task, err := request.YunBeiTaskTodo(ctx, &weapi.YunBeiTaskTodoReq{})
		if err == nil && task.Code == 200 {
			for _, v := range task.Data {
				if !v.Completed {
					continue
				}
				reply, err := request.YunBeiTaskFinish(ctx, &weapi.YunBeiTaskFinishReq{
					Period:      fmt.Sprintf("%d", v.Period),
					UserTaskId:  fmt.Sprintf("%d", v.UserTaskId),
					DepositCode: fmt.Sprintf("%d", v.DepositCode),
				})
				if err == nil && reply.Code == 200 {
					c.cmd.Printf("  云贝 [%s] 任务完成获得云贝数量 %v\n", v.TaskName, v.TaskPoint)
				}
			}
		}
	}

	// 3. 黑胶 VIP 会员任务
	c.cmd.Println("  --- VIP 任务 ---")
	if vipPoint != nil && vipPoint.Code == 200 {
		if vipPoint.Data.UserLevel.LatestVipStatus != 1 {
			c.cmd.Printf("  暂无会员权益 (VIP 状态: %v)\n", vipPoint.Data.UserLevel.LatestVipStatus)
		} else {
			// 黑胶乐签
			vipSign, err := request.VipTaskSign(ctx, &weapi.VipTaskSignReq{IsNew: ""})
			if err == nil && vipSign.Data {
				c.cmd.Println("  ✅ 黑胶 VIP 乐签成功")
			} else {
				c.cmd.Println("  黑胶 VIP 今天已乐签过")
			}

			// 一键领取所有已完成成长值
			if c.opts.Automatic {
				reward, err := request.VipRewardGetAll(ctx, &weapi.VipRewardGetAllReq{})
				if err == nil && reward.Data.Result {
					c.cmd.Println("  ✅ 黑胶成长值已一键领取成功")
				}
			}
		}
	}

	// 4. 刷新 token 维持会话
	refresh, err := request.TokenRefresh(ctx, &weapi.TokenRefreshReq{})
	if err != nil || refresh.Code != 200 {
		log.Debug("TokenRefresh err: %s", err)
	}

	c.cmd.Printf("[sign] -----------------------------------------------\n\n")
	return nil
}
