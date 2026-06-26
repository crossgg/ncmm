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
	"github.com/3899/ncmm/api/types"
	"github.com/3899/ncmm/api/weapi"
)

func (c *SignIn) isPlayDailyRecommendTask(taskName string) bool {
	switch taskName {
	case "听歌30分钟", "听歌", "每日推荐", "听推荐歌曲", "听推荐歌单中的歌", "听音乐30分钟":
		return true
	}
	return false
}

func (c *SignIn) isYunbeiTaskEnabled(taskName string, allowedTasks map[string]bool) bool {
	cfg := c.root.Cfg.Sign.YunbeiTask
	if cfg == nil {
		return false
	}
	switch taskName {
	case "浏览会员中心":
		return allowedTasks["ViewVipCenter"] && cfg.EnableViewVipCenter
	case "点赞评论、动态", "点赞":
		return allowedTasks["LikeComment"] && cfg.EnableLikeComment
	case "探索小众歌曲":
		return allowedTasks["ListenIndie"] && cfg.EnableListenIndie
	case "关注歌手":
		return allowedTasks["FollowArtist"] && cfg.EnableFollowArtist
	case "收藏":
		return allowedTasks["CollectSong"] && cfg.EnableCollectSong
	case "红心歌曲", "红心":
		return allowedTasks["LikeSong"] && cfg.EnableLikeSong
	case "发布动态", "分享动态", "发布图文", "发布图文动态", "发布笔记", "分享图文", "发布图文笔记":
		return allowedTasks["PublishNote"] && cfg.EnablePublishNote
	case "听歌30分钟", "听歌", "每日推荐", "听推荐歌曲", "听推荐歌单中的歌", "听音乐30分钟":
		return allowedTasks["PlayDailyRecommend"] && cfg.EnablePlayDailyRecommend
	}
	return false
}

func (c *SignIn) executeSingleYunbeiTask(ctx context.Context, eapiRequest *eapi.Api, request *weapi.Api, v eapi.YunBeiTaskTodoRespData, allowedTasks map[string]bool, cookieFile string, userId int64) {
	switch v.TaskName {
	case "浏览会员中心":
		c.cmd.Println("  👉 开始执行 [浏览会员中心] 任务...")
		c.doViewVipCenter(ctx, eapiRequest)
	case "点赞评论、动态", "点赞":
		c.cmd.Printf("  👉 开始执行 [%s] 任务 (使用 [点赞评论] 开关控制)...\n", v.TaskName)
		c.doLikeComments(ctx, request)
	case "探索小众歌曲":
		c.cmd.Println("  👉 开始执行 [探索小众歌曲] 听歌任务...")
		c.doListenIndie(ctx, eapiRequest, request)
	case "关注歌手":
		c.cmd.Println("  👉 开始执行 [关注歌手] 任务...")
		c.doFollowArtist(ctx, eapiRequest)
	case "收藏":
		c.cmd.Println("  👉 开始执行 [收藏] 任务 (使用 [收藏歌曲] 开关控制)...")
		c.doCollectSong(ctx, request, userId)
	case "红心歌曲", "红心":
		c.cmd.Printf("  👉 开始执行 [%s] 任务 (使用 [红心歌曲] 开关控制)...\n", v.TaskName)
		c.doLikeSong(ctx, eapiRequest)
	case "发布动态", "分享动态", "发布图文", "发布图文动态", "发布笔记", "分享图文", "发布图文笔记":
		c.cmd.Printf("  👉 开始执行 [%s] 任务 (使用 [发布动态] 开关控制)...\n", v.TaskName)
		c.doPublishNote(ctx, cookieFile)
	}
}

func (c *SignIn) sleepBetweenSubtasks(ctx context.Context, taskName string) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	sleepSec := 1 + r.Intn(5) // 1 ~ 5 秒
	c.cmd.Printf("  ⏳ 子任务 [%s] 执行完毕，随机等待 %d 秒后继续下一个子任务...\n", taskName, sleepSec)
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(sleepSec) * time.Second):
	}
}

