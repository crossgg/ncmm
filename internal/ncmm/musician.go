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
	"github.com/3899/ncmm/config"
	"github.com/3899/ncmm/pkg/database"
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
			Example: "  ncmm musician\n  ncmm musician-sign\n  ncmm musician-vip",
		},
	}
	c.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return c.execute(cmd.Context())
	}
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

// SignCommand 返回顶级命令 ncmm musician-sign
func (c *Musician) SignCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "musician-sign",
		Short:   "[need login] Execute musician daily sign-in and claim cloud beans (daily task)",
		Example: "  ncmm musician-sign",
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.executeSign(cmd.Context())
		},
	}
}

// VipCommand 返回顶级命令 ncmm musician-vip
func (c *Musician) VipCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "musician-vip",
		Short:   "[need login] Execute musician VIP advanced tasks: publish notes & relay play (monthly task)",
		Example: "  ncmm musician-vip",
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.executeVip(cmd.Context())
		},
	}
}

// ==================== 身份缓存逻辑 ====================

// musicianIdentityCacheKey 返回身份缓存的 badger key
func musicianIdentityCacheKey(cookieFile string) string {
	return fmt.Sprintf("musician:identity:%s", cookieFile)
}

func musicianRewardClaimCacheKey(cookieFile string, userMissionId, period int64) string {
	return fmt.Sprintf("musician:reward:%s:%d:%d", cookieFile, userMissionId, period)
}

// checkMusicianIdentityCache 检查本地缓存的音乐人身份状态
// 返回: (isMusician, cacheHit, error)
func (c *Musician) checkMusicianIdentityCache(ctx context.Context, db database.Database, cookieFile string) (bool, bool, error) {
	if db == nil {
		return false, false, nil
	}
	cacheDays := c.root.Cfg.Musician.IdentityCacheDays

	// -1 = 关闭缓存，始终走 API
	if cacheDays != nil && *cacheDays == -1 {
		return false, false, nil
	}

	cached, err := db.Get(ctx, musicianIdentityCacheKey(cookieFile))
	if err != nil {
		// 缓存未命中（key not found 或其他错误）
		return false, false, nil
	}

	// 缓存命中
	return cached == "1", true, nil
}

// saveMusicianIdentityCache 保存音乐人身份状态到本地缓存
func (c *Musician) saveMusicianIdentityCache(ctx context.Context, db database.Database, cookieFile string, isMusician bool) {
	if db == nil {
		return
	}
	cacheDays := c.root.Cfg.Musician.IdentityCacheDays
	if cacheDays != nil && *cacheDays == -1 {
		return // 缓存已关闭
	}

	value := "0"
	if isMusician {
		value = "1"
	}

	if cacheDays != nil && *cacheDays > 0 {
		ttl := time.Duration(*cacheDays) * 24 * time.Hour
		if err := db.Set(ctx, musicianIdentityCacheKey(cookieFile), value, ttl); err != nil {
			log.Warn("[musician] 写入身份缓存失败: %s", err)
		}
	} else {
		// 0 = 永久有效
		if err := db.Set(ctx, musicianIdentityCacheKey(cookieFile), value); err != nil {
			log.Warn("[musician] 写入身份缓存失败: %s", err)
		}
	}
}

// ==================== 公共初始化 ====================

// musicianContext 保存单次执行所需的公共上下文
type musicianContext struct {
	cli     *api.Client
	eapiCli *eapi.Api
	db      database.Database
	resp    *eapi.MusicianVipTasksResp // 可能为 nil（当缓存命中且仅执行 sign 时）
}

