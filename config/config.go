package config

import (
	_ "embed"
	"fmt"
	"os"
	"reflect"
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
	defaultConfig.syncMainCookie()
	// defaultConfig.ReplaceMagicVariables("HOME", HomeDir)
	if err := defaultConfig.Validate(); err != nil {
		panic(fmt.Sprintf("defaultConfig.Validate: %s", err))
	}
}

type AccountsConf struct {
	Main      string   `json:"main" yaml:"main"`
	Primary   string   `json:"primary" yaml:"primary"` // 兼容旧版
	Secondary []string `json:"secondary" yaml:"secondary"`
}

type YunbeiTaskConf struct {
	EnableViewVipCenter      bool `json:"enableViewVipCenter" yaml:"enableViewVipCenter"`
	EnableLikeComment        bool `json:"enableLikeComment" yaml:"enableLikeComment"`
	EnableListenIndie        bool `json:"enableListenIndie" yaml:"enableListenIndie"`
	EnableReserve            bool `json:"enableReserve" yaml:"enableReserve"`
	EnableFollowArtist       bool `json:"enableFollowArtist" yaml:"enableFollowArtist"`
	EnableLikeSong           bool `json:"enableLikeSong" yaml:"enableLikeSong"`
	EnableCollectSong        bool `json:"enableCollectSong" yaml:"enableCollectSong"`
	EnablePublishNote        bool `json:"enablePublishNote" yaml:"enablePublishNote"`
	EnablePlayDailyRecommend bool `json:"enablePlayDailyRecommend" yaml:"enablePlayDailyRecommend"`
}

type SignConf struct {
	EnableMain        bool            `json:"enableMain" yaml:"enableMain"`
	EnablePrimary     bool            `json:"enablePrimary" yaml:"enablePrimary"` // 兼容旧版
	EnableSecondaries bool            `json:"enableSecondaries" yaml:"enableSecondaries"`
	IdentityCacheDays *int            `json:"identityCacheDays" yaml:"identityCacheDays"`
	YunbeiTask        *YunbeiTaskConf `json:"yunbeiTask" yaml:"yunbeiTask"`
	Automatic         bool            `json:"automatic" yaml:"automatic"`
}

type TaskConf struct {
	Sign        bool `json:"sign" yaml:"sign"`
	PlayIds     bool `json:"playids" yaml:"playids"`
	MusicianVip bool `json:"musicianVip" yaml:"musicianVip"`
	Note        bool `json:"note" yaml:"note"`
	FansGroup   bool `json:"fansGroup" yaml:"fansGroup"`
}

// FansGroupConf 乐迷团任务配置
type FansGroupConf struct {
	EnableMain        bool  `json:"enableMain" yaml:"enableMain"`
	EnableSecondaries bool  `json:"enableSecondaries" yaml:"enableSecondaries"`
	AutoDeleteNote    *bool `json:"autoDeleteNote" yaml:"autoDeleteNote"`
}

type MixPlayConf struct {
	Enabled             bool    `json:"enabled" yaml:"enabled"`
	DailyRecommendRatio float64 `json:"dailyRecommendRatio" yaml:"dailyRecommendRatio"`
	CountTarget         bool    `json:"countTarget" yaml:"countTarget"`
}

// StringOrSlice supports unmarshaling from either a single string or an array of strings.
type StringOrSlice []string

// UnmarshalYAML implements yaml.Unmarshaler
func (s *StringOrSlice) UnmarshalYAML(value *yaml.Node) error {
	var str string
	if err := value.Decode(&str); err == nil {
		*s = []string{str}
		return nil
	}
	var slice []string
	if err := value.Decode(&slice); err == nil {
		*s = slice
		return nil
	}
	return fmt.Errorf("failed to unmarshal StringOrSlice at line %d", value.Line)
}