func (c *SignIn) handleYunbeiTasks(ctx context.Context, cli *api.Client, request *weapi.Api, userId int64, cookieFile string, allowedTasks map[string]bool) {
	eapiRequest := eapi.New(cli)

	// 1. 预约领云贝 (特殊板块，独立于日常任务列表，运行一次即可)
	if allowedTasks["Reserve"] {
		c.handleReserveYunbei(ctx, eapiRequest)
	}

	// 2. 双轮循环执行任务并领奖
	for round := 1; round <= 2; round++ {
		c.cmd.Printf("  👉 [云贝任务第 %d/2 轮] 获取待做任务列表...\n", round)
		task, err := eapiRequest.YunBeiTaskTodo(ctx, &eapi.YunBeiTaskTodoReq{})
		if err != nil || task.Code != 200 {
			c.cmd.Printf("  ❌ 获取云贝任务列表失败: %v\n", err)
			return
		}

		// 筛选待做且启用的任务
		var todoTasks []eapi.YunBeiTaskTodoRespData
		var playDailyRecommendTaskName string
		for _, v := range task.Data {
			if v.Completed {
				continue
			}
			if !c.isYunbeiTaskEnabled(v.TaskName, allowedTasks) {
				continue
			}
			if c.isPlayDailyRecommendTask(v.TaskName) {
				playDailyRecommendTaskName = v.TaskName
				continue
			}
			todoTasks = append(todoTasks, v)
		}

		// 若本轮没有需要做的任务，提前终止循环
		if len(todoTasks) == 0 && playDailyRecommendTaskName == "" {
			c.cmd.Printf("  ℹ️ [云贝任务第 %d/2 轮] 无待执行的未完成任务，退出循环\n", round)
			break
		}

		c.cmd.Printf("  👉 [云贝任务第 %d/2 轮] 成功拉取到待完成任务:\n", round)
		for _, v := range todoTasks {
			c.cmd.Printf("    - 任务: %-15s | 奖励: %d 云贝\n", v.TaskName, v.TaskPoint)
		}
		if playDailyRecommendTaskName != "" {
			c.cmd.Printf("    - 任务: %-15s | 奖励: 听歌奖励 (将在本轮常规任务后执行)\n", playDailyRecommendTaskName)
		}

		// 执行常规任务
		for i, v := range todoTasks {
			c.executeSingleYunbeiTask(ctx, eapiRequest, request, v, allowedTasks, cookieFile, userId)
			if i < len(todoTasks)-1 || playDailyRecommendTaskName != "" {
				c.sleepBetweenSubtasks(ctx, v.TaskName)
			}
		}

		// 执行日推播放任务 (前台串行，作为当前轮次的最后一个任务执行)
		if playDailyRecommendTaskName != "" {
			c.cmd.Printf("  👉 开始执行 [%s] 任务 (使用 [播放日推] 开关控制)...\n", playDailyRecommendTaskName)
			c.doPlayDailyRecommend(ctx, cookieFile, playDailyRecommendTaskName)
		}

		// 重新拉取任务列表，对本轮已完成的任务进行领奖
		c.cmd.Println("  👉 重新获取任务列表并领取奖励...")
		refreshedTask, err := eapiRequest.YunBeiTaskTodo(ctx, &eapi.YunBeiTaskTodoReq{})
		if err == nil && refreshedTask.Code == 200 {
			var claimedCount int
			for _, v := range refreshedTask.Data {
				if !v.Completed {
					continue
				}
				reply, err := request.YunBeiTaskFinish(ctx, &weapi.YunBeiTaskFinishReq{
					Period:      fmt.Sprintf("%d", v.Period),
					UserTaskId:  fmt.Sprintf("%d", v.UserTaskId),
					DepositCode: fmt.Sprintf("%d", v.DepositCode),
				})
				if err == nil && reply.Code == 200 {
					c.cmd.Printf("  🎉 成功领取云贝 [%s] 任务奖励，获得云贝: %v\n", v.TaskName, v.TaskPoint)
					claimedCount++
				}
			}
			if claimedCount == 0 {
				c.cmd.Println("  ℹ️ 没有可领取的任务奖励")
			}
		} else {
			c.cmd.Printf("  ❌ 重新拉取任务列表失败: %v\n", err)
		}
	}

	// 最终状态展示
	finalTask, err := eapiRequest.YunBeiTaskTodo(ctx, &eapi.YunBeiTaskTodoReq{})
	if err == nil && finalTask.Code == 200 {
		c.cmd.Println("  👉 最终的云贝任务列表状态:")
		for _, v := range finalTask.Data {
			statusStr := "已完成"
			if !v.Completed {
				statusStr = "未完成"
			}
			c.cmd.Printf("    - 任务: %-15s | 状态: %-6s | 奖励: %d 云贝\n", v.TaskName, statusStr, v.TaskPoint)
		}
	} else {
		c.cmd.Printf("  ❌ 获取最终任务状态列表失败: %v\n", err)
	}
}

