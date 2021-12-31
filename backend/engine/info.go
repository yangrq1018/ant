package engine

import (
	"fmt"
	"github.com/anacrolix/missinggo/pubsub"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/dustin/go-humanize"
	"math"
	"path/filepath"
	"time"
)

type WebviewInfo struct {
	HashToTorrentWebInfo map[metainfo.Hash]*TorrentWebInfo
}

type EngineInfo struct {
	TorrentLogsAndID
	MagnetNum         int
	EngineCMD         chan MessageTypeID
	HasRestarted      bool
	HashToTorrentLog  map[metainfo.Hash]*TorrentLog
	TorrentLogExtends map[metainfo.Hash]*TorrentLogExtend
}

//These information is needed in running time
type TorrentLogExtend struct {
	StatusPub         *pubsub.Subscription
	HasStatusPub      bool
	MagnetAnalyseChan chan bool
	MagnetDelChan     chan bool
	HasMagnetChan     bool
}

//WebInfo only can be used for show in the website, it is generated from engineInfo
type TorrentWebInfo struct {
	TorrentName   string
	TotalLength   string
	HexString     string
	Status        string
	StoragePath   string
	Percentage    float64
	DownloadSpeed string
	LeftTime      string
	Files         []FileInfo
	TorrentStatus torrent.TorrentStats
	UpdateTime    time.Time
}

type MessageTypeID int
type MessageFromWeb struct {
	MessageType MessageTypeID
	HexString   string
}

const (
	updateDuration = time.Second
)

const (
	GetInfo MessageTypeID = iota
	RefreshInfo
)

type FileInfo struct {
	Path     string
	Priority byte
	Size     string
}

type CMDInfo struct {
	MessageType MessageTypeID
}

type TorrentProgressInfo struct {
	CMDInfo
	Percentage    float64
	DownloadSpeed string
	LeftTime      string
	HexString     string
}

// TorrentLog will be saved to storm db, so types of its support is limited
type TorrentLog struct {
	metainfo.MetaInfo
	TorrentName string
	Status      TorrentStatus
	StoragePath string
}

type TorrentLogsAndID struct {
	ID          OnlyStormID `storm:"id"`
	TorrentLogs []TorrentLog
}

type TorrentStatus int

//StormID cant not be zero
const (
	QueuedStatus TorrentStatus = iota + 1
	// AnalysingStatus status only used for magnet
	AnalysingStatus
	RunningStatus
	StoppedStatus
	CompletedStatus
)

var StatusIDToName = []string{
	"",
	"Queued",
	"Analysing",
	"Running",
	"Stopped",
	"Completed",
}

type OnlyStormID int

const (
	TorrentLogsID OnlyStormID = iota + 1
)

func (engineInfo *EngineInfo) init() {
	engineInfo.MagnetNum = 0
	engineInfo.HasRestarted = false
	engineInfo.EngineCMD = make(chan MessageTypeID, 100)
	engineInfo.ID = TorrentLogsID
	engineInfo.HashToTorrentLog = make(map[metainfo.Hash]*TorrentLog)
	engineInfo.TorrentLogExtends = make(map[metainfo.Hash]*TorrentLogExtend)
}

func (engineInfo *EngineInfo) AddOneTorrent(singleTorrent *torrent.Torrent) (singleTorrentLog *TorrentLog) {
	singleTorrentLog, isExist := engineInfo.HashToTorrentLog[singleTorrent.InfoHash()]
	if !isExist {
		singleTorrentLog = createTorrentLogFromTorrent(singleTorrent)
		engineInfo.TorrentLogs = append(engineInfo.TorrentLogs, *singleTorrentLog)
		engineInfo.UpdateTorrentLog()
	}
	return
}

//For magnet
func (engineInfo *EngineInfo) AddOneTorrentFromMagnet(infoHash metainfo.Hash) (singleTorrentLog *TorrentLog) {
	singleTorrentLog, isExist := engineInfo.HashToTorrentLog[infoHash]
	if !isExist {
		singleTorrentLog = createTorrentLogFromMagnet(infoHash)
		engineInfo.TorrentLogs = append(engineInfo.TorrentLogs, *singleTorrentLog)
		engineInfo.UpdateTorrentLog()
		//create extend log
		_, extendIsExist := engineInfo.TorrentLogExtends[infoHash]
		if !extendIsExist {
			logger.Debug("create extend for magnet", infoHash)
			engineInfo.TorrentLogExtends[infoHash] = &TorrentLogExtend{
				HasStatusPub:      false,
				HasMagnetChan:     true,
				MagnetAnalyseChan: make(chan bool, 100),
				MagnetDelChan:     make(chan bool, 100),
			}
		} else if extendIsExist && !engineInfo.TorrentLogExtends[infoHash].HasMagnetChan {
			engineInfo.TorrentLogExtends[infoHash].HasMagnetChan = true
			engineInfo.TorrentLogExtends[infoHash].MagnetAnalyseChan = make(chan bool, 100)
			engineInfo.TorrentLogExtends[infoHash].MagnetDelChan = make(chan bool, 100)
		}
	}
	return
}

