// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"time"

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
	rng  *rand.Rand
}

func NewSign(root *Root, l *log.Logger) *SignIn {
	c := &SignIn{
		root: root,
		l:    l,
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
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

func (c *SignIn) isAutomatic() bool {
	if c.opts.Automatic {
		return true
	}
	if c.root.Cfg.Sign != nil && c.root.Cfg.Sign.Automatic {
		return true
	}
	return false
}

func (c *SignIn) Add(command ...*cobra.Command) {
	c.cmd.AddCommand(command...)
}

func (c *SignIn) Command() *cobra.Command {
	return c.cmd
}

func (c *SignIn) sleepBetweenAccounts(ctx context.Context, currentAccount string) {
	sleepSec := 5 + c.rng.Intn(16) // 5 ~ 20 秒
	c.cmd.Printf("[sign] ⏳ 账号 (%s) 任务处理完毕，为规避风控，随机等待 %d 秒后继续下一个账号...\n", currentAccount, sleepSec)
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(sleepSec) * time.Second):
	}
}

func (c *SignIn) execute(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	cfg := c.root.Cfg
	if cfg.Accounts == nil {
		return fmt.Errorf("配置文件中缺少 accounts 账号节点")
	}

	var activeAccounts []struct {
		Filepath string
		IsMain   bool
	}
	if cfg.Sign != nil && cfg.Sign.EnableMain && cfg.Accounts.Main != "" {
		activeAccounts = append(activeAccounts, struct {
			Filepath string
			IsMain   bool
		}{cfg.Accounts.Main, true})
	} else {
		if cfg.Sign != nil && !cfg.Sign.EnableMain {
			c.cmd.Println("[sign] 提示: 主账号日常签到任务已在配置文件中关闭 (enableMain = false)")
		} else if cfg.Accounts == nil || cfg.Accounts.Main == "" {
			c.cmd.Println("[sign] 提示: 主账号日常签到任务未执行，因为未配置主账号 (accounts.main)")
		}
	}

	if cfg.Sign != nil && cfg.Sign.EnableSecondaries && len(cfg.Accounts.Secondary) > 0 {
		for _, secCookie := range cfg.Accounts.Secondary {
			if secCookie != "" {
				activeAccounts = append(activeAccounts, struct {
					Filepath string
					IsMain   bool
				}{secCookie, false})
			}
		}
	} else {
		if cfg.Sign != nil && !cfg.Sign.EnableSecondaries {
			c.cmd.Println("[sign] 提示: 辅助账号日常签到任务已在配置文件中关闭 (enableSecondaries = false)")
		} else if cfg.Accounts == nil || len(cfg.Accounts.Secondary) == 0 {
			c.cmd.Println("[sign] 提示: 辅助账号日常签到任务未执行，因为未配置辅助账号 (accounts.secondary)")
		}
	}

	if len(activeAccounts) == 0 {
		c.cmd.Println("[sign] 未启用或未配置任何账号进行日常签到，请检查 config.yaml")
		return nil
	}

	for i, acc := range activeAccounts {
		if acc.IsMain {
			c.cmd.Printf("[sign] >>>>>> 开始主账号签到 (%s) <<<<<<\n", acc.Filepath)
			if err := c.RunSignForCookie(ctx, acc.Filepath, true, nil); err != nil {
				c.cmd.Printf("[sign] ❌ 主账号签到失败: %s\n", err)
			}
		} else {
			c.cmd.Printf("[sign] >>>>>> 开始辅助账号签到 (%s) <<<<<<\n", acc.Filepath)
			if err := c.RunSignForCookie(ctx, acc.Filepath, false, nil); err != nil {
				c.cmd.Printf("[sign] ❌ 辅助账号签到失败: %s\n", err)
			}
		}

		if i < len(activeAccounts)-1 {
			c.sleepBetweenAccounts(ctx, acc.Filepath)
		}
	}

	c.cmd.Println("[sign] 所有日常签到及播放任务执行完毕！")
	return nil
}

func (c *SignIn) RunSignForCookie(ctx context.Context, cookieFile string, isPrimary bool, allowedTasks map[string]bool) error {
	if len(allowedTasks) == 0 {
		allowedTasks = map[string]bool{
			"VipTask": true, "Reserve": true, "ViewVipCenter": true, "LikeComment": true,
			"FollowArtist": true, "LikeSong": true, "CollectSong": true, "PublishNote": true,
			"ListenIndie": true, "PlayDailyRecommend": true,
		}
	}

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
	var userId int64
	vipPoint, err := request.VipGrowPoint(ctx, &weapi.VipGrowPointReq{})
	if err == nil && vipPoint.Code == 200 {
		userId = vipPoint.Data.UserLevel.UserId
		c.cmd.Printf("  [当前账号信息] Uid: %d | 等级: %s (Lv.%d)\n", userId, vipPoint.Data.UserLevel.LevelName, vipPoint.Data.UserLevel.Level)
	}

	// 1. 执行云贝签到
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

	// 获取云贝签到进度与自动领取
	if c.isAutomatic() {
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

		// 执行云贝系列自动任务打卡并领奖
		hasYunbeiTasks := false
		yunbeiKeys := []string{"Reserve", "ViewVipCenter", "LikeComment", "FollowArtist", "LikeSong", "CollectSong", "PublishNote", "ListenIndie", "PlayDailyRecommend"}
		for _, k := range yunbeiKeys {
			if allowedTasks[k] {
				hasYunbeiTasks = true
				break
			}
		}
		if hasYunbeiTasks {
			c.handleYunbeiTasks(ctx, cli, request, userId, cookieFile, allowedTasks)
		}
	}

	// 2. 黑胶 VIP 会员任务
	if allowedTasks["VipTask"] {
		c.cmd.Println("  --- VIP 任务 ---")
		if vipPoint != nil && vipPoint.Code == 200 {
			if vipPoint.Data.UserLevel.LatestVipStatus != 1 {
				c.cmd.Printf("  暂无会员权益 (VIP 状态: %v)\n", vipPoint.Data.UserLevel.LatestVipStatus)
			} else {
				c.handleVipTasks(ctx, cli, request, vipPoint.Data.UserLevel.Level)
			}
		}
	}

	// 3. 刷新 token 维持会话
	refresh, err := request.TokenRefresh(ctx, &weapi.TokenRefreshReq{})
	if err != nil || refresh.Code != 200 {
		log.Debug("TokenRefresh err: %s", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(5+rand.Intn(11)) * time.Second):
		syncSessionConfig(ctx, cli, cookieFile, userId, nil, c.root.Cfg.Database)
	}

	c.cmd.Printf("[sign] -----------------------------------------------\n\n")
	return nil
}
