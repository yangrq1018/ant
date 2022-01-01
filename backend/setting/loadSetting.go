package setting

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/pelletier/go-toml"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/time/rate"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

var (
	clientConfig      ClientSetting
	haveCreatedConfig = false
	globalViper       = viper.New()
)

type ConnectSetting struct {
	SupportRemote bool
	IP            string
	Port          int
	Addr          string
	AuthUsername  string
	AuthPassword  string
}

type EngineSetting struct {
	UseSocksproxy         bool
	SocksProxyURL         string
	MaxActiveTorrents     int
	TorrentDBPath         string
	TorrentConfig         torrent.ClientConfig `json:"-"`
	Tmpdir                string
	MaxEstablishedConns   int
	EnableDefaultTrackers bool
	DefaultTrackers       [][]string
}

type LoggerSetting struct {
	LoggingLevel  log.Level
	LoggingOutput string
	Logger        *log.Logger
}

type ClientSetting struct {
	ConnectSetting
	EngineSetting
	LoggerSetting
}

// These settings can be determined by users
type WebSetting struct {
	UseSocksproxy         bool
	SocksProxyURL         string
	MaxEstablishedConns   int
	Tmpdir                string
	DataDir               string
	EnableDefaultTrackers bool
	DefaultTrackerList    string
	DisableIPv4           bool
	DisableIPv6           bool
}

func (cc *ClientSetting) GetWebSetting() (webSetting WebSetting) {
	webSetting.EnableDefaultTrackers = cc.EngineSetting.EnableDefaultTrackers
	webSetting.DefaultTrackerList = globalViper.GetString("EngineSetting.DefaultTrackerList")
	webSetting.UseSocksproxy = cc.EngineSetting.UseSocksproxy
	webSetting.SocksProxyURL = cc.EngineSetting.SocksProxyURL
	webSetting.MaxEstablishedConns = cc.MaxEstablishedConns
	webSetting.Tmpdir = cc.Tmpdir
	webSetting.DataDir = cc.TorrentConfig.DataDir
	webSetting.DisableIPv4 = cc.TorrentConfig.DisableIPv4
	webSetting.DisableIPv6 = cc.TorrentConfig.DisableIPv6
	return
}

// TODO
func calculateRateLimiters(uploadRate, downloadRate string) (*rate.Limiter, *rate.Limiter) {
	downloadRateLimiter := rate.NewLimiter(rate.Inf, 0)
	uploadRateLimiter := rate.NewLimiter(rate.Inf, 0)
	return uploadRateLimiter, downloadRateLimiter
}