// doViewVipCenter 执行浏览会员中心任务（15~25秒随机延迟）
func (c *SignIn) doViewVipCenter(ctx context.Context, eapiRequest *eapi.Api) {
	_, err := eapiRequest.YunbeiClickTask(ctx, &eapi.YunbeiClickTaskReq{
		TaskId:     6758460,
		SubAction:  "weibo",
		Type:       "feizhu",
		CheckToken: "",
	})
	if err != nil {
		c.cmd.Printf("  ❌ 浏览会员中心触发失败: %v\n", err)
		return
	}
	sleepSec := 15 + rand.Intn(11) // 15 ~ 25 秒随机
	c.cmd.Printf("  ⏳ 已模拟触发浏览会员中心任务，随机浏览 %d 秒...\n", sleepSec)
	time.Sleep(time.Duration(sleepSec) * time.Second)
	c.cmd.Println("  ✅ 浏览会员中心模拟结束")
}

// doLikeComments 点赞热门评论（点赞10个，3~10秒随机延迟后取消点赞）
func (c *SignIn) doLikeComments(ctx context.Context, request *weapi.Api) {
	comments, err := request.Comments(ctx, &weapi.CommentsReq{
		ThreadId: "R_SO_4_186016", // 晴天的ThreadId
		Limit:    "20",
		Offset:   "0",
	})
	if err != nil || comments.Code != 200 || len(comments.Comments) == 0 {
		c.cmd.Printf("  ❌ 获取用于点赞的评论列表失败: %v\n", err)
		return
	}

	targetCount := 10
	if len(comments.Comments) < targetCount {
		targetCount = len(comments.Comments)
	}

	c.cmd.Printf("  👉 准备点赞并取消点赞 %d 条评论...\n", targetCount)
	var successCount int
	for i := 0; i < targetCount; i++ {
		comment := comments.Comments[i]
		commentIdStr := fmt.Sprintf("%d", comment.CommentId)

		// 1. 点赞
		_, err := request.CommentLike(ctx, &weapi.CommentLikeReq{
			ThreadId:  "R_SO_4_186016",
			CommentId: commentIdStr,
		})
		if err == nil {
			// 2. 延迟 3~10 秒后取消点赞
			sleepSec := 3 + rand.Intn(8) // 3 ~ 10 秒
			time.Sleep(time.Duration(sleepSec) * time.Second)
			_, _ = request.CommentUnlike(ctx, &weapi.CommentLikeReq{
				ThreadId:  "R_SO_4_186016",
				CommentId: commentIdStr,
			})
			successCount++
		}
	}
	c.cmd.Printf("  ✅ 点赞评论操作完成，成功: %d/%d\n", successCount, targetCount)
}