// After get magnet info, update log information
func (engineInfo *EngineInfo) UpdateMagnetInfo(singleTorrent *torrent.Torrent) {
	singleTorrentLog, _ := engineInfo.HashToTorrentLog[singleTorrent.InfoHash()]
	singleTorrentLog.TorrentName = singleTorrent.Name()
	singleTorrentLog.MetaInfo = singleTorrent.Metainfo()
	singleTorrentLog.Status = QueuedStatus
	engineInfo.UpdateTorrentLog()

	singleTorrentLogExtend, _ := engineInfo.TorrentLogExtends[singleTorrent.InfoHash()]
	singleTorrentLogExtend.HasMagnetChan = false
	close(singleTorrentLogExtend.MagnetAnalyseChan)
	close(singleTorrentLogExtend.MagnetDelChan)
	return
}

func (engine *Engine) UpdateInfo() {
	engine.EngineRunningInfo.UpdateTorrentLog()
	engine.UpdateWebInfo()
}

func (engineInfo *EngineInfo) UpdateTorrentLog() {
	engineInfo.HashToTorrentLog = make(map[metainfo.Hash]*TorrentLog)

	for index, singleTorrentLog := range engineInfo.TorrentLogs {
		if singleTorrentLog.Status != AnalysingStatus {
			engineInfo.HashToTorrentLog[singleTorrentLog.HashInfoBytes()] = &engineInfo.TorrentLogs[index]
		} else {
			torrentHash := metainfo.Hash{}
			_ = torrentHash.FromHexString(singleTorrentLog.TorrentName)
			engineInfo.HashToTorrentLog[torrentHash] = &engineInfo.TorrentLogs[index]
		}
	}
}

func (engine *Engine) UpdateWebInfo() {
	engine.WebInfo.HashToTorrentWebInfo = make(map[metainfo.Hash]*TorrentWebInfo)
	for _, singleTorrent := range engine.TorrentEngine.Torrents() {
		engine.WebInfo.HashToTorrentWebInfo[singleTorrent.InfoHash()] = engine.GenerateInfoFromTorrent(singleTorrent)
	}
}

func createTorrentLogFromTorrent(singleTorrent *torrent.Torrent) *TorrentLog {
	absPath, err := filepath.Abs(clientConfig.EngineSetting.TorrentConfig.DataDir)
	if err != nil {
		logger.Error("Unable to get abs path -> ", err)
	}
	return &TorrentLog{
		MetaInfo:    singleTorrent.Metainfo(),
		TorrentName: singleTorrent.Name(),
		Status:      QueuedStatus,
		StoragePath: absPath,
	}
}

func createTorrentLogFromMagnet(infoHash metainfo.Hash) *TorrentLog {
	absPath, err := filepath.Abs(clientConfig.EngineSetting.TorrentConfig.DataDir)
	if err != nil {
		logger.Error("Unable to get abs path -> ", err)
	}
	return &TorrentLog{
		MetaInfo:    metainfo.MetaInfo{},
		TorrentName: infoHash.String(),
		Status:      AnalysingStatus,
		StoragePath: absPath,
	}
}

func generateByteSize(byteSize int64) string {
	return humanize.Bytes(uint64(byteSize))
}

//For complete status
func (engine *Engine) GenerateInfoFromLog(torrentLog TorrentLog) (torrentWebInfo *TorrentWebInfo) {
	torrentWebInfo, _ = engine.WebInfo.HashToTorrentWebInfo[torrentLog.HashInfoBytes()]
	torrentWebInfo = &TorrentWebInfo{
		TorrentName: torrentLog.TorrentName,
		HexString:   torrentLog.HashInfoBytes().HexString(),
		Status:      StatusIDToName[torrentLog.Status],
		StoragePath: torrentLog.StoragePath,
		Percentage:  1,
	}
	engine.WebInfo.HashToTorrentWebInfo[torrentLog.HashInfoBytes()] = torrentWebInfo
	return
}