func (cc *ClientSetting) loadValueFromConfig() {

	cc.LoggerSetting.LoggingLevel = log.AllLevels[globalViper.GetInt("LoggerSetting.LoggingLevel")]
	cc.LoggerSetting.LoggingOutput = globalViper.GetString("LoggerSetting.LoggingOutput")
	cc.LoggerSetting.Logger.SetLevel(cc.LoggerSetting.LoggingLevel)

	cc.EngineSetting.UseSocksproxy = globalViper.GetBool("EngineSetting.UseSocksproxy")
	cc.EngineSetting.SocksProxyURL = globalViper.GetString("EngineSetting.SocksProxyURL")
	cc.EngineSetting.MaxActiveTorrents = globalViper.GetInt("EngineSetting.MaxActiveTorrents")
	cc.EngineSetting.TorrentDBPath = globalViper.GetString("EngineSetting.TorrentDBPath")
	cc.EngineSetting.MaxEstablishedConns = globalViper.GetInt("EngineSetting.MaxEstablishedConns")
	tmpDir, tmpErr := filepath.Abs(filepath.ToSlash(globalViper.GetString("EngineSetting.Tmpdir")))
	_ = os.Mkdir(tmpDir, 0755)
	cc.EngineSetting.Tmpdir = tmpDir
	if tmpErr != nil {
		cc.Logger.WithFields(log.Fields{"Error": tmpErr}).Error("Fail to create default cache directory")
	}

	cc.ConnectSetting.IP = globalViper.GetString("ConnectSetting.IP")
	cc.ConnectSetting.Port = globalViper.GetInt("ConnectSetting.Port")
	cc.ConnectSetting.SupportRemote = globalViper.GetBool("ConnectSetting.SupportRemote")
	if cc.ConnectSetting.SupportRemote {
		cc.ConnectSetting.Addr = ":" + strconv.Itoa(cc.ConnectSetting.Port)
	} else {
		cc.ConnectSetting.Addr = cc.ConnectSetting.IP + ":" + strconv.Itoa(cc.ConnectSetting.Port)
	}
	cc.ConnectSetting.AuthUsername = globalViper.GetString("ConnectSetting.AuthUsername")
	cc.ConnectSetting.AuthPassword = globalViper.GetString("ConnectSetting.AuthPassword")

	cc.EngineSetting.TorrentConfig = *torrent.NewDefaultClientConfig()
	cc.EngineSetting.TorrentConfig.UploadRateLimiter, cc.EngineSetting.TorrentConfig.DownloadRateLimiter = calculateRateLimiters(viper.GetString("TorrentConfig.UploadRateLimit"), viper.GetString("TorrentConfig.DownloadRateLimit"))
	tmpDataDir, err := filepath.Abs(filepath.ToSlash(globalViper.GetString("EngineSetting.DataDir")))
	_ = os.Mkdir(tmpDataDir, 0755)
	cc.EngineSetting.TorrentConfig.DataDir = tmpDataDir
	if err != nil {
		cc.Logger.WithFields(log.Fields{"Error": err}).Error("Fail to create default datadir")
	}
	tmpListenAddr := globalViper.GetString("TorrentConfig.ListenAddr")
	if tmpListenAddr != "" {
		cc.EngineSetting.TorrentConfig.SetListenAddr(tmpListenAddr)
	}
	cc.EngineSetting.TorrentConfig.ListenPort = globalViper.GetInt("TorrentConfig.ListenPort")
	cc.EngineSetting.TorrentConfig.DisablePEX = globalViper.GetBool("TorrentConfig.DisablePEX")
	cc.EngineSetting.TorrentConfig.NoDHT = globalViper.GetBool("TorrentConfig.NoDHT")
	cc.EngineSetting.TorrentConfig.NoUpload = globalViper.GetBool("TorrentConfig.NoUpload")
	cc.EngineSetting.TorrentConfig.Seed = globalViper.GetBool("TorrentConfig.Seed")
	cc.EngineSetting.TorrentConfig.DisableUTP = globalViper.GetBool("TorrentConfig.DisableUTP")
	cc.EngineSetting.TorrentConfig.DisableTCP = globalViper.GetBool("TorrentConfig.DisableTCP")
	cc.EngineSetting.TorrentConfig.DisableIPv6 = globalViper.GetBool("EngineSetting.DisableIPv6")
	cc.EngineSetting.TorrentConfig.DisableIPv4 = globalViper.GetBool("EngineSetting.DisableIPv4")
	cc.EngineSetting.TorrentConfig.Debug = globalViper.GetBool("TorrentConfig.Debug")
	cc.EngineSetting.TorrentConfig.PeerID = globalViper.GetString("TorrentConfig.PeerID")

	cc.EngineSetting.TorrentConfig.HeaderObfuscationPolicy.Preferred = !globalViper.GetBool("EncryptionPolicy.PreferNoEncryption")

	blockListPath, err := filepath.Abs("biglist.p2p.gz")
	if err != nil {
		fmt.Printf("Failed to update block list is: %v\n", err)
	} else {
		cc.EngineSetting.TorrentConfig.IPBlocklist = cc.getBlocklist(blockListPath, globalViper.GetString("EngineSetting.DefaultIPBlockList"))
	}

	cc.EngineSetting.EnableDefaultTrackers = globalViper.GetBool("EngineSetting.EnableDefaultTrackers")
	if cc.EngineSetting.EnableDefaultTrackers {
		trackerPath, err := filepath.Abs("tracker.txt")
		if err != nil {
			cc.Logger.WithFields(log.Fields{"Error": err}).Error("Failed to update trackers list")
		}
		cc.EngineSetting.DefaultTrackers = cc.getDefaultTrackers(trackerPath, globalViper.GetString("EngineSetting.DefaultTrackerList"))
	} else {
		cc.EngineSetting.DefaultTrackers = [][]string{}
	}

	if cc.UseSocksproxy {
		cc.TorrentConfig.HTTPProxy = func(request *http.Request) (*url.URL, error) {
			return url.Parse(cc.SocksProxyURL)
		}
	}

	if cc.LoggerSetting.LoggingOutput == "file" {
		file, err := os.OpenFile("./ant_engine.log", os.O_CREATE|os.O_WRONLY, 0755)
		if err != nil {
			cc.Logger.WithFields(log.Fields{"Error": err}).Error("Failed to open log file")
		} else {
			cc.Logger.Out = file
		}
	} else {
		cc.Logger.Out = os.Stdout
	}
}

func (cc *ClientSetting) loadFromConfigFile() {
	globalViper.SetConfigName("config")
	globalViper.AddConfigPath("./")
	err := globalViper.ReadInConfig()
	if err != nil {
		cc.LoggerSetting.Logger.WithFields(log.Fields{"Detail": err}).Fatal("Can not find config.toml")
	} else {
		cc.loadValueFromConfig()
	}
	globalViper.WatchConfig()
	//globalViper.OnConfigChange(func(e fsnotify.Event) {
	//	fmt.Println("Config file changed:", e.Name)
	//	//cc.loadValueFromConfig()
	//})
}