type PlayIdsConfig struct {
	DailyMin          int64         `json:"daily_min" yaml:"daily_min"`
	DailyMax          int64         `json:"daily_max" yaml:"daily_max"`
	RunMin            int64         `json:"run_min" yaml:"run_min"`
	RunMax            int64         `json:"run_max" yaml:"run_max"`
	GapMin            int64         `json:"gap_min" yaml:"gap_min"`
	GapMax            int64         `json:"gap_max" yaml:"gap_max"`
	IDs               string        `json:"ids" yaml:"ids"`
	IDsFile           StringOrSlice `json:"idsFile" yaml:"idsFile"`
	EnableMain        bool          `json:"enableMain" yaml:"enableMain"`
	EnablePrimary     bool          `json:"enablePrimary" yaml:"enablePrimary"` // 兼容旧版
	EnableSecondaries bool          `json:"enableSecondaries" yaml:"enableSecondaries"`
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
	Note        *NoteConf        `json:"note" yaml:"note"`
	MusicianVip *MusicianVipConf `json:"musicianVip" yaml:"musicianVip"`
	FansGroup   *FansGroupConf   `json:"fansGroup" yaml:"fansGroup"`
	Task        *TaskConf        `json:"task" yaml:"task"`
}

// MusicianVipConf 音乐人黑胶会员任务配置
type MusicianVipConf struct {
	Play MusicianVipPlayConf `json:"play" yaml:"play"`
}

// NoteConf 笔记发布公共配置
type NoteConf struct {
	Titles       []string      `json:"titles" yaml:"titles"`
	TitlesFile   StringOrSlice `json:"titlesFile" yaml:"titlesFile"`
	Messages     []string      `json:"messages" yaml:"messages"`
	MessagesFile StringOrSlice `json:"messagesFile" yaml:"messagesFile"`
	ImageURLs    StringOrSlice `json:"imageUrls" yaml:"imageUrls"`
	Type         int           `json:"type" yaml:"type"`
	AutoDelete   *bool         `json:"autoDelete" yaml:"autoDelete"`
}

// MusicianVipPlayConf 播放任务配置
type MusicianVipPlayConf struct {
	IDs     string        `json:"ids" yaml:"ids"`
	IDsFile StringOrSlice `json:"idsFile" yaml:"idsFile"`
	RunMin  int64         `json:"run_min" yaml:"run_min"`
	RunMax  int64         `json:"run_max" yaml:"run_max"`
	GapMin  int64         `json:"gap_min" yaml:"gap_min"`
	GapMax  int64         `json:"gap_max" yaml:"gap_max"`
}

