package main

import
(
	"fmt"
	"context"
	"github.com/google/go-github/github"
	"strings"
	"time"
	"os"
	"sync"
	"log"
)

var Organizations = []string {"kubernetes", "kubernetes-incubator"}
var Users = []string { "dhilipkumars" }
var ctx context.Context
var cli *github.Client
var core *GitWorker
var search *GitWorker

type Table struct {
	sync.Mutex					//Lock
	Row map[string]*Record 		//Table to be populated
	RemaingCalls 	int			//Remain number of HTTP calls this we are allowed to make for this client connection
	ResetTime		time.Time	//If we ran out of the HTTP calls for this hour, when does it reset?
	Update time.Time			//Last updated time for commits
}

type Record struct {
	sync.Mutex
	Name string 		//Name of the person
	GHandle string		//Github handle of this person
	Commits int			//Number of commits so far
	NewCommits int		//Current processing number of commits
	Updated time.Time   //Last updated at
}

func (T *Table) Add (rec *Record) error {

	T.Lock()
	defer T.Unlock()

	T.Row[rec.GHandle] = rec
	return nil

}

func (T *Table) Exist(gHandle string) bool {

	_, ok := T.Row[gHandle]
	return ok
}

func (T *Table) zeroCommits () {

	for _, val := range T.Row {
		val.NewCommits = 0
	}
}

func (T *Table) swapToCommits () {

	for _, val := range T.Row {
		val.Commits = val.NewCommits
	}
}

func (T *Table) Tabulate () []Record {

	var result []Record

	for _, val := range T.Row {
		result = append(result, *val)
	}

	return result
}

//Get the latest updated commit counts
func (T *Table) SyncCommits () error {

	var wg sync.WaitGroup


	//Nullify the commit number for all the users
	T.zeroCommits()
	//for Orgaizations
	for _, org := range Organizations {
		T.CallorWait()
		opt := &github.RepositoryListByOrgOptions{Type: "public"}
		//Get Maximum number of repos
		opt.PerPage = 1000
		repos , resp, err := cli.Repositories.ListByOrg(ctx, org, opt)

		for _, repo := range repos {

			wg.Add(1)

			GetContributions(resp)


		}
	}
	T.swapToCommits()

	return nil
}

// CallorWait is a blocking call, which will first check if we have enough remaining HTTP calls we can make for this hour
// other wise we will know when it gets reset, so we will wait (sleeping) until the re-set
func (T *Table) CallorWait() {

	T.UpdateRate()

	if T.RemaingCalls > 0 {
		return
	}

	timetoWait := T.ResetTime.Sub(time.Now()).Seconds()
	log.Printf("Exhaused all the calls for this hour, Waiting for %d", timetoWait)

	for timetoWait > 0 {
		time.Sleep(time.Second)
		timetoWait -= 1
		if int(timetoWait) % 10 == 0 {
			log.Printf("%dsecs more to retry..", timetoWait)
		}
	}
}

//
func (T *Table) UpdateRate () {

	rate, _, err := cli.RateLimits(ctx)

	if err != nil {
		log.Printf("Unable to get Rate err=%v\n", err)
		return
	}

	T.RemaingCalls = rate.Core.Remaining
	T.ResetTime = rate.Core.Reset.Time
}

func NewTable () *Table {
	var T Table
	T.Row = make(map[string]*Record)

	return &T
}



func NewRecord (Name , GHandle string, commits int) *Record {
	var R Record
	R.Name = Name
	R.GHandle = GHandle
	R.Commits = commits

	return &R
}

func sumStats (weeks []github.WeeklyStats) github.WeeklyStats{
	var res github.WeeklyStats

	res.Commits = new(int)
	res.Additions = new(int)
	res.Deletions = new(int)

	for _, w := range weeks {
		*res.Additions += *w.Additions
		*res.Deletions += *w.Deletions
		*res.Commits += *w.Commits
	}
	return res
}

func GetContributions (ctx context.Context, cli *github.Client, org, repo string) ([]*github.ContributorStats, *github.Response, error) {

	retryCount := 3
	var contribs []*github.ContributorStats
	var res *github.Response
	var err error

	cli.Repositories.ListCommits()

	for retryCount > 0 {
		contribs, res, err = cli.Repositories.ListContributorsStats(ctx, org, repo)
		if err != nil {
			fmt.Printf("Error Occured %v\n", err)
			if strings.Contains(err.Error(), "job scheduled on GitHub side; try again later") {
				time.Sleep(time.Second * 30 )
				retryCount -= 1
				continue
			}
		}
		if res.Remaining == 0 {
			//No more retry available, we have exausted the number of retries for this session
			waitSeconds := res.Reset.Second()
			for waitSeconds > 0 {
				time.Sleep(time.Second)
				fmt.Fprintf(os.Stderr, "Will retry in %dsecs\n", waitSeconds)
				waitSeconds -= 1
			}
			continue
		}
		break
	}


	return contribs, res, err
}

func init() {

	ctx = context.Background()
	cli = github.NewClient(nil)
	core = NewGitWorker(ctx,cli,"Core", true)
	search = NewGitWorker(ctx, cli, "Search", false)

	go core.ProcessWorkItem()
	go search.ProcessWorkItem()

}

func main () {

//	userName := "dhilipkumars"
	orgName := "kubernetes"
	repoName := "kubernetes"




/*
	contribs, res, err := GetContributions(ctx, cli, orgName, repoName)
	fmt.Printf("Response res=%v firstPage=%d page=%d lastpage=%d \n", *res, res.FirstPage, res.NextPage, res.LastPage)
	if err != nil {
		fmt.Printf("Obtained error =%v\n", err)
		return
	}

	fmt.Printf("Total Contributors for this repo %s  is %d\n", repoName, len(contribs))
	for _ , c := range contribs {
		fmt.Printf("User:%s, Total=%d Stats=%v\n",*c.Author.Login, *c.Total, sumStats(c.Weeks))
	}

	*/


	////opt := github.ListOptions{PerPage:10000}
	var opt github.ListContributorsOptions
	opt.PerPage = 10000

	var cpage, lpage int
	var finContributors []*github.Contributor

	cpage = 0
	lpage = 1000

	for cpage < lpage {

		// cli.Organizations.ListMembers()
		contributors, cres, cerr := cli.Repositories.ListContributors(ctx, orgName, repoName, &opt)
		fmt.Printf("Response res=%v\n", *cres)
		if cerr != nil {
			fmt.Printf("Obtained error =%v\n", cerr)
			return
		}
		fmt.Printf("There are more pages to fetch %d\n", lpage - cpage)
		lpage = cres.LastPage
		cpage = cres.NextPage
		opt.Page = cpage
		time.Sleep(time.Millisecond * 100)
		finContributors = append(finContributors, contributors...)
	}
	fmt.Printf("Total Contributors for this repo %s  is %d\n", repoName, len(finContributors))
	for _ , c := range finContributors {

		fmt.Printf("User:%s, Total=%d\n",*c.Login, c.GetContributions())
	}

}