// doListenIndie 探索小众歌曲听歌（严格串行，每首交替上报凭证和听歌状态）
func (c *SignIn) doListenIndie(ctx context.Context, eapiRequest *eapi.Api, request *weapi.Api) {
	recommend, err := eapiRequest.YunbeiDistributionRecommendSong(ctx, &eapi.YunbeiDistributionRecommendSongReq{
		Offset: 0,
		Limit:  10,
	})
	if err != nil || recommend.Code != 200 || len(recommend.Data) == 0 {
		c.cmd.Printf("  ❌ 获取小众推荐歌曲失败: %v\n", err)
		return
	}

	targetCount := 10
	if len(recommend.Data) < targetCount {
		targetCount = len(recommend.Data)
	}

	// 批量查询歌曲详情以获得正确的 AlbumId 和歌名
	songIds := make([]string, targetCount)
	for i := 0; i < targetCount; i++ {
		songIds[i] = fmt.Sprintf("%d", recommend.Data[i].SongId)
	}
	reqList := make([]weapi.SongDetailReqList, len(songIds))
	for i, id := range songIds {
		reqList[i] = weapi.SongDetailReqList{Id: id, V: 0}
	}
	albumMap := make(map[int64]int64)
	nameMap := make(map[int64]string)
	details, detailErr := request.SongDetail(ctx, &weapi.SongDetailReq{C: reqList})
	if detailErr == nil && details != nil {
		for _, s := range details.Songs {
			albumMap[s.Id] = s.Al.Id
			nameMap[s.Id] = s.Name
		}
	}

	c.cmd.Printf("  👉 已拉取到 %d 首推荐小众歌曲，开始依次串行听歌打卡...\n", targetCount)

	for i := 0; i < targetCount; i++ {
		song := recommend.Data[i]
		sId := song.SongId
		aId := albumMap[sId]
		if aId == 0 {
			aId = song.AlbumId
		}
		songName := nameMap[sId]
		if songName == "" {
			songName = "未知歌曲"
		}

		// 31~50秒之间随机延迟听歌
		sleepSec := 31 + rand.Intn(20)
		c.cmd.Printf("  ⏳ 正在听第 %d/%d 首小众歌曲: ID=%d , 歌名=%s (模拟播放 %d秒)...\n", i+1, targetCount, sId, songName, sleepSec)

		// 阻塞等待模拟播放完成
		select {
		case <-ctx.Done():
			c.cmd.Println("  ❌ 听歌打卡被取消")
			return
		case <-time.After(time.Duration(sleepSec) * time.Second):
		}

		// 1. 请求云贝分配创建凭证 (YunbeiDistributionCreate)
		dist, distErr := eapiRequest.YunbeiDistributionCreate(ctx, &eapi.YunbeiDistributionCreateReq{
			YunbeiAmount: 150,
		})
		if distErr != nil {
			c.cmd.Printf("  ❌ [听歌打卡 %d/%d] 申请云贝分配凭证失败: %v\n", i+1, targetCount, distErr)
		} else if dist.Code == 200 && dist.Data {
			c.cmd.Printf("  🎉 [听歌打卡 %d/%d] 成功申请云贝分配凭证\n", i+1, targetCount)
		} else {
			c.cmd.Printf("  ⚠️ [听歌打卡 %d/%d] 申请云贝分配凭证异常: code=%d msg=%s\n", i+1, targetCount, dist.Code, dist.Message)
		}

		// 2. 额外延迟 2~5 秒以模拟前台点击或缓冲
		extraSleep := 2 + rand.Intn(4)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(extraSleep) * time.Second):
		}

		// 3. 上报听歌打卡 (TrialsongListen)
		_, listenErr := eapiRequest.TrialsongListen(ctx, &eapi.TrialsongListenReq{
			SongId:  fmt.Sprintf("%d", sId),
			AlbumId: fmt.Sprintf("%d", aId),
			Scene:   1,
		})
		if listenErr != nil {
			c.cmd.Printf("  ❌ [听歌打卡 %d/%d] (歌曲 ID: %d) 上报失败: %v\n", i+1, targetCount, sId, listenErr)
		} else {
			c.cmd.Printf("  ✅ [听歌打卡 %d/%d] (歌曲 ID: %d) 听歌完毕，成功上报\n", i+1, targetCount, sId)
		}

		// 如果不是最后一首歌曲，切歌过渡时睡眠 1~3 秒
		if i < targetCount-1 {
			transitionSleep := 1 + rand.Intn(3)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(transitionSleep) * time.Second):
			}
		}
	}
	c.cmd.Println("  ✅ 探索小众歌曲 10 首打卡完毕")
}

