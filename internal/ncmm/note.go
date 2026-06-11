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
	"strings"
	"time"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/api/eapi"
	"github.com/3899/ncmm/config"
	"github.com/3899/ncmm/pkg/log"

	"github.com/spf13/cobra"
)

type NoteOpts struct {
	CookieFile string
}

type Note struct {
	root *Root
	cmd  *cobra.Command
	l    *log.Logger
	rng  *rand.Rand
	opts NoteOpts
}

func NewNote(root *Root, l *log.Logger) *Note {
	c := &Note{
		root: root,
		l:    l,
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
		cmd: &cobra.Command{
			Use:     "note",
			Short:   "[need login] Auto-publish a text or image post (note)",
			Example: "  ncmm note --cookie-file run/cookie.json",
		},
	}
	c.addFlags()
	c.cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return c.execute(cmd.Context())
	}
	return c
}

func (c *Note) execute(ctx context.Context) error {
	cookieFile := c.opts.CookieFile
	if cookieFile == "" {
		if c.root.Cfg.Accounts != nil && c.root.Cfg.Accounts.Main != "" {
			cookieFile = c.root.Cfg.Accounts.Main
		} else {
			return fmt.Errorf("cookie file must be specified via --cookie-file or configured in config.yaml accounts.main")
		}
	}
	_, err := c.ExecuteForCookie(ctx, cookieFile)
	return err
}

func (c *Note) addFlags() {
	c.cmd.Flags().StringVar(&c.opts.CookieFile, "cookie-file", "", "指定额外的 cookie 文件路径")
}

func (c *Note) Command() *cobra.Command {
	return c.cmd
}

func (c *Note) getNoteConfig() (*config.NoteConf, error) {
	if c.root.Cfg.Note != nil {
		return c.root.Cfg.Note, nil
	}
	return nil, fmt.Errorf("note configuration is not set in config.yaml")
}

