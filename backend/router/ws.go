package router

import (
	"github.com/anatasluo/ant/backend/engine"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"net/http"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

//TODO : close handle
func torrentProgress(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

	logger.Info("websocket created!")
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Unable to init websocket", err)
		return
	}
	defer func() {
		_ = conn.Close()
	}()
	var tmp engine.MessageFromWeb
	var resInfo engine.TorrentProgressInfo
	for {

		select {
		case cmdID := <-runningEngine.EngineRunningInfo.EngineCMD:
			{
				logger.Debug("Send CMD Now: ", cmdID)
				if cmdID == engine.RefreshInfo {
					resInfo.MessageType = engine.RefreshInfo
					err = conn.WriteJSON(resInfo)
					if err != nil {
						logger.Error("Unable to write message", err)
					}
				}
			}
		default:
		}
		err = conn.ReadJSON(&tmp)
		if err != nil {
			logger.Errorf("Unable to read ressage: %v", err)
			break
		}

		if tmp.MessageType == engine.GetInfo {
			singleTorrent, isExist := runningEngine.GetOneTorrent(tmp.HexString)
			if isExist {
				singleTorrentLog := runningEngine.EngineRunningInfo.HashToTorrentLog[singleTorrent.InfoHash()]
				if singleTorrentLog != nil && (singleTorrentLog.Status == engine.RunningStatus || singleTorrentLog.Status == engine.CompletedStatus) {
					singleWebLog := runningEngine.GenerateInfoFromTorrent(singleTorrent)
					resInfo.MessageType = engine.GetInfo
					resInfo.HexString = singleWebLog.HexString
					resInfo.Percentage = singleWebLog.Percentage
					resInfo.LeftTime = singleWebLog.LeftTime
					resInfo.DownloadSpeed = singleWebLog.DownloadSpeed
					_ = conn.WriteJSON(resInfo)
				}
			}
		}

	}

}

func handleWS(router *httprouter.Router) {
	router.GET("/ws", torrentProgress)
}
