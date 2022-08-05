package main

import (
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

func (this Worker) Work() {
	go func() {
		for {
			this.PingAllSites()
			time.Sleep(1 * time.Minute)
		}
	}()
}

func (this Worker) PingAllSites() {
	sites := this.model.AllSites()
	length := len(sites)

	var wg sync.WaitGroup
	wg.Add(length)

	for i := 0; i < length; i++ {
		go func(i int) {
			defer wg.Done()
			site := sites[i]
			res, err := http.Head(site.Url)
			if err != nil {
				this.model.CreatePing(site.Id, 500)
				//this.logger.Printf("%v", err.Error())
				return
			}
			//this.logger.Printf("%s %d", res.Status, res.StatusCode)
			this.model.CreatePing(site.Id, res.StatusCode)
		}(i)
	}

	wg.Wait()
}
