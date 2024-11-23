package main

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
)

func GenFolderName() string {
	time := time.Now().Format("20060102150405")
	return "SYNC_" + time
}

type Sync struct {
	tempDir     string
	cloneFolder string
	archiveName string
	pattern     *regexp.Regexp
}

func NewSync(pattern *regexp.Regexp) Sync {
	name := GenFolderName()

	tempDir, err := os.MkdirTemp("", "")
	checkError(err)

	cloneFolder := filepath.Join(tempDir, name)
	err = os.Mkdir(cloneFolder, fs.ModeDir)
	checkError(err)

	return Sync{
		tempDir:     tempDir,
		cloneFolder: cloneFolder,
		archiveName: name,
		pattern:     pattern,
	}
}

func (state *Sync) FindRepo(ch chan<- string) {
	var wg sync.WaitGroup
	wg.Add(1)
	go state.findRepo(".", ch, &wg)
	wg.Wait()
	close(ch)
}

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

type Walk struct {
	wg   sync.WaitGroup
	path string
	re   regexp.Regexp
}

func NewWalk(path string, pattern regexp.Regexp) Walk {
	return Walk{wg: sync.WaitGroup{}, path: path, re: pattern}
}

func (sync *Sync) findRepo(path string, ch chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	dirs, err := os.ReadDir(path)
	if err != nil {
		log.Panicln("error reading dir:", err)
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		subpath := filepath.Join(path, dir.Name())
		if sync.pattern.MatchString(dir.Name()) && isGitRepo(subpath) {
			ch <- subpath
		} else {
			wg.Add(1)
			sync.findRepo(subpath, ch, wg)
		}
	}
}

func isGitRepo(path string) bool {
	_, err := git.PlainOpen(path)
	return err == nil
}

func main() {
	if len(os.Args) < 2 {
		log.Println("usage:", filepath.Base(os.Args[0]), " <Regex>")
		os.Exit(-1)
	}
	pattern, err := regexp.Compile(os.Args[1])
	if err != nil {
		panic(err)
	}
	sync := NewSync(pattern)

	repos := make(chan string, 100)
	go sync.FindRepo(repos)
	sync.Clone(repos)
	sync.CreateZipArchive()
}

func (sync *Sync) CreateZipArchive() {
	zipFilePath := filepath.Join(os.TempDir(), sync.archiveName+".zip")
	zipFile, err := os.OpenFile(zipFilePath, os.O_CREATE, fs.ModeCharDevice)
	if err != nil {
		log.Panicln("error opening zip file:", err)
		panic("failed to create zip file")
	}
	defer zipFile.Close()
	writer := zip.NewWriter(zipFile)
	defer writer.Close()
	writer.AddFS(os.DirFS(sync.tempDir))
	fmt.Println(zipFilePath)
}

func (sync *Sync) Clone(repos <-chan string) {
	for repo := range repos {
		log.Println("clone", repo)
		r, err := git.PlainOpen(repo)
		if err != nil {
			log.Fatalln(err)
		}
		head, err := r.Head()
		if err != nil {
			log.Fatalln("Error getting worktree:", repo, err)
		}
		dirName := filepath.Base(repo)

		newRepoPath := filepath.Join(sync.cloneFolder, dirName)
		log.Println(newRepoPath, repo, head.Name())
		newRepo, err := git.PlainClone(newRepoPath, false, &git.CloneOptions{
			URL:           repo,
			SingleBranch:  true,
			ReferenceName: head.Name(),
			Progress:      os.Stderr,
		})

		if err != nil {
			log.Fatalln("error cloning repo: ", repo)
		}

		remotes, err := newRepo.Remotes()
		if err != nil {
			log.Fatalln(err)
		}
		for _, remote := range remotes {
			if err := newRepo.DeleteRemote(remote.Config().Name); err != nil {
				log.Println("Failed to delete remote", remote)
			}
		}

		remotes, err = r.Remotes()
		if err != nil {
			log.Fatalln(err)
		}
		for _, remote := range remotes {
			if _, err := newRepo.CreateRemote(remote.Config()); err != nil {
				log.Fatalln(err)
			}
		}
	}
}
