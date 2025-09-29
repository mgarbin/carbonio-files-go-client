package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type FileEvent struct {
	CreatedAt time.Time
	IsDir     bool
}

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s <folder-to-watch>\n", os.Args[0])
		os.Exit(1)
	}
	watchDir := os.Args[1]
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	var mu sync.Mutex
	created := make(map[string]FileEvent)
	done := make(chan bool)

	// Recursive watch
	err = filepath.Walk(watchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			err = watcher.Add(path)
			if err != nil {
				log.Printf("Failed to watch directory %s: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				switch {
				case event.Op&fsnotify.Create == fsnotify.Create:
					fi, err := os.Stat(event.Name)
					if err == nil && fi.IsDir() {
						fmt.Printf("New folder: %s\n", event.Name)
						watcher.Add(event.Name)
						mu.Lock()
						created[event.Name] = FileEvent{CreatedAt: time.Now(), IsDir: true}
						mu.Unlock()
					} else {
						fmt.Printf("New file: %s\n", event.Name)
						mu.Lock()
						created[event.Name] = FileEvent{CreatedAt: time.Now(), IsDir: false}
						mu.Unlock()
					}
				case event.Op&fsnotify.Write == fsnotify.Write:
					mu.Lock()
					if ce, ok := created[event.Name]; ok && time.Since(ce.CreatedAt) < 3*time.Second {
						if ce.IsDir {
							fmt.Printf("New folder (modified): %s\n", event.Name)
						} else {
							fmt.Printf("New file (modified): %s\n", event.Name)
						}
						delete(created, event.Name)
					} else {
						fmt.Printf("Modified: %s\n", event.Name)
					}
					mu.Unlock()
				case event.Op&fsnotify.Remove == fsnotify.Remove:
					fmt.Printf("Deleted: %s\n", event.Name)
					mu.Lock()
					delete(created, event.Name)
					mu.Unlock()
				case event.Op&fsnotify.Rename == fsnotify.Rename:
					fmt.Printf("Renamed: %s\n", event.Name)
					mu.Lock()
					delete(created, event.Name)
					mu.Unlock()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("Error:", err)
			}
		}
	}()

	fmt.Printf("Watching folder: %s\n", watchDir)
	<-done
}