// handleReserveYunbei 活动预约领云贝（自动预约、领奖）
func (c *SignIn) handleReserveYunbei(ctx context.Context, eapiRequest *eapi.Api) {
	if c.root.Cfg.Sign.YunbeiTask == nil || !c.root.Cfg.Sign.YunbeiTask.EnableReserve {
		c.cmd.Println("  提示: 预约领云贝任务已在配置文件中关闭(enableReserve = false)，跳过执行")
		return
	}

	c.cmd.Println("  👉 正在检查 [预约领云贝] 状态...")
	info, err := eapiRequest.YunbeiReserveInfo(ctx, &eapi.YunbeiReserveInfoReq{})
	if err != nil || info.Code != 200 {
		c.cmd.Printf("  ❌ 获取活动预约状态失败: %v\n", err)
		return
	}

	chineseStatus := getReserveStatusChinese(info.Data.Type)

	// 检测到未预约，去预约 (包含任何 NO_BOOK 子串状态, 例如 PREV_CLAIMED_NO_BOOKED, NO_PREV_NO_BOOK 等)
	if strings.Contains(info.Data.Type, "NO_BOOK") {
		c.cmd.Printf("  👉 检测到当前预约状态: [%s]，开始执行预约...\n", chineseStatus)
		booked, err := eapiRequest.YunbeiReserveBooked(ctx, &eapi.YunbeiReserveBookedReq{ReqId: info.Data.ReqId})
		if err == nil && booked.Code == 200 {
			c.cmd.Println("  ✅ 预约活动成功！")
		} else {
			c.cmd.Printf("  ❌ 预约活动失败: %v\n", err)
		}
		return
	}

	// 检测到已预约但未领，执行领取
	if info.Data.Type == "PREV_BOOKED_UNCLAIMED" {
		c.cmd.Printf("  👉 检测到当前预约状态: [%s]，开始领取云贝...\n", chineseStatus)
		receive, err := eapiRequest.YunbeiReserveRewardReceive(ctx, &eapi.YunbeiReserveRewardReceiveReq{
			ReqId:      info.Data.ReqId,
			CheckToken: "",
		})
		if err == nil && receive.Code == 200 {
			c.cmd.Printf("  🎉 预约活动奖励领取成功，获得云贝数量：%v\n", receive.Data.CurrentAmount)

			// 领取成功后，重新获取最新的预约信息并执行自动预约新活动
			c.cmd.Println("  👉 奖励领取成功，正在检查并自动预约新活动...")
			info, err = eapiRequest.YunbeiReserveInfo(ctx, &eapi.YunbeiReserveInfoReq{})
			if err == nil && info.Code == 200 {
				chineseStatus = getReserveStatusChinese(info.Data.Type)
				if strings.Contains(info.Data.Type, "NO_BOOK") {
					c.cmd.Printf("  👉 检测到当前预约状态: [%s]，开始执行预约...\n", chineseStatus)
					booked, err := eapiRequest.YunbeiReserveBooked(ctx, &eapi.YunbeiReserveBookedReq{ReqId: info.Data.ReqId})
					if err == nil && booked.Code == 200 {
						c.cmd.Println("  ✅ 预约活动成功！")
					} else {
						c.cmd.Printf("  ❌ 预约活动失败: %v\n", err)
					}
				} else {
					c.cmd.Printf("  ℹ️ 当前预约状态: [%s]，无须做预约操作\n", chineseStatus)
				}
			} else {
				c.cmd.Printf("  ❌ 重新获取活动预约状态失败: %v\n", err)
			}
		} else {
			c.cmd.Printf("  ❌ 领取预约活动奖励失败: %v\n", err)
		}
		return
	}

	c.cmd.Printf("  ℹ️ 当前预约状态: [%s]，无须做预约或领奖操作\n", chineseStatus)
}