// initMusicianContext 初始化客户端并检查音乐人身份（优先读缓存）
// needVipData: true 表示需要 VIP 任务进度数据（vip 子命令或完整执行时需要）
func (c *Musician) initMusicianContext(ctx context.Context, cookieFile string, needVipData bool) (*musicianContext, error) {
	absPath, err := filepath.Abs(cookieFile)
	if err != nil {
		return nil, fmt.Errorf("解析 cookie 路径失败: %w", err)
	}

	// 初始化网络客户端
	networkCfg := *c.root.Cfg.Network
	networkCfg.Cookie.Filepath = absPath
	cli, err := api.NewClient(&networkCfg, c.l)
	if err != nil {
		return nil, fmt.Errorf("实例化客户端失败: %w", err)
	}

	// 初始化数据库（可选）。音乐人身份缓存是风控辅助缓存，如果数据库被占用，则自动跳过缓存直接通过 API 交互。
	db, err := database.NewWithOptions(c.root.Cfg.Database, 1, 0, true)
	if err != nil {
		c.cmd.Println("    ⚠️  本地数据库已被其他进程占用，已自动降级为无缓存模式直接执行")
		db = nil
	}

	mctx := &musicianContext{
		cli:     cli,
		eapiCli: eapi.New(cli),
		db:      db,
	}

	syncSessionConfig(ctx, cli, cookieFile, 0, db, nil)

	// 检查身份缓存
	isMusician, cacheHit, _ := c.checkMusicianIdentityCache(ctx, db, cookieFile)

	if cacheHit && !isMusician {
		// 缓存命中且非音乐人 → 直接返回错误
		mctx.close(ctx)
		return nil, fmt.Errorf("当前账号不是音乐人 (来自身份缓存)")
	}

	if cacheHit && !needVipData {
		// 缓存命中且只需 sign（不需要 VIP 任务进度）→ 跳过 API 调用
		c.cmd.Println("  ✅ 已认证音乐人 (来自身份缓存)")
		return mctx, nil
	}

	if !needVipData {
		// 日常签到只需要确认音乐人身份，使用 HAR 中的移动端 EAPI 身份接口。
		c.cmd.Println("  👉 检查音乐人身份...")
		role, err := mctx.eapiCli.MusicianRoleGet(ctx, &eapi.MusicianRoleGetReq{})
		if err != nil {
			mctx.close(ctx)
			return nil, fmt.Errorf("MusicianRoleGet: %w", err)
		}
		if role.Code != 200 {
			mctx.close(ctx)
			return nil, fmt.Errorf("MusicianRoleGet error: code=%d msg=%s", role.Code, role.Message)
		}

		c.saveMusicianIdentityCache(ctx, db, cookieFile, role.Data.IsMusician)
		if !role.Data.IsMusician {
			mctx.close(ctx)
			return nil, fmt.Errorf("当前账号不是音乐人")
		}

		c.cmd.Printf("  ✅ 已认证音乐人 | 用户ID: %d | 音乐人ID: %d\n", role.Data.UserId, role.Data.ArtistId)
		return mctx, nil
	}

	// 需要 VIP 数据 → 调用 VIP 权益任务 API。
	c.cmd.Println("  👉 检查任务状态...")
	resp, err := mctx.eapiCli.MusicianVipTasks(ctx, &eapi.MusicianVipTasksReq{ER: false})
	if err != nil {
		mctx.close(ctx)
		return nil, fmt.Errorf("MusicianVipTasks: %w", err)
	}
	if resp.Code != 200 {
		mctx.close(ctx)
		return nil, fmt.Errorf("MusicianVipTasks error: code=%d msg=%s", resp.Code, resp.Message)
	}

	// 写入身份缓存
	c.saveMusicianIdentityCache(ctx, db, cookieFile, resp.Data.IsMusician)

	if !resp.Data.IsMusician {
		mctx.close(ctx)
		return nil, fmt.Errorf("当前账号不是音乐人")
	}

	c.cmd.Printf("  ✅ 已认证音乐人 | 维持天数: %d 天 | 近30天播放: %d 次 | 解锁VIP权益: %v\n",
		resp.Data.MaintainDays, resp.Data.RecentPlayCount30, resp.Data.UnlockVipRight)

	mctx.resp = resp
	return mctx, nil
}

func (mc *musicianContext) close(ctx context.Context) {
	if mc.db != nil {
		mc.db.Close(ctx)
	}
	if mc.cli != nil {
		mc.cli.Close(ctx)
	}
}

// ==================== execute: 向后兼容，执行 sign + vip ====================

