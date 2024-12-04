package utils

import (
	"os"
	"os/signal"
	"syscall"
)

func SetupCloseHandler(callback func()) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		callback()
		os.Exit(0)
	}()
}
