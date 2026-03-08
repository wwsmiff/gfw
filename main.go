package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
)

// file watcher operation enum
type FileWatcherEvent = uint8

const (
	FileWatcherRebuild FileWatcherEvent = iota
)

func main() {
	root := "./tmp"
	//ignored := []string{".git"}
	fw_channel := make(chan FileWatcherEvent, 1)
	root_abs_path, err := filepath.Abs(root)
	build_args := []string{"go", "build", "."}
	run_args := []string{"./test"}

	build_ongoing := false
	rebuild_pending := false

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = watcher.Add(root_abs_path)
	if err != nil {
		log.Fatal(err)
	}

	// queue and log events
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if filepath.Base(event.Name) == "test" {
					continue
				}

				if event.Op&fsnotify.Create == fsnotify.Create {
					fileinfo, err := os.Stat(event.Name)
					if err != nil {
						continue
					}
					if fileinfo.IsDir() {
						err = watcher.Add(event.Name)
						if err != nil {
							return
						}
						log.Printf("Subdirectory: '%s' added to watcher.\n", event.Name)
					}
				}

				if event.Op&fsnotify.Write == fsnotify.Write {
					select {
					case fw_channel <- FileWatcherRebuild:
					default:
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error: ", err)
			}
		}
	}()

	run_cmd := exec.Command(run_args[0], run_args[1:]...)
	run_cmd.Dir = root_abs_path
	run_cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// process event and log
	fw_channel <- FileWatcherRebuild
	for {
		select {
		case fw_event := <-fw_channel:
			switch fw_event {
			case FileWatcherRebuild:
				if build_ongoing {
					rebuild_pending = true
					continue
				}
				for {
					rebuild_pending = false
					log.Println("Starting server rebuild.")
					if run_cmd != nil && run_cmd.Process != nil {
						run_cmd.Process.Kill()
						run_cmd.Wait()
					}
					build_cmd := exec.Command(build_args[0], build_args[1:]...)
					build_cmd.Dir = root_abs_path
					build_ongoing = true
					err := build_cmd.Run()
					build_ongoing = false
					if err != nil {
						log.Fatal("Error occured during server build: ", err)
						break
					}

					log.Println("Finished rebuild in Xms.")
					log.Println("Starting server.")

					run_cmd = exec.Command(run_args[0], run_args[1:]...)
					run_cmd.Dir = root_abs_path
					run_cmd.SysProcAttr = &syscall.SysProcAttr{
						Setpgid: true,
					}

					build_ongoing = false
					// TODO: replace X with actual time taken for a rebuild
					if err := run_cmd.Start(); err != nil {
						log.Fatal("Failed to run server: ", err)
					}

					if !rebuild_pending {
						break
					}
				}
			}
		}
	}

}
