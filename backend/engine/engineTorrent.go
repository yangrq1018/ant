package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	log "github.com/sirupsen/logrus"
)

func (engine *Engine) AddOneTorrentFromFile(filepathAbs string) (tmpTorrent *torrent.Torrent, err error) {
	torrentMetaInfo, err := metainfo.LoadFromFile(filepathAbs)
	if err == nil {
		return engine.AddOneTorrentFromInfoHash(torrentMetaInfo)
	}
	return tmpTorrent, err
}

func (engine *Engine) AddOneTorrentFromInfoHash(torrentMetaInfo *metainfo.MetaInfo) (tmpTorrent *torrent.Torrent, err error) {
	//To solve problem of different variable scope
	needMoreOperation := false
	tmpTorrent, needMoreOperation = engine.checkOneHash(torrentMetaInfo.HashInfoBytes())
	if needMoreOperation {
		tmpTorrent, err = engine.TorrentEngine.AddTorrent(torrentMetaInfo)
		engine.EngineRunningInfo.AddOneTorrent(tmpTorrent)
		engine.SaveInfo()
	}
	return tmpTorrent, err
}

//Check if duplicated torrent
func (engine *Engine) checkOneHash(infoHash metainfo.Hash) (tmpTorrent *torrent.Torrent, needMoreOperation bool) {
	torrentLog, isExist := engine.EngineRunningInfo.HashToTorrentLog[infoHash]
	if isExist && torrentLog.Status != CompletedStatus {
		logger.Info("Task has been created")
		tmpTorrent, _ = engine.TorrentEngine.Torrent(infoHash)
		needMoreOperation = false
	} else if isExist && torrentLog.Status == CompletedStatus {
		logger.Info("Task has been completed")
		tmpTorrent = nil
		needMoreOperation = false
	} else {
		logger.Info("Create a new task")
		needMoreOperation = true
	}
	return
}

// AddOneTorrentFromMagnet support 'magnet:' and 'infohash:'
func (engine *Engine) AddOneTorrentFromMagnet(linkAddress string) (tmpTorrent *torrent.Torrent, err error) {
	isMagnet, isInfoHash := strings.HasPrefix(linkAddress, "magnet:"), strings.HasPrefix(linkAddress, "infohash:")
	if isMagnet || isInfoHash {
		var infoHash metainfo.Hash
		if strings.HasPrefix(linkAddress, "magnet:") {
			var torrentMetaInfo *torrent.TorrentSpec
			torrentMetaInfo, err = torrent.TorrentSpecFromMagnetUri(linkAddress)
			if err != nil {
				logger.WithFields(log.Fields{"Error": err}).Error("unable to resolve magnet")
				return
			} else {
				infoHash = torrentMetaInfo.InfoHash
			}
		} else {
			infoHash = metainfo.NewHashFromHex(strings.TrimPrefix(linkAddress, "infohash:"))
		}
		var needMoreOperation bool
		tmpTorrent, needMoreOperation = engine.checkOneHash(infoHash)

		if needMoreOperation {
			engine.EngineRunningInfo.AddOneTorrentFromMagnet(infoHash)
			extendLog, _ := engine.EngineRunningInfo.TorrentLogExtends[infoHash]
			engine.EngineRunningInfo.MagnetNum++

			if isMagnet {
				tmpTorrent, err = engine.TorrentEngine.AddMagnet(linkAddress)
			} else {
				tmpTorrent, _ = engine.TorrentEngine.AddTorrentInfoHash(infoHash)
			}
			if err != nil {
				logger.WithFields(log.Fields{"Error": err, "Torrent": tmpTorrent}).Error("Unable to resolve magnet")
				return
			}
			go func() {
				select {
				case <-tmpTorrent.GotInfo():
					logger.Debug("Add torrent from magnet, url successfully resolved")
					engine.EngineRunningInfo.UpdateMagnetInfo(tmpTorrent)
					engine.GenerateInfoFromTorrent(tmpTorrent)
					engine.SaveInfo()
					engine.StartDownloadTorrent(tmpTorrent.InfoHash().HexString())
					engine.EngineRunningInfo.EngineCMD <- RefreshInfo
					logger.Debug("It should refresh")
					// save torrent as file
					if f, fErr := os.OpenFile(filepath.Join(clientConfig.EngineSetting.Tmpdir, tmpTorrent.Name()+".torrent"), os.O_WRONLY|os.O_CREATE, 0666); err == nil {
						defer f.Close()
						info := tmpTorrent.Metainfo()
						fErr = info.Write(f)
						if fErr != nil {
							logger.WithFields(log.Fields{"Error": err, "Torrent": tmpTorrent}).Error("Unable write torrent file")
						}
					} else {
						logger.WithFields(log.Fields{"Error": err, "Torrent": tmpTorrent}).Error("Unable save torrent file")
					}
				case <-extendLog.MagnetAnalyseChan:
					tmpTorrent.Drop()
					extendLog.MagnetDelChan <- true
					logger.Debug("One magnet has been deleted")
				}
				engine.EngineRunningInfo.MagnetNum--
			}()
		}
	} else {
		err = errors.New("invalid address")
	}
	return tmpTorrent, err
}

