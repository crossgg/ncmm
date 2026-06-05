package config

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/3899/ncmm/api"
	"github.com/3899/ncmm/pkg/database"
	"github.com/3899/ncmm/pkg/log"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var HomeDir string

var (
	//go:embed config.yaml
	defaultConfigByte []byte
	defaultConfig     *Config
)

func init() {
	var err error
	HomeDir, err = os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	if err := yaml.Unmarshal(defaultConfigByte, &defaultConfig); err != nil {
		panic(fmt.Sprintf("defaultConfig.Unmarshal: %s", err))
	}
	defaultConfig.syncPrimaryCookie()
	// defaultConfig.ReplaceMagicVariables("HOME", HomeDir)
	if err := defaultConfig.Validate(); err != nil {
		panic(fmt.Sprintf("defaultConfig.Validate: %s", err))
	}
}

type AccountsConf struct {
	Primary   string   `json:"primary" yaml:"primary"`
	Secondary []string `json:"secondary" yaml:"secondary"`
}

type SignConf struct {
	EnablePrimary     bool `json:"enablePrimary" yaml:"enablePrimary"`
	EnableSecondaries bool `json:"enableSecondaries" yaml:"enableSecondaries"`
	IdentityCacheDays *int `json:"identityCacheDays" yaml:"identityCacheDays"`
}

type MixPlayConf struct {
	Enabled             bool    `json:"enabled" yaml:"enabled"`
	DailyRecommendRatio float64 `json:"dailyRecommendRatio" yaml:"dailyRecommendRatio"`
	CountTarget         bool    `json:"countTarget" yaml:"countTarget"`
}

type PlayIdsConfig struct {
	DailyMin          int64  `json:"daily_min" yaml:"daily_min"`
	DailyMax          int64  `json:"daily_max" yaml:"daily_max"`
	RunMin            int64  `json:"run_min" yaml:"run_min"`
	RunMax            int64  `json:"run_max" yaml:"run_max"`
	GapMin            int64  `json:"gap_min" yaml:"gap_min"`
	GapMax            int64  `json:"gap_max" yaml:"gap_max"`
	IDs               string `json:"ids" yaml:"ids"`
	IDsFile           string `json:"idsFile" yaml:"idsFile"`
	EnablePrimary     bool   `json:"enablePrimary" yaml:"enablePrimary"`
	EnableSecondaries bool   `json:"enableSecondaries" yaml:"enableSecondaries"`
}

type Config struct {
	v           *viper.Viper
	Version     string           `json:"version" yaml:"version"`
	Accounts    *AccountsConf    `json:"accounts" yaml:"accounts"`
	Log         *log.Config      `json:"log" yaml:"log"`
	Network     *api.Config      `json:"network" yaml:"network"`
	Database    *database.Config `json:"database" yaml:"database"`
	PlayIds     *PlayIdsConfig   `json:"playids" yaml:"playids"`
	Sign        *SignConf        `json:"sign" yaml:"sign"`
	MixPlay     *MixPlayConf     `json:"mixPlay" yaml:"mixPlay"`
	MusicianVip *MusicianVipConf `json:"musicianVip" yaml:"musicianVip"`
}

// MusicianVipConf 音乐人黑胶会员任务配置
type MusicianVipConf struct {
	Note MusicianVipNoteConf `json:"note" yaml:"note"`
	Play MusicianVipPlayConf `json:"play" yaml:"play"`
}

// MusicianVipNoteConf 笔记发布任务配置
type MusicianVipNoteConf struct {
	Titles       []string `json:"titles" yaml:"titles"`
	TitlesFile   string   `json:"titlesFile" yaml:"titlesFile"`
	Messages     []string `json:"messages" yaml:"messages"`
	MessagesFile string   `json:"messagesFile" yaml:"messagesFile"`
	ImageURLs    []string `json:"imageUrls" yaml:"imageUrls"`
	Type         int      `json:"type" yaml:"type"`
	AutoDelete   *bool    `json:"autoDelete" yaml:"autoDelete"`
}

// MusicianVipPlayConf 播放任务配置
type MusicianVipPlayConf struct {
	IDs     string `json:"ids" yaml:"ids"`
	IDsFile string `json:"idsFile" yaml:"idsFile"`
	RunMin  int64  `json:"run_min" yaml:"run_min"`
	RunMax  int64  `json:"run_max" yaml:"run_max"`
	GapMin  int64  `json:"gap_min" yaml:"gap_min"`
	GapMax  int64  `json:"gap_max" yaml:"gap_max"`
}

func (c *Config) Validate() error {
	if c.Sign != nil {
		if c.Sign.IdentityCacheDays == nil {
			days := 30
			c.Sign.IdentityCacheDays = &days
		}
	}
	return nil
}

func GetDefault() *Config {
	return defaultConfig
}

func New(cfgPath ...string) (*Config, error) {
	var (
		conf Config
		opts = func(m *mapstructure.DecoderConfig) {
			m.TagName = "yaml"
		}
		_cfgPath string
	)
	if len(cfgPath) > 0 {
		_cfgPath = cfgPath[0]
	}

	v := viper.New()
	v.SetTypeByDefaultValue(true)
	v.SetEnvPrefix("ncmm")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.AllowEmptyEnv(true)
	v.SetConfigType("yaml")
	v.SetConfigFile(_cfgPath)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("ReadInConfig: %w", err)
	}
	if err := v.UnmarshalExact(&conf, opts); err != nil {
		return nil, fmt.Errorf("UnmarshalExact: %w", err)
	}
	conf.syncPrimaryCookie()
	if err := conf.Validate(); err != nil {
		return nil, err
	}
	return &conf, nil
}

// ReplaceMagicVariables 替换配置文件中的魔法变量。注意该方法只能调用一次再次调用则不会生效.
func (c *Config) ReplaceMagicVariables(name, value string) (*Config, bool) {

	var (
		isset   bool
		mapping = func(k string) string {
			switch k {
			case name:
				isset = true
				return value
			}
			return ""
		}
	)

	c.Log.Rotate.Filename = os.Expand(c.Log.Rotate.Filename, mapping)
	c.Network.Cookie.Filepath = os.Expand(c.Network.Cookie.Filepath, mapping)
	c.Database.Path = os.Expand(c.Database.Path, mapping)
	if c.Accounts != nil {
		c.Accounts.Primary = os.Expand(c.Accounts.Primary, mapping)
		for i, sec := range c.Accounts.Secondary {
			c.Accounts.Secondary[i] = os.Expand(sec, mapping)
		}
	}
	if c.PlayIds != nil && c.PlayIds.IDsFile != "" {
		c.PlayIds.IDsFile = os.Expand(c.PlayIds.IDsFile, mapping)
	}
	if c.MusicianVip != nil && c.MusicianVip.Play.IDsFile != "" {
		c.MusicianVip.Play.IDsFile = os.Expand(c.MusicianVip.Play.IDsFile, mapping)
	}
	if c.MusicianVip != nil && c.MusicianVip.Note.MessagesFile != "" {
		c.MusicianVip.Note.MessagesFile = os.Expand(c.MusicianVip.Note.MessagesFile, mapping)
	}
	if c.MusicianVip != nil && c.MusicianVip.Note.TitlesFile != "" {
		c.MusicianVip.Note.TitlesFile = os.Expand(c.MusicianVip.Note.TitlesFile, mapping)
	}
	return c, isset
}

func (c *Config) syncPrimaryCookie() {
	if c.Accounts != nil && c.Accounts.Primary != "" {
		if c.Network != nil {
			c.Network.Cookie.Filepath = c.Accounts.Primary
		}
	}
}