func (c *Musician) execute(ctx context.Context) error {
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
	if cfg.Musician != nil && cfg.Musician.EnableMain && cfg.Accounts.Main != "" {
		activeAccounts = append(activeAccounts, struct {
			Filepath string
			IsMain   bool
		}{cfg.Accounts.Main, true})
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableMain {
			c.cmd.Println("[musician] 提示: 主账号音乐人任务已在配置文件中关闭 (enableMain = false)")
		} else if cfg.Accounts == nil || cfg.Accounts.Main == "" {
			c.cmd.Println("[musician] 提示: 主账号音乐人任务未执行，因为未配置主账号 (accounts.main)")
		}
	}

	if cfg.Musician != nil && cfg.Musician.EnableSecondaries && len(cfg.Accounts.Secondary) > 0 {
		for _, secCookie := range cfg.Accounts.Secondary {
			if secCookie != "" {
				activeAccounts = append(activeAccounts, struct {
					Filepath string
					IsMain   bool
				}{secCookie, false})
			}
		}
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableSecondaries {
			c.cmd.Println("[musician] 提示: 辅助账号音乐人任务已在配置文件中关闭 (enableSecondaries = false)")
		} else if cfg.Accounts == nil || len(cfg.Accounts.Secondary) == 0 {
			c.cmd.Println("[musician] 提示: 辅助账号音乐人任务未执行，因为未配置辅助账号 (accounts.secondary)")
		}
	}

	if len(activeAccounts) == 0 {
		c.cmd.Println("[musician] 未启用或未配置任何账号进行音乐人任务，请检查 config.yaml")
		return nil
	}

	for i, acc := range activeAccounts {
		if acc.IsMain {
			c.cmd.Printf("[musician] >>>>>> 开始主账号音乐人任务 (%s) <<<<<<\n", acc.Filepath)
			if err := c.runMusicianForCookie(ctx, acc.Filepath, true); err != nil {
				c.cmd.Printf("[musician] ❌ 主账号任务失败: %s\n", err)
			}
		} else {
			c.cmd.Printf("[musician] >>>>>> 开始辅助账号音乐人任务 (%s) <<<<<<\n", acc.Filepath)
			if err := c.runMusicianForCookie(ctx, acc.Filepath, false); err != nil {
				c.cmd.Printf("[musician] ❌ 辅助账号任务失败: %s\n", err)
			}
		}

		if i < len(activeAccounts)-1 {
			c.sleepBetweenAccounts(ctx, acc.Filepath)
		}
	}

	c.cmd.Println("[musician] 所有音乐人日常与 VIP 任务执行完毕！")
	return nil
}

func (c *Musician) sleepBetweenAccounts(ctx context.Context, currentAccount string) {
	sleepSec := 5 + c.rng.Intn(16) // 5 ~ 20 秒
	c.cmd.Printf("[musician] ⏳ 账号 (%s) 任务处理完毕，为规避风控，随机等待 %d 秒后继续下一个账号...\n", currentAccount, sleepSec)
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(sleepSec) * time.Second):
	}
}

// runMusicianForCookie 执行单个账号的完整音乐人任务（sign + vip），单次 API 调用
func (c *Musician) runMusicianForCookie(ctx context.Context, cookieFile string, isPrimary bool) error {
	mctx, err := c.initMusicianContext(ctx, cookieFile, true)
	if err != nil {
		return err
	}
	defer mctx.close(ctx)

	// 第一阶段：音乐人日常任务
	c.doSignPhase(ctx, mctx, cookieFile)

	// 第二阶段：音乐人 VIP 任务
	c.doVipPhase(ctx, mctx, cookieFile)

	return nil
}

// ==================== executeSign: 仅日常签到 ====================