// GetOneTorrent Only handle torrent in client
func (engine *Engine) GetOneTorrent(hexString string) (tmpTorrent *torrent.Torrent, isExist bool) {
	torrentHash := metainfo.Hash{}
	err := torrentHash.FromHexString(hexString)
	if err != nil {
		logger.WithFields(log.Fields{"Error": err}).Error("Unable to get hash from hex string")
		tmpTorrent = nil
		isExist = false
	} else {
		tmpTorrent, isExist = engine.TorrentEngine.Torrent(torrentHash)

		//any operation on magnet is forbidden
		if isExist {
			if torrentLog, ok := engine.EngineRunningInfo.HashToTorrentLog[tmpTorrent.InfoHash()]; ok && torrentLog.Status == AnalysingStatus {
				tmpTorrent = nil
				isExist = false
			}
		}
	}
	return
}

//Max number of downloading torrents should be considered in electron
func (engine *Engine) StartDownloadTorrent(hexString string) (downloaded bool) {
	downloaded = true
	singleTorrent, isExist := engine.GetOneTorrent(hexString)
	if isExist {
		singleTorrentLog, _ := engine.EngineRunningInfo.HashToTorrentLog[singleTorrent.InfoHash()]
		if singleTorrentLog.Status != RunningStatus {
			singleTorrentLog.Status = RunningStatus
			engine.checkExtend(singleTorrent)
			//Some download setting for task
			singleTorrent.AddTrackers(clientConfig.DefaultTrackers)
			singleTorrent.SetMaxEstablishedConns(clientConfig.EngineSetting.MaxEstablishedConns)
			engine.WaitForCompleted(singleTorrent)
			singleTorrent.DownloadAll()
		}
	} else {
		downloaded = false
	}
	return
}

func (engine *Engine) CompleteOneTorrent(singleTorrent *torrent.Torrent) {
	singleTorrentLog, exist := engine.EngineRunningInfo.HashToTorrentLog[singleTorrent.InfoHash()]
	if !exist {
		return
	}
	singleTorrentLogExtend, extendExist := engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()]
	<-singleTorrent.GotInfo()
	//One more check
	entry := logger.WithFields(log.Fields{"TorrentName": singleTorrent.Name()})
	if singleTorrent.BytesCompleted() == singleTorrent.Info().TotalLength() {
		entry.Info("Torrent has been finished, verifying data...")
		singleTorrent.VerifyData()
		entry.Infof("Data verified!")
		singleTorrentLog.Status = CompletedStatus
		engine.SaveInfo()
		if extendExist && singleTorrentLogExtend.HasStatusPub && singleTorrentLogExtend.StatusPub != nil {
			singleTorrentLogExtend.HasStatusPub = false
			if !channelClosed(singleTorrentLogExtend.StatusPub.Values) {
				singleTorrentLogExtend.StatusPub.Values <- struct{}{}
				singleTorrentLogExtend.StatusPub.Close()
			}
		}
	} else {
		entry.Warnf("Torrent wants to be marked as finished, but bytes are not totally completed")
	}
}

func (engine *Engine) WaitForCompleted(singleTorrent *torrent.Torrent) {
	go func() {
		singleTorrentLog := engine.EngineRunningInfo.HashToTorrentLog[singleTorrent.InfoHash()]
		singleTorrentLogExtend := engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()]
		<-singleTorrent.GotInfo()
		for singleTorrentLog.Status == RunningStatus {
			if singleTorrent.BytesCompleted() == singleTorrent.Info().TotalLength() {
				engine.CompleteOneTorrent(singleTorrent)
				engine.UpdateInfo()
				return
			}
			<-singleTorrentLogExtend.StatusPub.Values
		}
		log.WithFields(log.Fields{"TorrentName": singleTorrentLog.TorrentName, "Status": singleTorrentLog.Status}).Info("Torrent status changed !")
	}()
}