// doFollowArtist 关注歌手（获取热门，关注第一个，3~10秒随机延迟后取消关注）
func (c *SignIn) doFollowArtist(ctx context.Context, eapiRequest *eapi.Api) {
	var artistIdStr = "6452" // 默认周杰伦
	var artistName = "周杰伦"

	hot, err := eapiRequest.ArtistHot(ctx, &eapi.ArtistHotReq{Offset: 0, Limit: 10})
	if err == nil && hot.Code == 200 && len(hot.Data) > 0 && len(hot.Data[0].Artists) > 0 {
		found := false
		for _, artist := range hot.Data[0].Artists {
			// 必须找一个目前未关注过的歌手，否则关注已关注的歌手不会触发任务完成
			if !artist.Followed {
				artistIdStr = fmt.Sprintf("%d", artist.Id)
				artistName = artist.Name
				found = true
				break
			}
		}
		if !found {
			// 如果全都是已关注的，只能默认选第一个
			artist := hot.Data[0].Artists[0]
			artistIdStr = fmt.Sprintf("%d", artist.Id)
			artistName = artist.Name
		}
	} else {
		c.cmd.Printf("  ℹ️ 获取热门歌手失败 (err=%v)，将使用默认歌手 [周杰伦] 进行关注歌手任务...\n", err)
	}

	// 1. 关注
	subResp, subErr := eapiRequest.ArtistSub(ctx, &eapi.ArtistSubReq{
		ArtistId: artistIdStr,
	})
	if subErr != nil {
		c.cmd.Printf("  ❌ 关注歌手 [%s] 失败: %v\n", artistName, subErr)
		return
	}
	if subResp.Code != 200 {
		c.cmd.Printf("  ❌ 关注歌手 [%s] 失败: Code=%d, Message=%s\n", artistName, subResp.Code, subResp.Message)
		return
	}

	// 2. 延迟 3~10 秒取消关注
	sleepSec := 3 + rand.Intn(8) // 3 ~ 10 秒
	c.cmd.Printf("  ⏳ 已成功关注歌手 [%s]，将在 %d 秒后取消关注以避嫌...\n", artistName, sleepSec)
	time.Sleep(time.Duration(sleepSec) * time.Second)

	unsubResp, unsubErr := eapiRequest.ArtistUnsub(ctx, &eapi.ArtistUnsubReq{
		ArtistIds: fmt.Sprintf("[%s]", artistIdStr),
	})
	if unsubErr != nil {
		c.cmd.Printf("  ❌ 取消关注歌手 [%s] 失败: %v\n", artistName, unsubErr)
		return
	}
	if unsubResp.Code != 200 {
		c.cmd.Printf("  ❌ 取消关注歌手 [%s] 失败: Code=%d, Message=%s\n", artistName, unsubResp.Code, unsubResp.Message)
		return
	}
	c.cmd.Printf("  ✅ 关注并取消关注歌手 [%s] 操作完毕\n", artistName)
}

// doLikeSong 红心歌曲（获取日推，红心第一个，3~10秒随机延迟后取消红心）
func (c *SignIn) doLikeSong(ctx context.Context, eapiRequest *eapi.Api) {
	var trackIdStr = "186016" // 默认晴天
	var songName = "晴天"

	songs, err := eapiRequest.DiscoveryRecommendSongs(ctx, &eapi.DiscoveryRecommendSongsReq{})
	if err == nil && songs.Code == 200 && len(songs.Data.DailySongs) > 0 {
		song := songs.Data.DailySongs[0]
		trackIdStr = fmt.Sprintf("%d", song.Id)
		songName = song.Name
	} else {
		c.cmd.Println("  ℹ️ 获取日推歌曲失败，将使用默认歌曲 [晴天] 进行红心歌曲任务...")
	}

	// 1. 红心歌曲
	_, likeErr := eapiRequest.SongLike(ctx, &eapi.SongLikeReq{
		TrackId:    trackIdStr,
		Like:       "true",
		Time:       "3",
		CheckToken: "",
	})
	if likeErr != nil {
		c.cmd.Printf("  ❌ 红心歌曲《%s》失败: %v\n", songName, likeErr)
		return
	}

	// 2. 延迟 3~10 秒取消红心
	sleepSec := 3 + rand.Intn(8) // 3 ~ 10 秒
	c.cmd.Printf("  ⏳ 已红心歌曲《%s》，将在 %d 秒后取消红心以避嫌...\n", songName, sleepSec)
	time.Sleep(time.Duration(sleepSec) * time.Second)

	_, _ = eapiRequest.SongLike(ctx, &eapi.SongLikeReq{
		TrackId:    trackIdStr,
		Like:       "false",
		Time:       "3",
		CheckToken: "",
	})
	c.cmd.Printf("  ✅ 红心并取消红心歌曲《%s》操作完毕\n", songName)
}