func (c *Musician) executeSign(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	cfg := c.root.Cfg
	if cfg.Accounts == nil {
		return fmt.Errorf("配置文件中缺少 accounts 账号节点")
	}

	var hasExecuted bool

	// 1. 主账号
	if cfg.Musician != nil && cfg.Musician.EnableMain && cfg.Accounts.Main != "" {
		c.cmd.Printf("[musician sign] >>>>>> 开始主账号音乐人日常签到 (%s) <<<<<<\n", cfg.Accounts.Main)
		if err := c.RunSignForCookie(ctx, cfg.Accounts.Main); err != nil {
			c.cmd.Printf("[musician sign] ❌ 主账号签到失败: %s\n", err)
		}
		hasExecuted = true
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableMain {
			c.cmd.Println("[musician sign] 提示: 主账号音乐人任务已在配置文件中关闭 (enableMain = false)")
		} else if cfg.Accounts == nil || cfg.Accounts.Main == "" {
			c.cmd.Println("[musician sign] 提示: 主账号音乐人任务未执行，因为未配置主账号 (accounts.main)")
		}
	}

	// 2. 辅助账号
	if cfg.Musician != nil && cfg.Musician.EnableSecondaries && len(cfg.Accounts.Secondary) > 0 {
		for _, secCookie := range cfg.Accounts.Secondary {
			c.cmd.Printf("[musician sign] >>>>>> 开始辅助账号音乐人日常签到 (%s) <<<<<<\n", secCookie)
			if err := c.RunSignForCookie(ctx, secCookie); err != nil {
				c.cmd.Printf("[musician sign] ❌ 辅助账号签到失败: %s\n", err)
			}
			hasExecuted = true
		}
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableSecondaries {
			c.cmd.Println("[musician sign] 提示: 辅助账号音乐人任务已在配置文件中关闭 (enableSecondaries = false)")
		} else if cfg.Accounts == nil || len(cfg.Accounts.Secondary) == 0 {
			c.cmd.Println("[musician sign] 提示: 辅助账号音乐人任务未执行，因为未配置辅助账号 (accounts.secondary)")
		}
	}

	if !hasExecuted {
		c.cmd.Println("[musician sign] 未启用或未配置任何账号进行音乐人签到，请检查 config.yaml")
	} else {
		c.cmd.Println("[musician sign] 所有音乐人日常签到任务执行完毕！")
	}
	return nil
}

// RunSignForCookie 执行单个账号的音乐人日常签到
func (c *Musician) RunSignForCookie(ctx context.Context, cookieFile string) error {
	mctx, err := c.initMusicianContext(ctx, cookieFile, false)
	if err != nil {
		return err
	}
	defer mctx.close(ctx)

	c.doSignPhase(ctx, mctx, cookieFile)
	return nil
}

// ==================== executeVip: 仅 VIP 进阶任务 ====================

func (c *Musician) executeVip(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	cfg := c.root.Cfg
	if cfg.Accounts == nil {
		return fmt.Errorf("配置文件中缺少 accounts 账号节点")
	}

	var hasExecuted bool

	// 1. 主账号
	if cfg.Musician != nil && cfg.Musician.EnableMain && cfg.Accounts.Main != "" {
		c.cmd.Printf("[musician vip] >>>>>> 开始主账号音乐人VIP进阶任务 (%s) <<<<<<\n", cfg.Accounts.Main)
		if err := c.RunVipForCookie(ctx, cfg.Accounts.Main); err != nil {
			c.cmd.Printf("[musician vip] ❌ 主账号VIP任务失败: %s\n", err)
		}
		hasExecuted = true
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableMain {
			c.cmd.Println("[musician vip] 提示: 主账号音乐人任务已在配置文件中关闭 (enableMain = false)")
		} else if cfg.Accounts == nil || cfg.Accounts.Main == "" {
			c.cmd.Println("[musician vip] 提示: 主账号音乐人任务未执行，因为未配置主账号 (accounts.main)")
		}
	}

	// 2. 辅助账号
	if cfg.Musician != nil && cfg.Musician.EnableSecondaries && len(cfg.Accounts.Secondary) > 0 {
		for _, secCookie := range cfg.Accounts.Secondary {
			c.cmd.Printf("[musician vip] >>>>>> 开始辅助账号音乐人VIP进阶任务 (%s) <<<<<<\n", secCookie)
			if err := c.RunVipForCookie(ctx, secCookie); err != nil {
				c.cmd.Printf("[musician vip] ❌ 辅助账号VIP任务失败: %s\n", err)
			}
			hasExecuted = true
		}
	} else {
		if cfg.Musician != nil && !cfg.Musician.EnableSecondaries {
			c.cmd.Println("[musician vip] 提示: 辅助账号音乐人任务已在配置文件中关闭 (enableSecondaries = false)")
		} else if cfg.Accounts == nil || len(cfg.Accounts.Secondary) == 0 {
			c.cmd.Println("[musician vip] 提示: 辅助账号音乐人任务未执行，因为未配置辅助账号 (accounts.secondary)")
		}
	}

	if !hasExecuted {
		c.cmd.Println("[musician vip] 未启用或未配置任何账号进行音乐人VIP任务，请检查 config.yaml")
	} else {
		c.cmd.Println("[musician vip] 所有音乐人VIP进阶任务执行完毕！")
	}
	return nil
}

