// Command redis-from-scratch is the RedForge server entry point.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print the server version and exit")
		bind        = flag.String("bind", "127.0.0.1", "address to bind")
		port        = flag.Int("port", 6380, "port to listen on")
		dir         = flag.String("dir", "./data", "persistence directory")
		appendfsync = flag.String("appendfsync", "everysec", "log fsync policy: always, everysec or no")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(server.Version)
		return
	}

	// The wall clock is the only production clock (MASTER-PLAN 3.4). The
	// store and the dispatcher share it so passive expiry and Ctx.Now agree.
	clock := store.RealClock{}
	policy, err := aof.ParsePolicy(*appendfsync)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	srv := server.New()
	st := store.New(clock)
	command.Install(srv, st, clock)

	// Replay the log before opening it for append, so a torn tail is cut
	// off first. T5.04 moves this into internal/recovery.
	loader := st.(store.Loader)
	loader.SetLoading(true)
	now := clock.NowMs()
	lastSeq, replayed, err := aof.Replay(filepath.Join(*dir, aof.FileName), 0, func(seq uint64, args [][]byte) {
		if err := command.Replay(srv, st, now, args); err != nil {
			fmt.Fprintf(os.Stderr, "replay seq %d: %v\n", seq, err)
		}
	})
	loader.SetLoading(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	srv.SetSeq(lastSeq)
	srv.RegisterInfo("Persistence", replayed.Info)

	aofw, err := aof.Open(*dir, policy, clock.NowMs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer aofw.Close()
	srv.Appenders = append(srv.Appenders, aofw)
	srv.OnTick(aofw.Tick)
	srv.RegisterInfo("Persistence", aofw.Info)
	if err := srv.Listen(*bind, *port); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer srv.Close()

	fmt.Printf("redis-from-scratch %s listening on %s:%d\n", server.Version, *bind, *port)
	if err := srv.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
