package utils

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func SetupCloseHandler(callback func()) {
	c := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	var once sync.Once

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		once.Do(func() {
			callback()
			done <- true
		})
	}()

	go func() {
		select {
		case <-done:
			os.Exit(0)
		case <-c:
			os.Exit(1)
		}
	}()
}
