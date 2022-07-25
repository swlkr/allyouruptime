package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Worker struct {
	logger Logger
	model  Model
}

func NewWorker(logger Logger, model Model) Worker {
	return Worker{
		logger: logger,
		model:  model,
	}
}

func (w Worker) Work() {
	go func() {
		for {
			w.PingAllSites()
			time.Sleep(1 * time.Minute)
		}
	}()
}

func (w Worker) PingAllSites() {
	sites := w.model.AllSites()
	length := len(sites)

	var wg sync.WaitGroup
	wg.Add(length)

	fmt.Println("Starting Work()")

	for i := 0; i < length; i++ {
		go func(i int) {
			defer wg.Done()
			site := sites[i]
			w.logger.Printf("HEAD %s", site.Url)
			res, err := http.Head(site.Url)
			if err != nil {
				w.logger.Printf("%v", err.Error())
				return
			}
			w.logger.Printf("%s %d", res.Status, res.StatusCode)
		}(i)
	}

	wg.Wait()
	fmt.Println("Finished Work()")
}