func (c *Config) Validate() error {
	if c.Accounts != nil {
		if c.Accounts.Main == "" && c.Accounts.Primary != "" {
			c.Accounts.Main = c.Accounts.Primary
		}
	}
	if c.PlayIds != nil {
		if !c.PlayIds.EnableMain && c.PlayIds.EnablePrimary {
			c.PlayIds.EnableMain = c.PlayIds.EnablePrimary
		}
	}
	if c.Sign != nil {
		if !c.Sign.EnableMain && c.Sign.EnablePrimary {
			c.Sign.EnableMain = c.Sign.EnablePrimary
		}
		if c.Sign.IdentityCacheDays == nil {
			days := 30
			c.Sign.IdentityCacheDays = &days
		}
		if c.Sign.YunbeiTask == nil {
			c.Sign.YunbeiTask = &YunbeiTaskConf{
				EnableViewVipCenter:      true,
				EnableLikeComment:        true,
				EnableListenIndie:        true,
				EnableReserve:            true,
				EnableFollowArtist:       true,
				EnableLikeSong:           true,
				EnableCollectSong:        true,
				EnablePublishNote:        true,
				EnablePlayDailyRecommend: false,
			}
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
			m.DecodeHook = mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.StringToSliceHookFunc(","),
				func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
					if t == reflect.TypeOf(StringOrSlice{}) {
						switch v := data.(type) {
						case string:
							return StringOrSlice{v}, nil
						case []interface{}:
							var res StringOrSlice
							for _, item := range v {
								if s, ok := item.(string); ok {
									res = append(res, s)
								} else {
									return nil, fmt.Errorf("invalid element type in StringOrSlice: %T", item)
								}
							}
							return res, nil
						case []string:
							return StringOrSlice(v), nil
						}
					}
					return data, nil
				},
			)
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
	conf.syncMainCookie()
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
		c.Accounts.Main = os.Expand(c.Accounts.Main, mapping)
		c.Accounts.Primary = os.Expand(c.Accounts.Primary, mapping)
		for i, sec := range c.Accounts.Secondary {
			c.Accounts.Secondary[i] = os.Expand(sec, mapping)
		}
	}
	if c.PlayIds != nil {
		for i, file := range c.PlayIds.IDsFile {
			c.PlayIds.IDsFile[i] = os.Expand(file, mapping)
		}
	}
	if c.MusicianVip != nil {
		for i, file := range c.MusicianVip.Play.IDsFile {
			c.MusicianVip.Play.IDsFile[i] = os.Expand(file, mapping)
		}
	}
	if c.Note != nil {
		for i, file := range c.Note.MessagesFile {
			c.Note.MessagesFile[i] = os.Expand(file, mapping)
		}
		for i, file := range c.Note.TitlesFile {
			c.Note.TitlesFile[i] = os.Expand(file, mapping)
		}
		for i, file := range c.Note.ImageURLs {
			c.Note.ImageURLs[i] = os.Expand(file, mapping)
		}
	}
	return c, isset
}

func (c *Config) syncMainCookie() {
	if c.Accounts != nil && c.Accounts.Main != "" {
		if c.Network != nil {
			c.Network.Cookie.Filepath = c.Accounts.Main
		}
	}
}

// MigrateConfigFile 自动将旧的 primary / enablePrimary 配置字段升级迁移为 main / enableMain
func MigrateConfigFile(cfgPath string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}

	modified := migrateNode(&root)
	if !modified {
		return nil
	}

	output, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, output, 0644)
}

func migrateNode(node *yaml.Node) bool {
	var modified bool
	if node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			if migrateNode(child) {
				modified = true
			}
		}
		return modified
	}

	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]

			// Case 1: accounts mapping
			if keyNode.Value == "accounts" && valNode.Kind == yaml.MappingNode {
				for j := 0; j < len(valNode.Content); j += 2 {
					subKey := valNode.Content[j]
					if subKey.Value == "primary" {
						subKey.Value = "main"
						modified = true
					}
				}
			}

			// Case 2: playids mapping
			if keyNode.Value == "playids" && valNode.Kind == yaml.MappingNode {
				for j := 0; j < len(valNode.Content); j += 2 {
					subKey := valNode.Content[j]
					if subKey.Value == "enablePrimary" {
						subKey.Value = "enableMain"
						modified = true
					}
				}
			}

			// Case 3: sign mapping
			if keyNode.Value == "sign" && valNode.Kind == yaml.MappingNode {
				for j := 0; j < len(valNode.Content); j += 2 {
					subKey := valNode.Content[j]
					if subKey.Value == "enablePrimary" {
						subKey.Value = "enableMain"
						modified = true
					}
				}
			}

			if migrateNode(valNode) {
				modified = true
			}
		}
	}

	if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			if migrateNode(child) {
				modified = true
			}
		}
	}

	return modified
}