func (engine *Engine) GenerateInfoFromTorrent(singleTorrent *torrent.Torrent) (torrentWebInfo *TorrentWebInfo) {
	torrentWebInfo, isExist := engine.WebInfo.HashToTorrentWebInfo[singleTorrent.InfoHash()]
	if !isExist || torrentWebInfo.Status == StatusIDToName[AnalysingStatus] {
		torrentLog, _ := engine.EngineRunningInfo.HashToTorrentLog[singleTorrent.InfoHash()]
		if torrentLog.Status != AnalysingStatus {
			<-singleTorrent.GotInfo()
			torrentWebInfo = &TorrentWebInfo{
				TorrentName:   singleTorrent.Info().Name,
				TotalLength:   generateByteSize(singleTorrent.Info().TotalLength()),
				HexString:     torrentLog.HashInfoBytes().HexString(),
				Status:        StatusIDToName[torrentLog.Status],
				StoragePath:   torrentLog.StoragePath,
				Percentage:    float64(singleTorrent.BytesCompleted()) / float64(singleTorrent.Info().TotalLength()),
				DownloadSpeed: "Estimating",
				LeftTime:      "Estimating",
				TorrentStatus: singleTorrent.Stats(),
				UpdateTime:    time.Now(),
			}
			for _, key := range singleTorrent.Files() {
				torrentWebInfo.Files = append(torrentWebInfo.Files, FileInfo{
					Path:     key.Path(),
					Priority: byte(key.Priority()),
					Size:     generateByteSize(key.Length()),
				})
			}
		} else {
			//for magnet
			torrentWebInfo = &TorrentWebInfo{
				TorrentName:   torrentLog.TorrentName,
				HexString:     torrentLog.TorrentName,
				Status:        StatusIDToName[torrentLog.Status],
				StoragePath:   torrentLog.StoragePath,
				Percentage:    0,
				DownloadSpeed: "Estimating",
				LeftTime:      "Estimating",
				TorrentStatus: singleTorrent.Stats(),
				UpdateTime:    time.Now(),
			}
		}
		engine.WebInfo.HashToTorrentWebInfo[singleTorrent.InfoHash()] = torrentWebInfo
	} else {
		torrentLog, _ := engine.EngineRunningInfo.HashToTorrentLog[singleTorrent.InfoHash()]
		torrentWebInfo.TorrentStatus = singleTorrent.Stats()
		torrentWebInfo.Status = StatusIDToName[torrentLog.Status]

		timeNow := time.Now()
		timeDis := timeNow.Sub(torrentWebInfo.UpdateTime).Seconds()
		percentageNow := float64(singleTorrent.BytesCompleted()) / float64(singleTorrent.Info().TotalLength())
		percentageDis := percentageNow - torrentWebInfo.Percentage
		if timeDis >= updateDuration.Seconds() && percentageDis > 0 {
			leftDuration := time.Duration((1 - torrentWebInfo.Percentage) / (percentageDis / timeDis) * 1000 * 1000 * 1000)
			torrentWebInfo.LeftTime = humanizeDuration(leftDuration)
			torrentWebInfo.DownloadSpeed = generateByteSize(int64(percentageDis*float64(singleTorrent.Info().TotalLength())/timeDis)) + "/s"
			torrentWebInfo.Percentage = percentageNow
			torrentWebInfo.UpdateTime = time.Now()
			if torrentWebInfo.Percentage == 1 {
				engine.CompleteOneTorrent(singleTorrent)
			}
		}
	}
	return
}

func humanizeDuration(duration time.Duration) string {
	if duration.Seconds() < 60.0 {
		return fmt.Sprintf("%d seconds", int64(duration.Seconds()))
	}
	if duration.Minutes() < 60.0 {
		remainingSeconds := math.Mod(duration.Seconds(), 60)
		return fmt.Sprintf("%d minutes %d seconds", int64(duration.Minutes()), int64(remainingSeconds))
	}
	if duration.Hours() < 24.0 {
		remainingMinutes := math.Mod(duration.Minutes(), 60)
		remainingSeconds := math.Mod(duration.Seconds(), 60)
		return fmt.Sprintf("%d hours %d minutes %d seconds",
			int64(duration.Hours()), int64(remainingMinutes), int64(remainingSeconds))
	}
	remainingHours := math.Mod(duration.Hours(), 24)
	remainingMinutes := math.Mod(duration.Minutes(), 60)
	remainingSeconds := math.Mod(duration.Seconds(), 60)
	return fmt.Sprintf("%d days %d hours %d minutes %d seconds",
		int64(duration.Hours()/24), int64(remainingHours),
		int64(remainingMinutes), int64(remainingSeconds))
}
