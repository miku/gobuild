package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	beeutils "github.com/astaxie/beego/utils"
	"github.com/shxsun/gobuild/models"
	"github.com/shxsun/gobuild/utils"
	"github.com/shxsun/gobuild/xsh"
)

var GOPATH, GOBIN string

func init() {
	var err error
	GOPATH, err = filepath.Abs("project")
	if err != nil {
		lg.Fatal(err)
	}
	GOBIN, err = filepath.Abs("files")
	if err != nil {
		lg.Fatal(err)
	}
}

type Job struct {
	wbc     *utils.WriteBroadcaster
	cmd     *exec.Cmd
	sh      *xsh.Session
	project string //
	ref     string
	gopath  string // init
	srcDir  string // init
	sha     string // get
	sync.Mutex
}

func NewJob(project, ref string, wbc *utils.WriteBroadcaster) *Job {
	b := &Job{
		wbc:     wbc,
		sh:      xsh.NewSession(),
		project: project,
		ref:     ref,
	}
	//fmt.Println(reflect.TypeOf(wbc), wbc)
	if wbc != nil {
		b.sh.Stdout = wbc
		b.sh.Stderr = wbc
		//b.wbc = wbc
	}
	env := map[string]string{
		"PATH":    "/bin:/usr/bin:/usr/local/bin",
		"PROJECT": project,
	}
	b.sh.Env = env
	return b
}

// prepare environ
func (b *Job) init() (err error) {
	gopath, err := ioutil.TempDir("tmp", "gopath-")
	if err != nil {
		return
	}
	b.gopath, err = filepath.Abs(gopath)
	if err != nil {
		return
	}
	b.sh.Env["GOPATH"] = b.gopath
	b.srcDir = filepath.Join(b.gopath, "src", b.project)
	return
}

// download src
func (b *Job) get() (err error) {
	b.sh.Call("echo", []string{"downloading src"})
	err = b.sh.Call("go", []string{"get", "-v", "-d", b.project})
	if err != nil {
		return
	}
	err = b.sh.Call("echo", []string{"fetch", b.ref}, xsh.Dir(b.srcDir))
	if err != nil {
		return
	}
	// fetch branch
	err = b.sh.Call("git", []string{"fetch", "origin"})
	if err != nil {
		return
	}
	if b.ref == "-" {
		b.ref = "master"
	}
	err = b.sh.Call("git", []string{"checkout", b.ref})
	if err != nil {
		return
	}
	r, err := xsh.Capture("git", []string{"rev-parse", "HEAD"}, xsh.Dir(b.srcDir))
	if err != nil {
		return
	}
	b.sha = r.Trim()
	//log.Println("cur sha = ", b.sha)
	return
}

// build src
func (j *Job) build(os, arch string) (file string, err error) {
	fmt.Println(j.sh.Env)
	j.sh.Env["GOOS"] = os
	j.sh.Env["GOARCH"] = arch

	err = j.sh.Call("go", []string{"get", "-v", "."})
	if err != nil {
		return
	}
	// find binary
	target := filepath.Base(j.project)
	if os == "windows" {
		target += ".exe"
	}
	gobin := filepath.Join(j.gopath, "bin")
	return beeutils.SearchFile(target, gobin, filepath.Join(gobin, os+"_"+arch))
}

// achieve and upload
func (b *Job) pkg(bins []string) (addr string, err error) {
	return Package(bins, filepath.Join(b.srcDir, ".build"))
}

// remove tmp file
func (b *Job) clean() (err error) {
	b.sh.Call("echo", []string{"cleaning..."})
	err = os.RemoveAll(b.gopath)
	return
}

// init + build + pkg + clean
func (j *Job) Auto() (addr string, err error) {
	defer func() {
		if j.wbc != nil {
			j.wbc.CloseWriters()
		}
	}()
	if err = j.init(); err != nil {
		return
	}
	// defer clean should start when GOPATH success created
	defer func() {
		er := j.clean()
		if er != nil {
			lg.Warn(er)
		}
	}()
	// download src
	err = j.get()
	if err != nil {
		return
	}
	// build xc
	file, err := j.build("linux", "amd64")
	if err != nil {
		return
	}
	addr, err = j.pkg([]string{file})
	if err != nil {
		return
	}

	// save to db
	p := new(models.Project)
	p.Name = j.project
	p.Ref = "master" // TODO
	//err = models.SyncProject(p)
	if err != nil {
		return
	}
	return
}