// Event (动态) API — 发送/删除动态
// Ported from https://github.com/XiaoMengXinX/Music163Api-Go
// Endpoints:
//   - /api/share/friends/resource (发送动态, EAPI)
//   - /api/event/delete (删除动态, EAPI)

package eapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/api/types"
	"github.com/3899/ncmm/pkg/utils"
)

// ===================== 发送动态 (EventPublish) =====================

// EventPublishReq 发送动态请求
type EventPublishReq struct {
	// Title 动态标题 (新版图文笔记支持标题)
	Title string `json:"title,omitempty"`
	// Msg 动态文本内容
	Msg string `json:"msg"`
	// Type 动态类型, 发纯文本用 "noresource"
	Type string `json:"type"`
	// Uuid 唯一标识 (32位hex), 为空时自动生成
	Uuid string `json:"uuid"`
	// Pics 图片信息JSON字符串, 由 EventUploadImage 生成; 无图片时留空
	Pics string `json:"pics,omitempty"`
	// AddComment 是否添加评论, 默认 false
	AddComment bool `json:"addComment"`
	// PrivacySetting 隐私设置
	PrivacySetting string `json:"privacySetting,omitempty"`
	// SocialSpaceVisible 是否可见空间 默认 1
	SocialSpaceVisible int `json:"socialSpaceVisible,omitempty"`
}

// EventPublishResp 发送动态响应
type EventPublishResp struct {
	types.RespCommon[any]
	UserId int   `json:"userId"`
	Id     int64 `json:"id"`
	Event  struct {
		DiscussId    string `json:"discussId"`
		ForwardCount int    `json:"forwardCount"`
		Json         string `json:"json"`
		Uuid         string `json:"uuid"`
		EventTime    int64  `json:"eventTime"`
	} `json:"event"`
}

// EventPublish 发送动态 (支持图片)
// 接口: /api/share/friends/resource
// 加密: EAPI
// 需要登录
func (a *Api) EventPublish(ctx context.Context, req *EventPublishReq) (*EventPublishResp, error) {
	// 自动生成 UUID
	if req.Uuid == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("generate uuid: %w", err)
		}
		req.Uuid = hex.EncodeToString(b)
	}
	if req.Type == "" {
		req.Type = "noresource"
	}
	if req.PrivacySetting == "" {
		req.PrivacySetting = "0"
	}
	if req.SocialSpaceVisible == 0 {
		req.SocialSpaceVisible = 1
	}

	var (
		url   = "https://interface3.music.163.com/eapi/note/share/friends/resource"
		reply EventPublishResp
		opts  = api.NewOptions()
	)
	opts.CryptoMode = api.CryptoModeEAPI

	resp, err := a.client.Request(ctx, url, req, &reply, opts)
	if err != nil {
		return nil, fmt.Errorf("Request: %w", err)
	}
	_ = resp
	return &reply, nil
}

// ===================== 删除动态 (EventDelete) =====================

// EventDeleteReq 删除动态请求
type EventDeleteReq struct {
	// Id 动态ID
	Id int64 `json:"id"`
}

// EventDeleteResp 删除动态响应
type EventDeleteResp struct {
	types.RespCommon[any]
}

// EventDelete 删除动态
// 接口: /api/event/delete
// 加密: EAPI
// 需要登录
func (a *Api) EventDelete(ctx context.Context, req *EventDeleteReq) (*EventDeleteResp, error) {
	var (
		url   = "https://interface3.music.163.com/eapi/event/delete"
		reply EventDeleteResp
		opts  = api.NewOptions()
	)
	opts.CryptoMode = api.CryptoModeEAPI

	resp, err := a.client.Request(ctx, url, req, &reply, opts)
	if err != nil {
		return nil, fmt.Errorf("Request: %w", err)
	}
	_ = resp
	return &reply, nil
}

// ===================== 图片上传 (Event) =====================

// eventImgPicInfo 事件图片信息 (用于动态图片参数)
type eventImgPicInfo struct {
	OriginId      string `json:"originId"`
	SquareId      string `json:"squareId"`
	RectangleId   string `json:"rectangleId"`
	PcSquareId    string `json:"pcSquareId"`
	PcRectangleId string `json:"pcRectangleId"`
	OriginJpgId   string `json:"originJpgId"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	Index         int    `json:"index"`
}

// eventNosTokenResp Nos Token 分配响应
type eventNosTokenResp struct {
	types.RespCommon[any]
	Result struct {
		Bucket    string `json:"bucket"`
		DocId     string `json:"docId"`
		ObjectKey string `json:"objectKey"`
		Token     string `json:"token"`
	} `json:"result"`
}

// eventUploadImgResp 上传事件图片信息响应
type eventUploadImgResp struct {
	types.RespCommon[any]
	PicSubtype string `json:"picSubtype"`
	PicInfo    struct {
		OriginId    int64  `json:"originId"`
		SquareId    int64  `json:"squareId"`
		RectangleId int64  `json:"rectangleId"`
		Format      string `json:"format"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
	} `json:"picInfo"`
}

