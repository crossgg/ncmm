# 🎵 ncmm (ncmctl)

`ncmm` 是一个基于 Go 语言（Go 1.25.0+）开发的网易云音乐命令行工具。编译后生成的命令行工具为 `ncmctl`，支持多种方式登录网易云音乐，并能对指定的歌曲 ID 列表进行模拟播放。

---

## 🚀 核心功能

1. **🔑 账号登录管理 (`ncmctl login`)**
   - **扫码登录 (`qrcode`)**：在终端命令行直接渲染并打印二维码字符，同时在本地生成二维码图片，用户使用手机网易云音乐 APP 扫码即可快捷登录。
   - **手机号登录 (`phone`)**：支持使用短信验证码（发送验证码并在终端交互输入）或账号密码进行登录。
   - **Cookie 导入 (`cookie`)**：支持从文本或文件导入 Cookie。具备对 JSON 数组格式、Netscape 文本格式以及传统 HTTP Header 格式 Cookie 的自动识别和解析能力。
   - **CookieCloud 同步 (`cookiecloud`)**：通过配置 CookieCloud 的服务器地址、UUID 和密码，自动拉取并过滤同步网易云音乐的 Cookie。

2. **🎵 模拟歌曲播放 (`ncmctl playids`)**
   - 针对指定的 `songId` 歌曲池（支持命令行参数直接传入，或从外部文本文件批量读取），通过接口请求与时延等待来模拟播放：
     - **获取播放链接**：调用 `SongPlayerV1` 接口获取歌曲的流媒体 URL。
     - **模拟音频拉取**：首次播放某首歌曲时，真实请求 CDN 地址拉取音频数据至内存以模拟完整流量交互；单次运行中相同的歌曲再次播放时直接读取内存记录，避免重复下载以节省网络带宽。
     - **真实时长等待**：前台展示进度条，程序会真实等待（`time.Sleep`）该首歌曲对应的完整时间。
     - **播放动作上报**：歌曲播放结束后，调用网易云音乐的 `WebLog` 接口，发送播放行为日志。
     - **随机播放间隔**：每首歌曲播放完毕后，在配置的 `gap-min` 到 `gap-max` 秒范围内随机休眠，再开始下一首。

3. **📊 每日播放目标控制与本地进度存储**
   - 播放状态和进度记录在本地嵌入式 Badger 数据库中。
   - **随机每日上限**：每天首次启动时，系统在配置文件设定的 `[daily_min, daily_max]` 区间内随机生成一个今日播放目标并存入数据库。当天的多次运行将复用这一目标。
   - **限额自增与达标退出**：每播放成功一首，本地已播计数自增；当今日播放数量达到随机目标上限时，程序将自动跳过并退出，防止每日播放次数恒定。
   - **单次运行目标**：支持通过 `[run_min, run_max]` 参数设定单次运行的播放歌曲数。

---

## ⚙️ 配置文件说明 (`config.yaml`)

默认配置文件路径为 `~/.ncmctl/config.yaml`（支持在运行时通过 `-c` 或 `--config` 指定）。配置字段如下：

```yaml
# 配置文件版本
version: 1.0

# log 日志模块配置
log:
  # 应用名称
  app: ncm
  # 日志输出格式: text / json
  format: text
  # 日志级别: debug < info < warn < error
  level: info
  # 日志是否输出到标准输出 (控制台)
  stdout: false
  # 滚动日志配置
  rotate:
    # 日志文件保存路径
    filename: "${HOME}/.ncmctl/log/ncm.log"
    # 单个日志文件最大大小 (单位: MB)
    maxsize: 100
    # 日志文件保留天数
    maxage: 7
    # 日志文件保留最大数量
    maxbackups: 3
    # 日志打印是否使用本地时间
    localtime: true
    # 日志文件是否启用 gzip 压缩
    compress: true

# 网络模块配置
network:
  # 是否开启 resty 调试日志输出
  debug: false
  # 全局请求超时时间
  timeout: 60s
  # 网络请求失败重试次数
  retry: 3
  # cookie 配置用于保存登录相关信息
  cookie:
    # cookie 存储文件路径，登录成功后会将 cookie 序列化至该文件
    filepath: "${HOME}/.ncmctl/cookie.json"
    # cookie 刷盘检测间隔
    interval: 3s
  # 全局自定义 User-Agent
  user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"

# 本地数据存储 (主要记录播放状态进度)
database:
  # 缓存驱动类型，目前仅支持 badger
  driver: badger
  # 缓存目录路径
  path: "${HOME}/.ncmctl/database/badger/"

# playids 播放指定歌曲配置
playids:
  # 每日播放目标随机下限 (每天首次启动时在此范围内随机生成当日目标)
  daily_min: 50
  # 每日播放目标随机上限
  daily_max: 200
  # 单次运行目标下限 (为 0 表示单次不设限，直到跑完今日剩余播放目标)
  run_min: 0
  # 单次运行目标上限
  run_max: 0
  # 歌曲与歌曲之间的静默间隔最小值 (单位: 秒)
  gap_min: 10
  # 歌曲与歌曲之间的静默间隔最大值 (单位: 秒)
  gap_max: 30
```

---

## 🛠️ 编译与安装

在项目根目录下执行以下命令进行编译：
```bash
go build -o ncmctl.exe main.go
```
*提示：在 Linux / macOS 等类 Unix 平台下，请编译为 `ncmctl`：`go build -o ncmctl main.go`。*

---

## 📖 命令行使用说明

### 0. 全局通用参数

`ncmctl` 命令支持以下全局通用参数，可以跟在主命令或任意子命令后：