// RunVipForCookie 执行单个账号的音乐人 VIP 进阶任务
func (c *Musician) RunVipForCookie(ctx context.Context, cookieFile string) error {
	mctx, err := c.initMusicianContext(ctx, cookieFile, true)
	if err != nil {
		return err
	}
	defer mctx.close(ctx)

	c.doVipPhase(ctx, mctx, cookieFile)
	return nil
}

// ==================== 任务执行阶段 ====================

// doSignPhase 执行第一阶段：音乐人日常签到 + 领云豆
func (c *Musician) doSignPhase(ctx context.Context, mctx *musicianContext, cookieFile string) {
	c.cmd.Println("  👉 [第一阶段] 开始执行音乐人日常任务 (日常签到与云豆领奖)...")

	if deviceId := mctx.cli.GetDeviceId(); deviceId != "" {
		c.cmd.Printf("    👉 [deviceId] 使用 Cookie 中的设备 ID: %s\n", deviceId)
	}

	// 1. 执行前先展示任务列表，便于观察签到前后的状态变化。
	c.cmd.Println("    👉 音乐人任务列表（执行前）...")
	beforeTasks := c.fetchMusicianRewardTasks(ctx, mctx)
	c.printMusicianTaskList(beforeTasks)

	// 2. 音乐人日常签到。HAR 对应 /api/creator/user/access。
	c.cmd.Println("    👉 开始音乐人日常签到...")
	signResp, err := mctx.eapiCli.MusicianSign(ctx, &eapi.MusicianSignReq{})
	if err != nil {
		c.cmd.Printf("    ❌ 音乐人日常签到失败: %v\n", err)
	} else if signResp.Code == 200 && signResp.Data {
		c.cmd.Println("    ✅ 音乐人日常签到成功")
	} else if signResp.Code == 200 {
		c.cmd.Println("    ℹ️ 音乐人日常签到接口已成功返回，但未返回新的签到状态")
	} else {
		c.cmd.Printf("    ℹ️ 音乐人日常签到提示: code=%d msg=%s\n", signResp.Code, signResp.Message)
	}

	// 3. 签到后刷新任务状态，领取可领取的周期/阶段云豆奖励。
	afterSignTasks := c.fetchMusicianRewardTasks(ctx, mctx)
	claimCount := c.claimMusicianRewards(ctx, mctx, cookieFile, afterSignTasks)

	finalTasks := afterSignTasks
	if claimCount > 0 {
		finalTasks = c.fetchMusicianRewardTasks(ctx, mctx)
	}

	// 4. 展示执行后任务列表。
	c.cmd.Println("    👉 音乐人任务列表（执行后）...")
	c.printMusicianTaskList(finalTasks)
}