// getReserveStatusChinese 将英文预约状态转换为易读的中文
func getReserveStatusChinese(status string) string {
	switch status {
	case "NO_PREV_NO_BOOK":
		return "未领奖且未预约"
	case "PREV_CLAIMED_NO_BOOKED":
		return "上次奖励已领，新活动待预约"
	case "PREV_BOOKED_UNCLAIMED":
		return "已预约，有待领奖励"
	case "PREV_CLAIMED_BOOKED":
		return "上次奖励已领，新活动已预约"
	default:
		if strings.Contains(status, "BOOKED") || strings.Contains(status, "BOOK") {
			if strings.Contains(status, "NO_BOOK") || strings.Contains(status, "NO_BOOKED") {
				return "新活动待预约"
			}
			return "已预约活动"
		}
		return status
	}
}

// doCollectSong 收藏一首歌曲（获取用户歌单，添加第一首歌，3~10秒随机延迟后删除以避嫌）
func (c *SignIn) doCollectSong(ctx context.Context, request *weapi.Api, userId int64) {
	if userId == 0 {
		c.cmd.Println("  ❌ 收藏歌曲失败: 获取当前账号 Uid 失败")
		return
	}

	playlists, err := request.Playlist(ctx, &weapi.PlaylistReq{
		Uid:   fmt.Sprintf("%d", userId),
		Limit: "5",
	})
	if err != nil || playlists.Code != 200 || len(playlists.Playlist) == 0 {
		c.cmd.Printf("  ❌ 获取歌单列表失败: %v\n", err)
		return
	}

	pid := playlists.Playlist[0].Id
	trackId := int64(186016) // 默认晴天

	// 1. 添加歌曲到歌单 (收藏)
	_, addErr := request.PlaylistAddOrDel(ctx, &weapi.PlaylistAddOrDelReq{
		Op:       "add",
		Pid:      pid,
		TrackIds: types.IntsString{trackId},
		Imme:     true,
	})
	if addErr != nil {
		c.cmd.Printf("  ❌ 收藏歌曲失败: %v\n", addErr)
		return
	}

	// 2. 延迟 3~10 秒后从歌单中删除该歌曲 (取消收藏)
	sleepSec := 3 + rand.Intn(8) // 3 ~ 10 秒
	c.cmd.Printf("  ⏳ 已成功收藏歌曲，将在 %d 秒后从歌单 [%s] 中移除该歌曲...\n", sleepSec, playlists.Playlist[0].Name)
	time.Sleep(time.Duration(sleepSec) * time.Second)

	_, _ = request.PlaylistAddOrDel(ctx, &weapi.PlaylistAddOrDelReq{
		Op:       "del",
		Pid:      pid,
		TrackIds: types.IntsString{trackId},
		Imme:     true,
	})
	c.cmd.Println("  ✅ 收藏并取消收藏歌曲操作完毕")
}

// doPublishNote 调用 Note 服务完成图文笔记发布
func (c *SignIn) doPublishNote(ctx context.Context, cookieFile string) {
	n := NewNote(c.root, c.l)
	_, err := n.ExecuteForCookie(ctx, cookieFile)
	if err != nil {
		c.cmd.Printf("  ❌ 发布图文动态失败: %v\n", err)
	} else {
		c.cmd.Println("  ✅ 发布图文动态成功")
	}
}