// UpdateAccountsInFile 更新配置文件中的 accounts 并为每个账号添加昵称注释，同时保持原有文件的注释和排版
func UpdateAccountsInFile(cfgPath string, mainPath string, mainNickname string, secondaryPaths []string, secondaryNicknames []string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}

	var docMapping *yaml.Node
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		docMapping = root.Content[0]
	}

	if docMapping == nil || docMapping.Kind != yaml.MappingNode {
		docMapping = &yaml.Node{Kind: yaml.MappingNode}
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{docMapping}
	}

	// 查找或创建 'accounts' 映射
	var accountsMapping *yaml.Node
	for i := 0; i < len(docMapping.Content); i += 2 {
		if docMapping.Content[i].Value == "accounts" {
			if docMapping.Content[i+1].Kind == yaml.MappingNode {
				accountsMapping = docMapping.Content[i+1]
			}
			break
		}
	}

	if accountsMapping == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "accounts"}
		accountsMapping = &yaml.Node{Kind: yaml.MappingNode}
		docMapping.Content = append(docMapping.Content, keyNode, accountsMapping)
	}

	// 收集现有的注释以便复用
	oldComments := make(map[string]string)
	for i := 0; i < len(accountsMapping.Content); i += 2 {
		key := accountsMapping.Content[i].Value
		valNode := accountsMapping.Content[i+1]
		if key == "main" || key == "primary" {
			if valNode.LineComment != "" {
				oldComments["main"] = valNode.LineComment
			}
		}
		if key == "secondary" && valNode.Kind == yaml.SequenceNode {
			for _, item := range valNode.Content {
				if item.LineComment != "" {
					oldComments[item.Value] = item.LineComment
				}
			}
		}
	}

	// 更新 'main' 字段
	var mainKeyNode, mainValNode *yaml.Node
	for i := 0; i < len(accountsMapping.Content); i += 2 {
		if accountsMapping.Content[i].Value == "main" || accountsMapping.Content[i].Value == "primary" {
			mainKeyNode = accountsMapping.Content[i]
			mainValNode = accountsMapping.Content[i+1]
			mainKeyNode.Value = "main" // 确保是 main
			break
		}
	}

	if mainKeyNode == nil {
		mainKeyNode = &yaml.Node{Kind: yaml.ScalarNode, Value: "main"}
		mainValNode = &yaml.Node{Kind: yaml.ScalarNode}
		accountsMapping.Content = append(accountsMapping.Content, mainKeyNode, mainValNode)
	}

	mainValNode.Kind = yaml.ScalarNode
	mainValNode.Value = mainPath
	if mainNickname != "" {
		mainValNode.LineComment = "# 昵称: " + mainNickname
	} else if oldComm, ok := oldComments["main"]; ok {
		mainValNode.LineComment = oldComm
	} else {
		mainValNode.LineComment = ""
	}

	// 更新 'secondary' 字段
	var secKeyNode, secValNode *yaml.Node
	for i := 0; i < len(accountsMapping.Content); i += 2 {
		if accountsMapping.Content[i].Value == "secondary" {
			secKeyNode = accountsMapping.Content[i]
			secValNode = accountsMapping.Content[i+1]
			break
		}
	}

	if secKeyNode == nil {
		secKeyNode = &yaml.Node{Kind: yaml.ScalarNode, Value: "secondary"}
		secValNode = &yaml.Node{Kind: yaml.SequenceNode}
		accountsMapping.Content = append(accountsMapping.Content, secKeyNode, secValNode)
	}

	secValNode.Kind = yaml.SequenceNode
	secValNode.Content = nil
	for i, path := range secondaryPaths {
		itemNode := &yaml.Node{Kind: yaml.ScalarNode, Value: path}
		var comment string
		if i < len(secondaryNicknames) && secondaryNicknames[i] != "" {
			comment = "# 昵称: " + secondaryNicknames[i]
		} else if oldComm, ok := oldComments[path]; ok {
			comment = oldComm
		}

		if comment != "" {
			trimmed := strings.TrimSpace(comment)
			if !strings.HasPrefix(trimmed, "#") {
				itemNode.LineComment = "# " + trimmed
			} else {
				itemNode.LineComment = comment
			}
		}
		secValNode.Content = append(secValNode.Content, itemNode)
	}

	output, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, output, 0644)
}