func (c *Musician) claimMusicianRewards(ctx context.Context, mctx *musicianContext, cookieFile string, tasks []eapi.MusicianMissionTask) int {
	if len(tasks) == 0 {
		c.cmd.Println("    ℹ️ 没有可领取的云豆奖励")
		return 0
	}

	var claimCount int
	claimed := make(map[string]bool)
	for _, task := range tasks {
		if !isMusicianRewardClaimable(task) {
			continue
		}

		key := fmt.Sprintf("%d:%d", task.UserMissionId, task.Period)
		if claimed[key] {
			continue
		}
		claimed[key] = true
		if c.isMusicianRewardClaimCached(ctx, mctx, cookieFile, task) {
			continue
		}

		name := musicianTaskDisplayName(task)
		reward, err := mctx.eapiCli.MusicianRewardObtain(ctx, &eapi.MusicianRewardObtainReq{
			UserMissionId: task.UserMissionId,
			Period:        task.Period,
		})
		if err != nil {
			c.cmd.Printf("    ❌ 领取 [%s]云豆奖励失败: %v\n", name, err)
			continue
		}
		if reward.Code != 200 {
			c.cmd.Printf("    ℹ️ 领取 [%s]云豆奖励提示: code=%d msg=%s\n", name, reward.Code, reward.Message)
			continue
		}

		if rewardWorth := musicianTaskRewardWorth(task); rewardWorth != "" {
			c.cmd.Printf("    🎉 成功领取 [%s]云豆奖励：+%s\n", name, rewardWorth)
		} else {
			c.cmd.Printf("    🎉 成功领取 [%s]云豆奖励\n", name)
		}
		c.saveMusicianRewardClaimCache(ctx, mctx, cookieFile, task)
		claimCount++
	}

	if claimCount == 0 {
		c.cmd.Println("    ℹ️ 没有可领取的云豆奖励")
	}
	return claimCount
}

func (c *Musician) isMusicianRewardClaimCached(ctx context.Context, mctx *musicianContext, cookieFile string, task eapi.MusicianMissionTask) bool {
	if mctx.db == nil || task.UserMissionId <= 0 {
		return false
	}
	ok, err := mctx.db.Exists(ctx, musicianRewardClaimCacheKey(cookieFile, task.UserMissionId, task.Period))
	if err != nil {
		log.Warn("[musician] 读取云豆领奖缓存失败: %s", err)
		return false
	}
	return ok
}

func (c *Musician) saveMusicianRewardClaimCache(ctx context.Context, mctx *musicianContext, cookieFile string, task eapi.MusicianMissionTask) {
	if mctx.db == nil || task.UserMissionId <= 0 {
		return
	}
	if err := mctx.db.Set(ctx, musicianRewardClaimCacheKey(cookieFile, task.UserMissionId, task.Period), "1", 45*24*time.Hour); err != nil {
		log.Warn("[musician] 写入云豆领奖缓存失败: %s", err)
	}
}

func (c *Musician) fetchMusicianRewardTasks(ctx context.Context, mctx *musicianContext) []eapi.MusicianMissionTask {
	var allTasks []eapi.MusicianMissionTask
	seen := make(map[string]bool)
	appendTasks := func(tasks []eapi.MusicianMissionTask) {
		for _, task := range tasks {
			key := fmt.Sprintf("%d:%d:%d", task.UserMissionId, task.MissionId, task.Period)
			if seen[key] {
				continue
			}
			seen[key] = true
			allTasks = append(allTasks, task)
		}
	}

	// 日常签到周期任务：移动端抓包主要使用 platform=300, tag=101。
	if cycle, err := mctx.eapiCli.MusicianMissionCycleList(ctx, &eapi.MusicianMissionListReq{Platform: 300, Tag: 101}); err == nil && cycle.Code == 200 {
		appendTasks(cycle.Data.List)
	} else if err != nil {
		c.cmd.Printf("    ⚠️ 获取音乐人周期任务失败: %v\n", err)
	}

	// 兼容网页版/旧移动端入口：HAR 中也出现 actionType=102, platform=200 的周期列表。
	if cycle, err := mctx.eapiCli.MusicianMissionCycleList(ctx, &eapi.MusicianMissionListReq{Platform: 200, ActionType: 102}); err == nil && cycle.Code == 200 {
		appendTasks(cycle.Data.List)
	}

	// 阶段任务用于补充可领取云豆，抓包参数为 platform=300, tag=303。
	if stage, err := mctx.eapiCli.MusicianMissionStageList(ctx, &eapi.MusicianMissionListReq{Platform: 300, Tag: 303}); err == nil && stage.Code == 200 {
		appendTasks(stage.Data.List)
	} else if err != nil {
		c.cmd.Printf("    ⚠️ 获取音乐人阶段任务失败: %v\n", err)
	}

	return allTasks
}

