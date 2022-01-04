package engine

import (
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anatasluo/ant/backend/setting"
	log "github.com/sirupsen/logrus"
	"path/filepath"
	"sync"
)

type Engine struct {
	TorrentEngine     *torrent.Client
	TorrentDB         *TorrentDB
	WebInfo           *WebviewInfo
	EngineRunningInfo *EngineInfo
}

var (
	onlyEngine     Engine
	onlyEngineOnce sync.Once
	clientConfig   = setting.GetClientSetting()
	logger         = clientConfig.LoggerSetting.Logger
)

func GetEngine() *Engine {
	onlyEngineOnce.Do(func() {
		onlyEngine.initAndRunEngine()
	})
	return &onlyEngine
}

// this could be slow if there are a lot of torrents to be recovered
func (engine *Engine) initAndRunEngine() {
	engine.TorrentDB = GetTorrentDB(clientConfig.EngineSetting.TorrentDBPath)

	var tmpErr error
	engine.TorrentEngine, tmpErr = torrent.NewClient(&clientConfig.EngineSetting.TorrentConfig)
	if tmpErr != nil {
		logger.WithFields(log.Fields{"Error": tmpErr}).Error("Failed to Created torrent engine")
	}

	engine.WebInfo = &WebviewInfo{}
	engine.WebInfo.HashToTorrentWebInfo = make(map[metainfo.Hash]*TorrentWebInfo)

	engine.EngineRunningInfo = &EngineInfo{}
	engine.EngineRunningInfo.init()

	// recover from storm database
	engine.setEnvironment()
}

func (engine *Engine) setEnvironment() {
	engine.TorrentDB.GetLogs(&engine.EngineRunningInfo.TorrentLogsAndID)
	logger.Debug("Number of torrent(s) in db is ", len(engine.EngineRunningInfo.TorrentLogs))
	for i, singleLog := range engine.EngineRunningInfo.TorrentLogs {
		if singleLog.Status != CompletedStatus {
			// 把未完成的种子添加到下载队列中，初始状态为Stopped
			_, tmpErr := engine.TorrentEngine.AddTorrent(&singleLog.MetaInfo)
			if tmpErr != nil {
				logger.WithFields(log.Fields{"Error": tmpErr}).Info("Failed to add torrent to client")
			} else {
				logger.Infof("SetEnvironment: add torrent %v %v %v to client",
					singleLog.TorrentName,
					singleLog.MetaInfo.HashInfoBytes(),
					singleLog.StoragePath)
			}
			engine.EngineRunningInfo.TorrentLogs[i].Status = StoppedStatus
		}
	}
	if len(engine.EngineRunningInfo.TorrentLogs) > 0 {
		logger.Info("loaded all torrents from TorrentDB")
	}
	engine.UpdateInfo()
}

func (engine *Engine) Restart() {

	logger.Info("Restart engine")

	//To handle problems caused by change of settings
	for index := range engine.EngineRunningInfo.TorrentLogs {
		if engine.EngineRunningInfo.TorrentLogs[index].Status != CompletedStatus && engine.EngineRunningInfo.TorrentLogs[index].StoragePath != clientConfig.TorrentConfig.DataDir {
			filePath := filepath.Join(engine.EngineRunningInfo.TorrentLogs[index].StoragePath, engine.EngineRunningInfo.TorrentLogs[index].TorrentName)
			log.WithFields(log.Fields{"Path": filePath}).Info("To restart engine, these unfinished files will be deleted")
			singleTorrent, torrentExist := engine.GetOneTorrent(engine.EngineRunningInfo.TorrentLogs[index].HashInfoBytes().HexString())
			if torrentExist {
				singleTorrent.Drop()
			}
			engine.EngineRunningInfo.TorrentLogs[index].StoragePath = clientConfig.TorrentConfig.DataDir
			engine.UpdateInfo()
			delFiles(filePath)
		}
	}
	engine.Cleanup()
	GetEngine()

}

func (engine *Engine) SaveInfo() {
	tmpErr := engine.TorrentDB.DB.Save(&engine.EngineRunningInfo.TorrentLogsAndID)
	if tmpErr != nil {
		logger.WithFields(log.Fields{"Error": tmpErr}).Fatal("Failed to save torrent queues")
	}
}

func (engine *Engine) Cleanup() {
	engine.UpdateInfo()

	for index := range engine.EngineRunningInfo.TorrentLogs {
		if engine.EngineRunningInfo.TorrentLogs[index].Status != CompletedStatus {
			if engine.EngineRunningInfo.TorrentLogs[index].Status == AnalysingStatus {
				aimLog := engine.EngineRunningInfo.TorrentLogs[index]
				torrentHash := metainfo.Hash{}
				_ = torrentHash.FromHexString(aimLog.TorrentName)
				magnetTorrent, isExist := engine.TorrentEngine.Torrent(torrentHash)
				if isExist {
					logger.Info("One magnet will be deleted " + magnetTorrent.String())
					magnetTorrent.Drop()
				}
			} else if engine.EngineRunningInfo.TorrentLogs[index].Status == RunningStatus {
				engine.StopOneTorrent(engine.EngineRunningInfo.TorrentLogs[index].HashInfoBytes().HexString())
				engine.EngineRunningInfo.TorrentLogs[index].Status = StoppedStatus
			} else if engine.EngineRunningInfo.TorrentLogs[index].Status == QueuedStatus {
				engine.EngineRunningInfo.TorrentLogs[index].Status = StoppedStatus
			}
		}
	}

	//Update info in torrentLogs, remove magnet
	tmpLogs := engine.EngineRunningInfo.TorrentLogs
	engine.EngineRunningInfo.TorrentLogs = nil

	for index := range tmpLogs {
		if tmpLogs[index].Status != AnalysingStatus {
			engine.EngineRunningInfo.TorrentLogs = append(engine.EngineRunningInfo.TorrentLogs, tmpLogs[index])
		}
	}

	engine.SaveInfo()

	engine.TorrentEngine.Close()
	engine.TorrentDB.Cleanup()
}