// EventUploadImage 上传动态图片
// 完成三步操作: 获取nos token → 上传文件 → 获取图片信息
// 返回值为 EventPublishReq.Pics 所需的 JSON 字符串
func (a *Api) EventUploadImage(ctx context.Context, filePath string) (string, error) {
	// 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	md5, err := utils.MD5Hex(data)
	if err != nil {
		return "", fmt.Errorf("MD5Hex: %w", err)
	}

	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	filename := filepath.Base(filePath)
	fileSize := int64(len(data))

	// Step 1: 获取 Nos Token
	var (
		tokenURL = "https://music.163.com/eapi/nos/token/alloc"
		tokenReq = struct {
			Filename   string `json:"filename"`
			Local      string `json:"local"`
			NosProduct int    `json:"nos_product"`
			FileSize   int64  `json:"fileSize"`
			Md5        string `json:"md5"`
			Ext        string `json:"ext"`
			Type       string `json:"type"`
		}{
			Filename:   filename,
			Local:      "false",
			NosProduct: 0,
			FileSize:   fileSize,
			Md5:        md5,
			Ext:        ext,
			Type:       "image",
		}
		tokenReply eventNosTokenResp
		tokenOpts  = api.NewOptions()
	)
	tokenOpts.CryptoMode = api.CryptoModeEAPI

	resp, err := a.client.Request(ctx, tokenURL, tokenReq, &tokenReply, tokenOpts)
	if err != nil {
		return "", fmt.Errorf("get nos token: %w", err)
	}
	_ = resp

	if tokenReply.Code != 200 {
		return "", fmt.Errorf("get nos token failed: code=%d, msg=%s", tokenReply.Code, tokenReply.Message)
	}

	// Step 2: 上传文件到 NOS
	uploadNode, err := a.getUploadNode(ctx, "cloudmusic")
	if err != nil {
		return "", fmt.Errorf("get upload node: %w", err)
	}

	contentType := utils.DetectContentType(data, ext)
	uploadURL := fmt.Sprintf("%s/%s/%s?version=1.0&offset=0&complete=true",
		uploadNode, tokenReply.Result.Bucket, tokenReply.Result.ObjectKey)

	headers := map[string]string{
		"X-Nos-Token":  tokenReply.Result.Token,
		"Content-Type": contentType,
	}

	if err := a.rawUpload(ctx, uploadURL, headers, data); err != nil {
		return "", fmt.Errorf("upload file: %w", err)
	}

	// Step 3: 获取事件图片信息
	var (
		imgURL  = "https://music.163.com/eapi/upload/event/img/v1"
		imgReq  = struct {
			ImgId  string `json:"imgid"`
			Format string `json:"format"`
		}{
			ImgId:  tokenReply.Result.DocId,
			Format: ext,
		}
		imgReply eventUploadImgResp
		imgOpts  = api.NewOptions()
	)
	imgOpts.CryptoMode = api.CryptoModeEAPI

	resp, err = a.client.Request(ctx, imgURL, imgReq, &imgReply, imgOpts)
	if err != nil {
		return "", fmt.Errorf("get event img info: %w", err)
	}
	_ = resp

	if imgReply.Code != 200 {
		return "", fmt.Errorf("get event img info failed: code=%d, msg=%s", imgReply.Code, imgReply.Message)
	}

	// 构建 pics JSON
	originIdStr := strconv.FormatInt(imgReply.PicInfo.OriginId, 10)
	squareIdStr := strconv.FormatInt(imgReply.PicInfo.SquareId, 10)
	rectangleIdStr := strconv.FormatInt(imgReply.PicInfo.RectangleId, 10)

	picInfo := eventImgPicInfo{
		OriginId:      originIdStr,
		SquareId:      squareIdStr,
		RectangleId:   rectangleIdStr,
		PcSquareId:    squareIdStr,
		PcRectangleId: rectangleIdStr,
		OriginJpgId:   originIdStr,
		Width:         imgReply.PicInfo.Width,
		Height:        imgReply.PicInfo.Height,
		Index:         0,
	}
	picsBytes, err := json.Marshal([]eventImgPicInfo{picInfo})
	if err != nil {
		return "", fmt.Errorf("marshal pics: %w", err)
	}

	return string(picsBytes), nil
}