- `-c` / `--config` (String)：指定配置文件 `config.yaml` 的路径。若不指定，默认加载 `${HOME}/.ncmctl/config.yaml`；若该文件不存在，则会使用程序内置的默认配置值。
- `--home` (String)：指定数据存储的家目录。默认值为当前用户的系统家目录（`${HOME}`）。该家目录将用于存放运行日志（`log/`）、Cookie 状态文件（`cookie.json`）以及本地 Badger 数据库（`database/`）。
- `--debug` (Boolean)：开启命令行调试模式。开启后，日志会强制输出到标准输出（Stdout），日志级别临时设为 `debug`，并输出底层的网络请求调试信息。

---

### 1. 账号登录 (`ncmctl login`)

进行歌曲播放前，必须执行登录操作以保存登录凭证（Cookie 数据默认存放在家目录下的 `cookie.json` 中）。

#### 二维码扫码登录 (`qrcode`)
```bash
ncmctl login qrcode [-t 超时时间] [-d 图片输出目录] [-l 二维码纠错等级]
# 示例：5分钟超时，二维码图片保存在当前目录
ncmctl login qrcode -t 5m -d ./
```
- `-t` / `--timeout` (Duration): 扫码等待和状态轮询的超时时长，默认 `5m` (5分钟)，可填 `10m`、`30s` 等。
- `-d` / `--dir` (String): 保存临时二维码图片 `qrcode.png` 的目录路径，默认当前工作目录。登录成功或超时后该文件会自动清理。
- `-l` / `--level` (Int): 二维码纠错等级（`0` -> 7%, `1` -> 15% 默认, `2` -> 25%, `3` -> 30%）。

#### 手机号登录 (`phone`)
```bash
# 短信验证码登录 (不指定密码时会自动发送验证码，并在控制台交互提示输入验证码)
ncmctl login phone 188xxxx8888

# 密码登录
ncmctl login phone 188xxxx8888 -p "YourPassword"
```
- `-p` / `--password` (String): 登录账号密码。不提供该参数时，系统将使用验证码登录。
- `--countrycode` (Int): 国家及地区代码，默认 `86` (中国大陆)。
- `-t` / `--timeout` (Duration): 登录操作的超时时间，默认 `10m` (10分钟)。

#### Cookie 直接导入 (`cookie`)
支持自动识别 Header 字符串、JSON 数组、Netscape 文件等多种 Cookie 格式：
```bash
# 导入 Cookie 字符串
ncmctl login cookie 'MUSIC_U=xxxx; __csrf=yyyy;'

# 从外部 Cookie 文本文件导入
ncmctl login cookie -f ./cookie.txt
```
- `-f` / `--file` (String): 存放 Cookie 的文件路径。如果指定此参数，将从该文件中读取 Cookie。
- `--format` (String): 手动指定导入 Cookie 文件的格式（支持 `json`、`netscape` 或 `header`），不指定则程序会自动探测并尝试三种格式。

#### CookieCloud 同步 (`cookiecloud`)
```bash
ncmctl login cookiecloud -u <UUID> -p <密码> -s <服务器地址>
```
- `-u` / `--uuid` (String): CookieCloud 账户的 UUID，必填。
- `-p` / `--password` (String): CookieCloud 账户的密码，必填。
- `-s` / `--server` (String): CookieCloud 服务器端点地址，默认 `http://127.0.0.1:8088`。
- `-t` / `--timeout` (Duration): 请求 CookieCloud 服务器的超时时长，默认 `30s`。
- `-H` / `--headers` (String): 请求 CookieCloud 时的自定义 HTTP 头部信息，格式为 `key1=value1,key2=value2`。

---

### 2. 模拟播放 (`ncmctl playids`)

```bash
ncmctl playids --ids <songId列表> [--ids-file <文件>] [--num <播放数量>] [--gap-min <秒>] [--gap-max <秒>] [--daily-min <下限>] [--daily-max <上限>]
```

#### 📌 参数说明

| 参数/Flag | 说明 | 默认值 |
| :--- | :--- | :--- |
| `--ids` | 逗号分隔的歌曲 ID 列表 | 空 |
| `--ids-file` | 从文本文件读取歌曲 ID 列表（每行一个 ID，支持 `#` 注释） | 空 |
| `--num` | 本次运行最大播放的歌曲数量（`0` 表示播到今日目标上限为止） | `0` |
| `--gap-min` | 歌曲切换之间的最小随机等待间隔（秒） | 配置项 `gap_min` / `10` |
| `--gap-max` | 歌曲切换之间的最大随机等待间隔（秒） | 配置项 `gap_max` / `30` |
| `--daily-min` | 当天首次启动时，随机目标的最小歌曲数限制 | 配置项 `daily_min` / `50` |
| `--daily-max` | 当天首次启动时，随机目标的最大歌曲数限制 | 配置项 `daily_max` / `200` |
| `--run-min` | 单次运行的最小歌曲数量设定 | 配置项 `run_min` / `0` |
| `--run-max` | 单次运行的最大歌曲数量设定 | 配置项 `run_max` / `0` |

#### 💡 使用示例
```bash
# 播放指定的多个歌曲 ID
ncmctl playids --ids 3373818852,3373845775

# 从歌曲 ID 列表文件读取并播放
ncmctl playids --ids-file ./songs.txt

# 调试播放：只播放 1 首，且无等待间隔
ncmctl playids --ids 3373818852 --num 1 --gap-min 0 --gap-max 0
```

`songs.txt` 文件示例：
```text
# 这是一行注释，系统会自动跳过
3373818852
3373845775
```

---

## ⚠️ 免责声明

本项目仅供学术研究和 Golang 学习探讨之用，请勿用于任何商业用途或违反网易云音乐服务条款的行为。对于使用本项目带来的任何账号封禁、数据丢失等不良后果，由使用者自行承担，本项目不提供任何连带保证。
