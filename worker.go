package main


import (
	"k8s.io/client-go/util/workqueue"
	"github.com/google/go-github/github"
	"log"
	"time"
	"context"

)

//GitWorker a Simple workquie executor
type GitWorker struct {
	Name string
	Cli  *github.Client
	Ctx context.Context
	MsgProcessed int
	SleepInterval time.Duration
	Limit *github.Rate
	Q *workqueue.Type
	isCore bool
}

func NewGitWorker (ctx context.Context, cli *github.Client, Name string, core bool) *GitWorker {
	return &GitWorker{MsgProcessed:0, SleepInterval:10, Name: Name, Ctx:ctx, Cli:cli, Q:workqueue.NewNamed(Name), isCore:core}
}

func (W *GitWorker) ProcessWorkItem () {

	log.Printf("%s Worker - Started", W.Name)
	defer log.Printf("%s Worker - Finished", W.Name)

	for {

		W.Limit = nil

		item, shutdown := W.Q.Get()
		if shutdown {
			log.Printf("%s Worker - Recived shutdown signal", W.Name)
			return
		}
		rL, _, err := W.Cli.RateLimits(W.Ctx)
		if err != nil {
			log.Printf("RateLimits() error =%v", err)
			time.Sleep(time.Second)
			continue
		}

		if W.isCore {
			W.Limit = rL.Core
		} else {
			W.Limit = rL.Search
		}

		if W.Limit.Remaining == 0 {
			log.Printf("%s Worker - No remaining calls available, resets at %v", W.Name, W.Limit.Reset.Time)
			time.Sleep(time.Second * W.SleepInterval)
			continue
		}

		W.MsgProcessed++
		fn := item.(func() error)
		err = fn()
		if err != nil {
			log.Printf("%s Worker - Error processign message=%v", W.Name, err)
		}
		W.Q.Done(fn)

	}

}

