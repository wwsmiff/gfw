package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// file watcher operation enum
type FileWatcherEvent = uint8

const (
	FileWatcherRebuild FileWatcherEvent = iota
	FileWathcerDebounce
)

func main() {
	//ignored := []string{".git"}
	fw_channel := make(chan FileWatcherEvent, 1)

	signal_channel := make(chan os.Signal, 1)
	signal.Notify(signal_channel, syscall.SIGINT, syscall.SIGTERM)

	root := flag.String("root", "", "Root directory for starting hot reload server.")
	build_args_str := flag.String("build", "", "Command for building the server.")
	run_args_str := flag.String("exec", "", "Command for running the server.")

	flag.Parse()

	root_abs_path, err := filepath.Abs(*root)
	build_args := strings.Split(*build_args_str, " ")
	run_args := strings.Split(*run_args_str, " ")

	build_ongoing := false
	rebuild_pending := false

	debounce_delay := 200 * time.Millisecond
	debounce_timer := time.NewTimer(debounce_delay)
	watched := map[string]struct{}{}

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

				if filepath.Base(event.Name) == "testserver" {
					continue
				}

				if event.Op&fsnotify.Create == fsnotify.Create {
					fileinfo, err := os.Stat(event.Name)
					if err != nil {
						continue
					}
					if fileinfo.IsDir() {
						err := watcher.Add(event.Name)
						if err != nil {
							return
						}
						watched[event.Name] = struct{}{}
						log.Printf("Subdirectory: '%s' added to watcher.\n", event.Name)
					}
				}

				if event.Op&fsnotify.Remove == fsnotify.Remove {
					_, ok := watched[event.Name]
					if ok {
						watcher.Remove(event.Name)
						log.Printf("Subdirectory: '%s' removed from watcher.\n", event.Name)
					}
				}

				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
					if !debounce_timer.Stop() {
						select {
						case <-debounce_timer.C:
						default:
						}
					}
					debounce_timer.Reset(debounce_delay)
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
		case <-debounce_timer.C:
			fw_channel <- FileWatcherRebuild
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
						syscall.Kill(-run_cmd.Process.Pid, syscall.SIGTERM)
						time.Sleep(1 * time.Second)
						syscall.Kill(-run_cmd.Process.Pid, syscall.SIGKILL)
						run_cmd.Wait()
					}
					build_cmd := exec.Command(build_args[0], build_args[1:]...)
					build_cmd.Dir = root_abs_path
					build_ongoing = true
					start := time.Now()
					err := build_cmd.Run()
					elapsed := time.Since(start)
					build_ongoing = false
					if err != nil {
						log.Fatal("Error occured during server build: ", err)
						break
					}

					log.Printf("Finished rebuild in %d ms.", elapsed.Milliseconds())
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
						continue
					}

					if !rebuild_pending {
						break
					}
				}
			}
		case <-signal_channel:
			log.Println("Shutting down.")
			if run_cmd != nil && run_cmd.Process != nil {
				syscall.Kill(-run_cmd.Process.Pid, syscall.SIGTERM)
				time.Sleep(1 * time.Second)
				syscall.Kill(-run_cmd.Process.Pid, syscall.SIGKILL)
				run_cmd.Wait()
			}
			os.Exit(0)
		}
	}
}