// EventUploadImages 批量上传动态图片
// 返回值为 EventPublishReq.Pics 所需的 JSON 字符串
func (a *Api) EventUploadImages(ctx context.Context, filePaths []string) (string, error) {
	var pics []eventImgPicInfo
	for _, fp := range filePaths {
		data, err := os.ReadFile(fp)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", fp, err)
		}

		md5, err := utils.MD5Hex(data)
		if err != nil {
			return "", fmt.Errorf("MD5Hex: %w", err)
		}

		ext := strings.TrimPrefix(filepath.Ext(fp), ".")
		filename := filepath.Base(fp)
		fileSize := int64(len(data))

		// 获取 Nos Token
		var (
			tokenURL = "https://music.163.com/eapi/nos/token/alloc"
			tokenReq = struct {
				Filename   string `json:"filename"`
				Local      string `json:"local"`
				NosProduct int    `json:"nos_product"`
				FileSize   int64  `json:"fileSize"`
				Md5        string `json:"md5"`
				Ext        string `json:"ext"`
				Type       string `json:"type"`
			}{
				Filename:   filename,
				Local:      "false",
				NosProduct: 0,
				FileSize:   fileSize,
				Md5:        md5,
				Ext:        ext,
				Type:       "image",
			}
			tokenReply eventNosTokenResp
			tokenOpts  = api.NewOptions()
		)
		tokenOpts.CryptoMode = api.CryptoModeEAPI

		if _, err := a.client.Request(ctx, tokenURL, tokenReq, &tokenReply, tokenOpts); err != nil {
			return "", fmt.Errorf("get nos token for %s: %w", fp, err)
		}
		if tokenReply.Code != 200 {
			return "", fmt.Errorf("get nos token failed for %s: code=%d", fp, tokenReply.Code)
		}

		// 上传文件
		uploadNode, err := a.getUploadNode(ctx, "cloudmusic")
		if err != nil {
			return "", fmt.Errorf("get upload node: %w", err)
		}

		contentType := utils.DetectContentType(data, ext)
		uploadURL := fmt.Sprintf("%s/%s/%s?version=1.0&offset=0&complete=true",
			uploadNode, tokenReply.Result.Bucket, tokenReply.Result.ObjectKey)

		headers := map[string]string{
			"X-Nos-Token":  tokenReply.Result.Token,
			"Content-Type": contentType,
		}

		if err := a.rawUpload(ctx, uploadURL, headers, data); err != nil {
			return "", fmt.Errorf("upload %s: %w", fp, err)
		}

		// 获取图片信息
		var (
			imgURL  = "https://music.163.com/eapi/upload/event/img/v1"
			imgReq  = struct {
				ImgId  string `json:"imgid"`
				Format string `json:"format"`
			}{
				ImgId:  tokenReply.Result.DocId,
				Format: ext,
			}
			imgReply eventUploadImgResp
			imgOpts  = api.NewOptions()
		)
		imgOpts.CryptoMode = api.CryptoModeEAPI

		if _, err := a.client.Request(ctx, imgURL, imgReq, &imgReply, imgOpts); err != nil {
			return "", fmt.Errorf("get event img info for %s: %w", fp, err)
		}
		if imgReply.Code != 200 {
			return "", fmt.Errorf("get event img info failed for %s: code=%d", fp, imgReply.Code)
		}

		originIdStr := strconv.FormatInt(imgReply.PicInfo.OriginId, 10)
		squareIdStr := strconv.FormatInt(imgReply.PicInfo.SquareId, 10)
		rectangleIdStr := strconv.FormatInt(imgReply.PicInfo.RectangleId, 10)

		pics = append(pics, eventImgPicInfo{
			OriginId:      originIdStr,
			SquareId:      squareIdStr,
			RectangleId:   rectangleIdStr,
			PcSquareId:    squareIdStr,
			PcRectangleId: rectangleIdStr,
			OriginJpgId:   originIdStr,
			Width:         imgReply.PicInfo.Width,
			Height:        imgReply.PicInfo.Height,
			Index:         len(pics),
		})
	}

	picsBytes, err := json.Marshal(pics)
	if err != nil {
		return "", fmt.Errorf("marshal pics: %w", err)
	}
	return string(picsBytes), nil
}
