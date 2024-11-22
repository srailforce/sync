package main

import (
	"archive/zip"
	"fmt"
	"io"
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

	return "SYNC_" + time + "_"
}

type Walk struct {
	wg   sync.WaitGroup
	path string
	re   regexp.Regexp
}

func NewWalk(path string, pattern regexp.Regexp) Walk {
	return Walk{wg: sync.WaitGroup{}, path: path, re: pattern}
}

func WalkDir(dirFs string, pattern regexp.Regexp, emitter chan<- string, wg *sync.WaitGroup) {
	dirs, err := fs.ReadDir(os.DirFS(dirFs), ".")
	if err != nil {
		log.Panicln("error reading dir:", err)
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		path := filepath.Join(dirFs, dir.Name())
		if pattern.MatchString(dir.Name()) && IsGitRepo(path) {
			log.Println("send ", path)
			emitter <- path
		} else {
			wg.Add(1)
			WalkDir(path, pattern, emitter, wg)
		}
	}
	defer wg.Done()
}

func (walk *Walk) WalkDir(ch chan<- string) {
	walk.wg.Add(1)
	go WalkDir(walk.path, walk.re, ch, &walk.wg)
	walk.wg.Wait()
	close(ch)
}

func IsGitRepo(path string) bool {
	_, err := git.PlainOpen(path)
	return err == nil
}

func main() {
	if len(os.Args) < 2 {
		log.Println("usage:", os.Args[0], " <Regex>")
		os.Exit(-1)
	}
	pattern, err := regexp.Compile(os.Args[1])
	if err != nil {
		panic(err)
	}

	folderName := GenFolderName()
	tempDir, err := os.MkdirTemp("", folderName)
	dirName := filepath.Base(tempDir)
	cloneDir := filepath.Join(tempDir, dirName)
	if err := os.Mkdir(cloneDir, os.ModeDir); err != nil {
		log.Fatal("error creating temp dir", err)
		panic(err)
	}

	if err != nil {
		log.Println("error creating temp dir:", err)
		return
	}

	log.Println("Clone into ", tempDir)
	repos := make(chan string, 100)
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	log.Println("current dir", wd)
	var cloneWg sync.WaitGroup
	w := NewWalk(wd, *pattern)
	go w.WalkDir(repos)
	cloneWg.Add(1)
	go Clone(cloneDir, repos, &cloneWg)
	cloneWg.Wait()
	w.wg.Wait()
	// close(repos)
	log.Println("end wait")
	CreateZipArchive(tempDir, dirName)
}

func CreateZipArchive(tempDir, folerName string) {
	rootFolder := os.TempDir()
	fileName := filepath.Join(rootFolder, folerName+".zip")
	zipFile, err := os.OpenFile(fileName, os.O_CREATE, fs.ModeCharDevice)
	if err != nil {
		log.Panicln("error opening zip file:", err)
		panic("failed to create zip file")
	}
	defer zipFile.Close()
	writer := zip.NewWriter(zipFile)
	defer writer.Close()
	writer.AddFS(os.DirFS(tempDir))
	fmt.Println(fileName)
}

func Clone(tempDir string, repos chan string, wg *sync.WaitGroup) {
	for {
		if repo, ok := <-repos; ok {
			log.Println("clone", repo)
			r, err := git.PlainOpen(repo)
			if err != nil {
				log.Println(err)
				return
			}
			head, err := r.Head()
			if err != nil {
				log.Println("Error getting worktree:", repo, err)
				return
			}
			dirName := filepath.Base(repo)

			_, err = git.PlainClone(filepath.Join(tempDir, dirName), false, &git.CloneOptions{
				URL:           repo,
				SingleBranch:  true,
				ReferenceName: head.Name(),
				Progress:      io.Discard,
			})

			if err != nil {
				log.Println("error cloning repo: ", repo)
			}
		} else {
			log.Println("end clone")
			break
		}
	}
	defer wg.Done()
}