// Load setting from config.toml
func (cc *ClientSetting) createClientSetting() {

	//Default settings
	cc.LoggerSetting.Logger = log.New()

	cc.loadFromConfigFile()
}

func GetClientSetting() *ClientSetting {
	if haveCreatedConfig == false {
		haveCreatedConfig = true
		clientConfig.createClientSetting()
	}
	return &clientConfig
}

func (cc *ClientSetting) UpdateConfig(newSetting WebSetting) {
	globalViper.Set("EngineSetting.EnableDefaultTrackers", newSetting.EnableDefaultTrackers)
	globalViper.Set("EngineSetting.DefaultTrackerList", newSetting.DefaultTrackerList)
	globalViper.Set("EngineSetting.UseSocksproxy", newSetting.UseSocksproxy)
	globalViper.Set("EngineSetting.SocksProxyURL", newSetting.SocksProxyURL)
	globalViper.Set("EngineSetting.MaxEstablishedConns", newSetting.MaxEstablishedConns)
	globalViper.Set("EngineSetting.Tmpdir", newSetting.Tmpdir)
	globalViper.Set("EngineSetting.DataDir", newSetting.DataDir)
	globalViper.Set("EngineSetting.DisableIPv4", newSetting.DisableIPv4)
	globalViper.Set("EngineSetting.DisableIPv6", newSetting.DisableIPv6)

	tr, err := toml.TreeFromMap(globalViper.AllSettings())
	trS := tr.String()
	err = ioutil.WriteFile("config.toml", []byte(trS), 0644)
	if err != nil {
		cc.Logger.WithFields(log.Fields{"Error": err, "Settings": trS}).Fatal("Unable to update settings")
	}
	haveCreatedConfig = false
	GetClientSetting()
}

func (cc *ClientSetting) getDefaultTrackers(filepath string, url string) [][]string {
	datas, err := readLines(filepath)
	if err != nil {
		panic(err)
	}
	var res [][]string

	for i := range datas {
		if datas[i] != "" {
			res = append(res, []string{
				datas[i],
			})
		}
	}

	//Update list if possible for next time
	go cc.downloadFile(url, filepath)
	return res
}

// Download and add the blocklist.
func (cc *ClientSetting) getBlocklist(filepath string, blocklistURL string) iplist.Ranger {

	// Load blocklist.
	// #nosec
	// We trust our temporary directory as we just wrote the file there ourselves.
	blocklistReader, err := os.Open(filepath)
	if err != nil {
		cc.Logger.WithFields(log.Fields{"Error": err}).Error("Error opening blocklist")
		return nil
	}

	// Extract file.
	gzipReader, err := gzip.NewReader(blocklistReader)
	if err != nil {
		cc.Logger.WithFields(log.Fields{"Error": err}).Error("Error extracting blocklist")
		return nil
	}

	// Read as iplist.
	blocklist, err := iplist.NewFromReader(gzipReader)
	if err != nil {
		cc.Logger.WithFields(log.Fields{"Error": err}).Error("Error reading blocklist")
		return nil
	}
	cc.Logger.Debug("Loading blocklist")

	//Update list if possible for next time
	//go cc.downloadFile(blocklistURL, filepath)
	return blocklist
}

// Read a whole file into the memory and store it as array of lines
func readLines(path string) (lines []string, err error) {
	var (
		file   *os.File
		part   []byte
		prefix bool
	)
	if file, err = os.Open(path); err != nil {
		return
	}
	defer func() {
		_ = file.Close()
	}()

	reader := bufio.NewReader(file)
	buffer := bytes.NewBuffer(make([]byte, 0))
	for {
		if part, prefix, err = reader.ReadLine(); err != nil {
			break
		}
		buffer.Write(part)
		if !prefix {
			lines = append(lines, buffer.String())
			buffer.Reset()
		}
	}
	if err == io.EOF {
		err = nil
	}
	return
}

func (cc *ClientSetting) downloadFile(downloadURL string, filepath string) {
	// Get the data
	resp, err := http.Get(downloadURL)
	if err != nil {
		cc.Logger.WithFields(log.Fields{"Error": err}).Error("Failed to update list")
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	// Create the file
	out, err := os.Create(filepath)
	defer func() {
		_ = out.Close()
	}()
	if err != nil {
		if err != nil {
			cc.Logger.WithFields(log.Fields{"Error": err}).Error("Failed to create list")
			return
		}
	}

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		if err != nil {
			cc.Logger.WithFields(log.Fields{"Error": err}).Error("Failed to trackers list")
			return
		}
	}
	cc.Logger.Info("update file successfully")
}
