package router

import (
	"github.com/anatasluo/ant/backend/engine"
	"github.com/anatasluo/ant/backend/setting"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/cors"
	"github.com/urfave/negroni"
)

var (
	clientConfig  = setting.GetClientSetting()
	runningEngine *engine.Engine
	logger        = clientConfig.LoggerSetting.Logger
)

func InitRouter() *negroni.Negroni {
	runningEngine = engine.GetEngine()
	router := httprouter.New()

	// Enable router
	handleTorrent(router)
	handleMagnet(router)
	handleWS(router)
	handlePlayer(router)
	handleSetting(router)

	// Use global middleware
	n := negroni.New()

	//Enable cors
	c := cors.AllowAll()
	n.Use(c)

	//Enable auth
	//auth := setting.Auth{Username : clientConfig.ConnectSetting.AuthUsername, Password : clientConfig.ConnectSetting.AuthPassword}
	//auth.Hash()
	//n.Use(auth)

	n.Use(negroni.NewLogger())

	n.UseHandler(router)

	return n
}