func musicianTaskDisplayName(task eapi.MusicianMissionTask) string {
	for _, v := range []string{task.Description, task.Name, task.Title} {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	if task.MissionId > 0 {
		return fmt.Sprintf("mission-%d", task.MissionId)
	}
	return "unknown"
}

func (c *Musician) printMusicianTaskList(tasks []eapi.MusicianMissionTask) {
	if len(tasks) == 0 {
		c.cmd.Println("    ℹ️ 暂无音乐人周期/阶段任务")
		return
	}

	for _, task := range tasks {
		c.cmd.Printf("    - 任务: %-20s | %s | 进度: %d/%d | 云豆: %s\n",
			musicianTaskDisplayName(task),
			musicianTaskStatusText(task),
			task.ProgressRate,
			task.TargetCount,
			musicianTaskRewardWorth(task),
		)
	}
}

func musicianTaskStatusText(task eapi.MusicianMissionTask) string {
	if task.Status == 100 {
		return "已完成"
	}
	return "未完成"
}

func musicianTaskRewardWorth(task eapi.MusicianMissionTask) string {
	return strings.TrimSpace(task.RewardWorth)
}

func isMusicianRewardClaimable(task eapi.MusicianMissionTask) bool {
	if task.UserMissionId <= 0 {
		return false
	}
	if task.Status != 100 && task.NeedToReceive <= 0 {
		return false
	}
	if task.RewardId <= 0 && task.RewardWorth == "" && task.NeedToReceive <= 0 {
		return false
	}
	if task.TargetCount > 0 && task.ProgressRate < task.TargetCount && task.NeedToReceive <= 0 {
		return false
	}
	return true
}

// doVipPhase 执行第二阶段：音乐人 VIP 进阶任务
func (c *Musician) doVipPhase(ctx context.Context, mctx *musicianContext, cookieFile string) {
	if mctx.resp == nil || mctx.resp.Data.FurtherTask == nil {
		c.cmd.Println("  👉 [第二阶段] 提示: 没有音乐人 VIP 进阶任务 (跳过)")
		return
	}

	c.cmd.Printf("  👉 [第二阶段] 开始执行音乐人 VIP 任务 (发布笔记与接力刷歌)... 进度: %d/%d 完成\n",
		mctx.resp.Data.FurtherTask.ProgressRate, mctx.resp.Data.FurtherTask.TotalCompleteNum)

	// 遍历子任务，检查并执行
	for _, sub := range mctx.resp.Data.FurtherTask.Children {
		progress := sub.ProgressRate
		if sub.MissionCode == "mission_code_recently_play_count" {
			progress = mctx.resp.Data.RecentPlayCount30
		}
		c.cmd.Printf("    - 任务: %-15s — 状态: %d, 进度: %d/%d\n",
			sub.Name, sub.MissionStatus, progress, sub.TotalCompleteNum)

		if sub.MissionStatus == 100 {
			c.cmd.Printf("    ✅ 任务已完成: %s\n", sub.Name)
			continue
		}

		switch sub.MissionCode {
		case "mission_code_musician_notebook_publish":
			if c.root.Cfg.Musician.EnableVipNote != nil && !*c.root.Cfg.Musician.EnableVipNote {
				c.cmd.Println("    ℹ️ 笔记任务已在配置中关闭 (enableVipNote = false)，跳过")
			} else if sub.ProgressRate >= sub.TotalCompleteNum {
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
			if c.root.Cfg.Musician.EnableVipPlay != nil && !*c.root.Cfg.Musician.EnableVipPlay {
				c.cmd.Println("    ℹ️ 播放任务已在配置中关闭 (enableVipPlay = false)，跳过")
			} else if err := c.handlePlayTask(ctx, mctx.cli, sub, mctx.resp.Data.RecentPlayCount30); err != nil {
				log.Error("    ❌ 播放任务执行失败: %s", err)
				c.cmd.Printf("    ❌ 播放任务失败: %s\n", err)
			}

		default:
			c.cmd.Printf("    ⚠️ 未知任务类型: %s\n", sub.MissionCode)
		}
	}
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

		// 执行单账号播放打卡，返回该账号实际成功播放的"主歌"数量
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