func (c *Note) ExecuteForCookie(ctx context.Context, cookieFile string) (int64, error) {
	log.Info("[note] 开始为账号 (%s) 执行发布图文动态...", cookieFile)

	cfg, err := c.getNoteConfig()
	if err != nil {
		return 0, err
	}

	networkCfg := c.root.Cfg.Network
	absPath, err := filepath.Abs(cookieFile)
	if err != nil {
		return 0, fmt.Errorf("解析 cookie 文件路径失败: %w", err)
	}

	networkCfgCopy := *networkCfg
	networkCfgCopy.Cookie.Filepath = absPath
	networkCfg = &networkCfgCopy

	cli, err := api.NewClient(networkCfg, c.l)
	if err != nil {
		return 0, fmt.Errorf("NewClient: %w", err)
	}
	defer cli.Close(ctx)

	eapiCli := eapi.New(cli)

	// 获取笔记内容 (支持 messagesFile 外部文本拉取与并集合并去重)
	var messages []string
	if len(cfg.Messages) > 0 {
		messages = append(messages, cfg.Messages...)
	}
	for _, file := range uniqueStrings(cfg.MessagesFile) {
		if file != "" {
			fileMsgs, err := parseMessagesFromFile(file)
			if err != nil {
				c.cmd.Printf("[note] [WARN] 读取 messagesFile (%s) 失败: %s，该来源将被跳过\n", file, err)
			} else {
				messages = append(messages, fileMsgs...)
			}
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
	for _, file := range uniqueStrings(cfg.TitlesFile) {
		if file != "" {
			fileTitles, err := parseMessagesFromFile(file)
			if err != nil {
				c.cmd.Printf("[note] [WARN] 读取 titlesFile (%s) 失败: %s，该来源将被跳过\n", file, err)
			} else {
				titles = append(titles, fileTitles...)
			}
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
		// 解析多源图片URL（支持直接图片URL及图片链接文件）
		uniqueImageUrls := uniqueStrings(c.resolveImageURLs(ctx, cfg.ImageURLs))
		if len(uniqueImageUrls) == 0 {
			return 0, fmt.Errorf("没有配置任何有效的图片URL，请在 config.yaml 的 note.imageUrls 中配置")
		}

		// 轮询重试下载，直至成功
		shuffledUrls := shuffleSlice(uniqueImageUrls)
		var tmpFile string
		var downloadErr error
		var selectedURL string
		for len(shuffledUrls) > 0 {
			idx := len(shuffledUrls) - 1
			selectedURL = shuffledUrls[idx]
			shuffledUrls = shuffledUrls[:idx]

			c.cmd.Printf("[note] 尝试下载图片: %s\n", selectedURL)
			tmpFile, downloadErr = downloadImageToTemp(ctx, selectedURL)
			if downloadErr == nil {
				c.cmd.Printf("[note] 图片下载成功: %s\n", selectedURL)
				break
			}
			c.cmd.Printf("[note] [WARN] 下载图片失败 (%s): %s，尝试下一张...\n", selectedURL, downloadErr)
		}

		if downloadErr != nil || tmpFile == "" {
			return 0, fmt.Errorf("所有候选图片下载均失败，最后一次错误: %w", downloadErr)
		}
		if tmpFile != selectedURL {
			defer os.Remove(tmpFile)
		}

		c.cmd.Printf("[note] 发布图文笔记: 标题=%q, 内容=%q, 图片=%s\n", title, msg, selectedURL)

		// 上传图片
		c.cmd.Println("[note] 上传图片...")
		var errUpload error
		pics, errUpload = eapiCli.EventUploadImage(ctx, tmpFile)
		if errUpload != nil {
			return 0, fmt.Errorf("上传图片失败: %w", errUpload)
		}
		c.cmd.Printf("[note] 图片上传成功: %s\n", pics)
	} else {
		c.cmd.Printf("[note] 发布普通笔记: 标题=%q, 内容=%q\n", title, msg)
	}

	// 发布动态
	c.cmd.Println("[note] 发布动态...")
	resp, err := eapiCli.EventPublish(ctx, &eapi.EventPublishReq{
		Title: title,
		Msg:   msg,
		Type:  "noresource",
		Pics:  pics,
	})
	if err != nil {
		return 0, fmt.Errorf("发布动态失败: %w", err)
	}
	if resp.Code != 200 {
		return 0, fmt.Errorf("发布动态失败: code=%d", resp.Code)
	}

	c.cmd.Printf("[note] ✅ 笔记发布成功! 动态ID: %d\n", resp.Id)

	// 检查是否自动删除发布后的笔记（默认为开启）
	autoDelete := true
	if cfg.AutoDelete != nil {
		autoDelete = *cfg.AutoDelete
	}

	if autoDelete {
		// 5 ~ 30 秒的随机延迟
		delay := 5 + c.rng.Intn(26)
		c.cmd.Printf("[note] 等待 %d 秒后执行自动删除...\n", delay)
		time.Sleep(time.Duration(delay) * time.Second)
		respDel, err := eapiCli.EventDelete(ctx, &eapi.EventDeleteReq{
			Id: resp.Id,
		})
		if err != nil {
			c.cmd.Printf("[note] ⚠️ 自动删除动态失败: %s\n", err)
		} else if respDel.Code != 200 {
			c.cmd.Printf("[note] ⚠️ 自动删除动态失败: code=%d\n", respDel.Code)
		} else {
			c.cmd.Printf("[note] 🗑️ 笔记已成功自动删除 (动态ID: %d)\n", resp.Id)
		}
	}
	return resp.Id, nil
}

// getRandomMessage 随机获取一条消息
func (c *Note) getRandomMessage(messages []string) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[c.rng.Intn(len(messages))]
}

// getRandomImageURL 随机获取一个图片URL
func (c *Note) getRandomImageURL(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	return urls[c.rng.Intn(len(urls))]
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

func parseMessagesFromFile(filePath string) ([]string, error) {
	var data []byte
	var err error
	if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
		resp, err := httpClient.Get(filePath)
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

func (c *Note) resolveImageURLs(ctx context.Context, sources []string) []string {
	var imageUrls []string

	for _, src := range uniqueStrings(sources) {
		if src == "" {
			continue
		}
		// Check if it's a remote URL
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			// Request it to check if it's a text list of URLs or a direct image
			resp, err := httpClient.Get(src)
			if err != nil {
				c.cmd.Printf("[note] [WARN] 请求图片源 (%s) 失败: %s，该源将被跳过\n", src, err)
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				contentType := strings.ToLower(resp.Header.Get("Content-Type"))
				// If it is an image, it is a direct image URL
				if strings.HasPrefix(contentType, "image/") {
					imageUrls = append(imageUrls, src)
				} else {
					// Otherwise, read as a list of URLs
					data, err := io.ReadAll(resp.Body)
					if err != nil {
						c.cmd.Printf("[note] [WARN] 读取图片源列表 (%s) 失败: %s\n", src, err)
						continue
					}
					lines := strings.Split(string(data), "\n")
					for _, line := range lines {
						line = strings.TrimSpace(line)
						if line != "" && !strings.HasPrefix(line, "#") {
							imageUrls = append(imageUrls, line)
						}
					}
				}
			} else {
				c.cmd.Printf("[note] [WARN] 请求图片源 (%s) 返回状态码: %d\n", src, resp.StatusCode)
			}
		} else {
			// Local path
			if strings.HasSuffix(strings.ToLower(src), ".txt") {
				// Read file as list of URLs
				data, err := os.ReadFile(src)
				if err != nil {
					c.cmd.Printf("[note] [WARN] 读取本地图片源列表 (%s) 失败: %s\n", src, err)
					continue
				}
				lines := strings.Split(string(data), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						imageUrls = append(imageUrls, line)
					}
				}
			} else {
				// Direct local image file
				imageUrls = append(imageUrls, src)
			}
		}
	}
	return imageUrls
}
