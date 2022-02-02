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
	EngineRunningInfo *RunningInfo
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

	engine.EngineRunningInfo = &RunningInfo{}
	engine.EngineRunningInfo.init()

	// recover from storm database
	engine.setEnvironment()
}

func (engine *Engine) setEnvironment() {
	engine.TorrentDB.GetLogs(&engine.EngineRunningInfo.TorrentLogsAndID)
	engine.EngineRunningInfo.UpdateTorrentLog()
	logger.Infof("Number of torrent(s) in db: %d", len(engine.EngineRunningInfo.TorrentLogs))
	var wg sync.WaitGroup
	for i, singleLog := range engine.EngineRunningInfo.TorrentLogs {
		switch singleLog.Status {
		case CompletedStatus:
		default:
			// 把未完成的种子添加到下载队列中，初始状态为Stopped
			wg.Add(1)
			go func(i int, singleLog TorrentLog) {
				logger.Infof("setEnvironment: adding torrent %v(%v) on %q to queue",
					singleLog.TorrentName,
					singleLog.MetaInfo.HashInfoBytes(),
					singleLog.StoragePath)
				defer wg.Done()
				t, tmpErr := engine.TorrentEngine.AddTorrent(&singleLog.MetaInfo)
				if tmpErr != nil {
					logger.WithFields(log.Fields{"Error": tmpErr}).Infof("Failed to add torrent %q to client", singleLog.TorrentName)
					return
				}
				t.AddTrackers(clientConfig.DefaultTrackers)
				t.SetMaxEstablishedConns(clientConfig.EngineSetting.MaxEstablishedConns)
				engine.checkExtend(t)
				engine.EngineRunningInfo.TorrentLogs[i].Status = RunningStatus
				engine.WaitForCompleted(t)
				t.DownloadAll()
				logger.Infof("added %s to engine", singleLog.TorrentName)
			}(i, singleLog)
		}
	}
	go func() {
		wg.Wait()
		if len(engine.EngineRunningInfo.TorrentLogs) > 0 {
			logger.Info("all torrents from TorrentDB loaded")
		}
		engine.UpdateInfo()
	}()
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
		status := engine.EngineRunningInfo.TorrentLogs[index].Status
		if status != CompletedStatus {
			switch status {
			case AnalysingStatus:
				aimLog := engine.EngineRunningInfo.TorrentLogs[index]
				torrentHash := metainfo.Hash{}
				_ = torrentHash.FromHexString(aimLog.TorrentName)
				magnetTorrent, isExist := engine.TorrentEngine.Torrent(torrentHash)
				if isExist {
					logger.Info("One magnet will be deleted " + magnetTorrent.String())
					magnetTorrent.Drop()
				}
			case RunningStatus:
				engine.StopOneTorrent(engine.EngineRunningInfo.TorrentLogs[index].HashInfoBytes().HexString())
				//engine.EngineRunningInfo.TorrentLogs[index].Status = StoppedStatus
			case QueuedStatus:
				//engine.EngineRunningInfo.TorrentLogs[index].Status = StoppedStatus
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

func (engine *Engine) checkExtend(singleTorrent *torrent.Torrent) {
	//check if extend exist
	_, extendIsExist := engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()]
	if !extendIsExist {
		engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()] = &TorrentLogExtend{
			StatusPub:     singleTorrent.SubscribePieceStateChanges(),
			HasStatusPub:  true,
			HasMagnetChan: false,
		}
	} else if extendIsExist && !engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()].HasStatusPub {
		logger.Debug("it has extend but no status pub")
		engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()].HasStatusPub = true
		engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()].StatusPub = singleTorrent.SubscribePieceStateChanges()
	}
}