// doPlayDailyRecommend 串行执行日推歌曲播放 31~45 分钟
func (c *SignIn) doPlayDailyRecommend(ctx context.Context, cookieFile string, taskName string) {
	c.cmd.Printf("  ⏳ [日推播放] 正在初始化播放服务 (%s)...\n", cookieFile)
	absPath, err := filepath.Abs(cookieFile)
	if err != nil {
		c.cmd.Printf("  ❌ [日推播放] (%s) 解析 cookie 路径失败: %v\n", cookieFile, err)
		return
	}

	networkCfg := *c.root.Cfg.Network
	networkCfg.Cookie.Filepath = absPath

	cli, err := api.NewClient(&networkCfg, c.l)
	if err != nil {
		c.cmd.Printf("  ❌ [日推播放] (%s) 实例化客户端失败: %v\n", cookieFile, err)
		return
	}
	defer cli.Close(ctx)
	request := weapi.New(cli)

	// 获取用户信息以过滤用户本人的歌
	user, err := request.GetUserInfo(ctx, &weapi.GetUserInfoReq{})
	if err != nil {
		c.cmd.Printf("  ❌ [日推播放] (%s) 验证登录状态失败: %v\n", cookieFile, err)
		return
	}
	if user.Code != 200 || user.Profile == nil || user.Account == nil {
		c.cmd.Printf("  ❌ [日推播放] (%s) 用户未登录或登录态已失效\n", cookieFile)
		return
	}
	nickname := user.Profile.Nickname
	userId := user.Account.Id

	// 拉取每日推荐歌曲
	recommendResp, err := request.RecommendSongs(ctx, &weapi.RecommendSongsReq{})
	if err != nil {
		c.cmd.Printf("  ❌ [日推播放] (%s) 拉取每日推荐失败: %v\n", cookieFile, err)
		return
	}
	if recommendResp.Code != 200 || len(recommendResp.Data.DailySongs) == 0 {
		c.cmd.Printf("  ❌ [日推播放] (%s) 接口返回暂无推荐歌曲\n", cookieFile)
		return
	}

	// 随机选择 31~45 分钟听歌
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	targetMinutes := 31 + r.Int63n(15) // 31 ~ 45
	targetSeconds := targetMinutes * 60

	var recommendSongIds []string
	var totalDurationSec int64
	var targetSongsCount int64

	for _, song := range recommendResp.Data.DailySongs {
		// 过滤掉本人歌曲以避嫌
		isSelf := false
		for _, ar := range song.Ar {
			if ar.Name == nickname {
				isSelf = true
				break
			}
		}
		if song.DjId > 0 && song.DjId == userId {
			isSelf = true
		}
		if isSelf {
			c.cmd.Printf("  ⚠️ [日推播放] (%s) 检测到推荐歌曲 %s (ID: %d) 是歌手本人歌曲，自动跳过\n", cookieFile, song.Name, song.Id)
			continue
		}

		songIdStr := fmt.Sprintf("%d", song.Id)
		recommendSongIds = append(recommendSongIds, songIdStr)

		songTime := song.Dt / 1000
		if songTime <= 0 {
			songTime = 180 // default 3 minutes
		}
		// 累加歌曲时长，并加上预计间隔（例如15秒）
		totalDurationSec += songTime + 15
		targetSongsCount++

		if totalDurationSec >= targetSeconds {
			break
		}
	}

	if len(recommendSongIds) == 0 {
		c.cmd.Printf("  ❌ [日推播放] (%s) 没有可播放的推荐歌曲\n", cookieFile)
		return
	}

	c.cmd.Printf("  👉 [日推播放] (%s) 已计算完成：目标播放时间为 %d 分钟，需要播放约 %d 首日推歌。\n", cookieFile, targetMinutes, targetSongsCount)

	// 实例化 PlayIds 并执行
	p := NewPlayIds(c.root, c.l)
	p.opts = PlayIdsOpts{
		RunMin:         targetSongsCount,
		RunMax:         targetSongsCount,
		GapMin:         10, // 与默认值保持一致
		GapMax:         30,
		CookieFile:     cookieFile,
		DisableMixPlay: true, // 已经是日推歌曲了，关闭多余的混听
	}

	_, err = p.executeForCookie(ctx, cookieFile, recommendSongIds)
	if err != nil {
		c.cmd.Printf("  ❌ [日推播放] (%s) 播放日推失败: %v\n", cookieFile, err)
	} else {
		c.cmd.Printf("  ✅ [日推播放] (%s) 成功播放日推歌曲完成\n", cookieFile)
	}
}
