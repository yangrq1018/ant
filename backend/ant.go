package main

import (
	"github.com/anatasluo/ant/backend/engine"
	"github.com/anatasluo/ant/backend/router"
	"github.com/anatasluo/ant/backend/setting"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/negroni"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

var (
	clientConfig = setting.GetClientSetting()
	logger       = clientConfig.LoggerSetting.Logger
	nRouter      *negroni.Negroni
)

func runAPP() {
	go func() {
		// Init server router
		nRouter = router.InitRouter()
		err := http.ListenAndServe(clientConfig.ConnectSetting.Addr, nRouter)
		if err != nil {
			logger.WithFields(log.Fields{"Error": err}).Fatal("Failed to created http service")
		}

	}()
}

func cleanUp() {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt,
			syscall.SIGHUP,
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGQUIT)
		<-c
		log.Info("The programme will stop!")
		engine.GetEngine().Cleanup()
		os.Exit(0)
	}()
}

func main() {
	runAPP()
	cleanUp()
	runtime.Goexit()
}