func (engine *Engine) StopOneTorrent(hexString string) (stopped bool) {
	singleTorrent, torrentExist := engine.GetOneTorrent(hexString)
	if torrentExist {
		singleTorrentLog := engine.EngineRunningInfo.HashToTorrentLog[singleTorrent.InfoHash()]
		if singleTorrentLog.Status != CompletedStatus {
			singleTorrentLog.Status = StoppedStatus
			engine.SaveInfo()
			//engine.EngineRunningInfo.UpdateTorrentLog()
			singleTorrentLogExtend, extendExist := engine.EngineRunningInfo.TorrentLogExtends[singleTorrent.InfoHash()]
			if extendExist && singleTorrentLogExtend.HasStatusPub && singleTorrentLogExtend.StatusPub != nil {
				singleTorrentLogExtend.HasStatusPub = false
				if !channelClosed(singleTorrentLogExtend.StatusPub.Values) {
					singleTorrentLogExtend.StatusPub.Values <- struct{}{}
					singleTorrentLogExtend.StatusPub.Close()
				}
			}
			singleTorrent.SetMaxEstablishedConns(0)
		}
		stopped = true
	} else {
		stopped = false
	}
	return
}

// TODO: Find error of out range of index, not find reason now
// Delete on torrent will operate logs directly, rather than get from getOne
func (engine *Engine) DelOneTorrent(hexString string) (deleted bool) {
	deleted = false

	for index := 0; index < len(engine.EngineRunningInfo.TorrentLogs); index++ {
		if engine.EngineRunningInfo.TorrentLogs[index].Status != AnalysingStatus && engine.EngineRunningInfo.TorrentLogs[index].HashInfoBytes().HexString() == hexString {
			if engine.EngineRunningInfo.TorrentLogs[index].Status == RunningStatus {
				engine.StopOneTorrent(hexString)
			}
			singleTorrent, torrentExist := engine.TorrentEngine.Torrent(engine.EngineRunningInfo.TorrentLogs[index].HashInfoBytes())
			if torrentExist {
				singleTorrent.Drop()
			}
			filePath := filepath.Join(engine.EngineRunningInfo.TorrentLogs[index].StoragePath, engine.EngineRunningInfo.TorrentLogs[index].TorrentName)
			logger.WithFields(log.Fields{"Path": filePath}).Info("Files have been deleted!")
			//fmt.Printf("Before delete: %+v\n", engine.EngineRunningInfo.TorrentLogsAndID)
			engine.EngineRunningInfo.TorrentLogsAndID.TorrentLogs = append(engine.EngineRunningInfo.TorrentLogs[:index], engine.EngineRunningInfo.TorrentLogs[index+1:]...)
			//fmt.Printf("After delete: %+v\n", engine.EngineRunningInfo.TorrentLogsAndID)
			engine.UpdateInfo()
			engine.SaveInfo()
			delFiles(filePath)
			deleted = true
		} else if engine.EngineRunningInfo.TorrentLogs[index].Status == AnalysingStatus && engine.EngineRunningInfo.TorrentLogs[index].TorrentName == hexString {
			//Magnet hash is stored in torrentName
			torrentHash := metainfo.Hash{}
			_ = torrentHash.FromHexString(engine.EngineRunningInfo.TorrentLogs[index].TorrentName)
			extendLog := engine.EngineRunningInfo.TorrentLogExtends[torrentHash]
			extendLog.MagnetAnalyseChan <- true
			<-extendLog.MagnetDelChan
			engine.EngineRunningInfo.TorrentLogs = append(engine.EngineRunningInfo.TorrentLogs[:index], engine.EngineRunningInfo.TorrentLogs[index+1:]...)
			engine.UpdateInfo()
			engine.SaveInfo()
			deleted = true
			logger.Debug("Delete Magnet Done")
			return
		}
	}
	return
}

func delFiles(path string) {
	err := os.RemoveAll(path)
	if err != nil {
		logger.WithFields(log.Fields{"Error": err}).Error("unable to delete files")
	}
}

func channelClosed(ch <-chan interface{}) bool {
	select {
	case <-ch:
		return true
	default:
	}
	return false
}
