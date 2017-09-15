package main

import (
	"context"
	"fmt"
	"github.com/google/go-github/github"
	"log"
	"sync"
	"time"
	"sort"
)

var Organizations = []string{"kubernetes", "kubernetes-incubator"}
//var Organizations = []string{"kubernetes"}
var ctx context.Context
var cli *github.Client
var core *GitWorker
var search *GitWorker

const (
	UserSyncPeriod   = 2
	CommitSyncPeriod = 1
	PRSyncPeriod     = 1
	ItemsPerPage     = 1000
	HuaweiOrg        = "Huawei-PaaS"
	HTTPServerPort   = ":8989"
)

type Table struct {
	sync.Mutex                    //Lock
	Row        map[string]*Record //Table to be populated
	Update     time.Time          //Last updated time for commits
}

func (T *Table) Add(rec *Record) error {

	T.Lock()
	defer T.Unlock()

	T.Row[rec.GHandle] = rec
	return nil

}

func (T *Table) Exist(gHandle string) bool {

	_, ok := T.Row[gHandle]
	return ok
}

func (T *Table) zeroCommits() {

	for _, val := range T.Row {
		val.NewCommits = 0
	}
}

func (T *Table) swapToCommits() {

	for _, val := range T.Row {
		val.Commits = val.NewCommits
	}
}

type ByCommit []Record

func (b ByCommit) Len() int {return len(b)}
func (b ByCommit) Swap(i,j int) {b[i], b[j] = b[j], b[i]}
func (b ByCommit) Less(i, j int) bool {return b[i].Commits > b[j].Commits}

func (T *Table) Tabulate() []Record {

	var result []Record

	for _, val := range T.Row {
		if val.Commits > 0 {
			result = append(result, *val)
		}
	}

	sort.Sort(ByCommit(result))

	return result
}

//Sync syncs the the table and update it.
func (T *Table) Sync() error {
	UserCh := time.After(time.Second * 10)
	CommitCh := time.After(time.Minute * CommitSyncPeriod)
	PRCh := time.After(time.Minute * PRSyncPeriod)

	for {
		select {

		case <-UserCh:
			T.SyncUser()
			UserCh = time.After(time.Hour * UserSyncPeriod)
			log.Printf("Table->Sync() Synced Users")

		case <-CommitCh:
			T.SyncCommits()
			log.Printf("Table->Sync() Synced Commits")
			CommitCh = time.After(time.Hour * CommitSyncPeriod)

		case <-PRCh:
			//T.SyncPRs()
			log.Printf("Table->Sync() Synced PRs")
			PRCh = time.After(time.Hour * PRSyncPeriod)
		}

	}
}

func (T *Table) SyncUser() error {

	var err error
	var res *github.Response
	var currentPage, lastPage int
	var wg sync.WaitGroup
	var opt github.ListMembersOptions
	var result, members []*github.User

	currentPage = 1
	lastPage = 1000
	opt.PerPage = ItemsPerPage
	opt.Page = currentPage
	opt.PublicOnly = true

	for currentPage <= lastPage {
		wg.Add(1)
		core.AddMsg(func() error {
			members, res, err = cli.Organizations.ListMembers(ctx, HuaweiOrg, &opt)
			wg.Done()
			return err
		})
		wg.Wait()
		if err != nil {
			log.Printf("Table->SyncUser() Error getting user list err=%v", err)
			return err
		}
		result = append(result, members...)
		currentPage++
		opt.Page = currentPage
		lastPage = res.LastPage
	}

	for _, usr := range result {
		userName := *usr.Login
		if !T.Exist(userName) {
			T.Add(NewRecord(userName, userName, 0))
		}
	}

	return err

}

//Get the latest updated commit counts
func (T *Table) SyncCommits() error {

	var wg sync.WaitGroup
	var reposInOrgs []*github.Repository
	var err error

	//Nullify the commit number for all the users
	T.zeroCommits()
	opt := &github.RepositoryListByOrgOptions{Type: "public"}
	opt.PerPage = ItemsPerPage
	//for Orgaizations
	for _, org := range Organizations {

		//Get Maximum number of repos
		wg.Add(1)
		core.AddMsg(func() error {
			reposInOrgs, _, err = cli.Repositories.ListByOrg(ctx, org, opt)
			wg.Done()
			return err
		})
		wg.Wait()

		if err != nil {
			log.Printf("Table->SyncCommits() Error collecting repos for org=%s err=%v", org, err)
			return err
		}

		for _, repo := range reposInOrgs {

			contributors, err := T.GetContributors(org, *repo.Name)
			if err != nil {
				log.Printf("Table->SyncCommits() Error collecting contributors for org=%s repo=%s err=%v", org, repo.Name, err)
				return err
			}
			//Update the contributors
			for _, c := range contributors {
				userName := *c.Login
				if T.Exist(userName) {
					T.Row[userName].NewCommits += c.GetContributions()
					log.Printf("Table->SyncCommits() %s/%s/%s contrib=%d Total=%d", org, *repo.Name, userName, c.GetContributions(), T.Row[userName].NewCommits)
				}
			}
		}
	}
	T.swapToCommits()
	T.Update = time.Now()

	return err
}

func (T *Table) GetContributors(orgName, repoName string) ([]*github.Contributor, error) {

	var result, contribs []*github.Contributor
	var err error
	var res *github.Response
	var currentPage, lastPage int
	var wg sync.WaitGroup
	var opt github.ListContributorsOptions

	//Default the page numbers
	currentPage = 1
	lastPage = 1000
	opt.PerPage = ItemsPerPage
	opt.Page = currentPage

	for currentPage <= lastPage {

		wg.Add(1)

		core.AddMsg(func() error {
			contribs, res, err = cli.Repositories.ListContributors(ctx, orgName, repoName, &opt)
			wg.Done()
			return err
		})
		wg.Wait()
		currentPage++
		opt.Page = currentPage
		lastPage = res.LastPage
		if err != nil {
			return result, err
		}
		result = append(result, contribs...)
	}
	return result, err
}

//NewTable creates a dummy
func NewTable() *Table {
	var T Table
	T.Row = make(map[string]*Record)

	return &T
}

type Record struct {
	sync.Mutex
	Name       string    //Name of the person
	GHandle    string    //Github handle of this person
	Commits    int       //Number of commits so far
	NewCommits int       //Current processing number of commits
	Updated    time.Time //Last updated at
}

func NewRecord(Name, GHandle string, commits int) *Record {
	var R Record
	R.Name = Name
	R.GHandle = GHandle
	R.Commits = commits

	return &R
}

func init() {

	ctx = context.Background()
	cli = github.NewClient(nil)
	core = NewGitWorker(ctx, cli, "Core", true)
	search = NewGitWorker(ctx, cli, "Search", false)

	go core.ProcessWorkItem()
	go search.ProcessWorkItem()

}

func mainfunc() {

	T := NewTable()

	//Start the go sync table
	go T.Sync()
	//Start the webserver
	go StartServer(T)

	for {
		select {
		case <-time.After(time.Minute):
			recs := T.Tabulate()
			tableStr := fmt.Sprintf("====================Updated %v==============\n", T.Update)
			for i, rec := range recs {
				tableStr += fmt.Sprintf("%3d) %5s\t\t%d\t%d\n", i+1, rec.GHandle, rec.Commits, rec.NewCommits)
			}
			tableStr += fmt.Sprintf("=============================================\n")
			log.Println(tableStr)
		}
	}
}

func main() {

	mainfunc()

}
