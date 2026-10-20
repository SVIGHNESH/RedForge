// Command redis-from-scratch is the RedForge server entry point.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/recovery"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/snapshot"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print the server version and exit")
		bind        = flag.String("bind", "127.0.0.1", "address to bind")
		port        = flag.Int("port", 6380, "port to listen on")
		dir         = flag.String("dir", "./data", "persistence directory")
		appendfsync = flag.String("appendfsync", "everysec", "log fsync policy: always, everysec or no")
		bgsaveDelay = flag.Int("debug-bgsave-delay-ms", 0, "test hook: delay before the snapshot rename; never use in the evaluation")
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

	// Recover before the log is opened for append, so a torn tail is cut
	// off first, and before listening, so no client sees a half-built
	// keyspace (MASTER-PLAN 4.9).
	recovered, err := recovery.Run(*dir, st, srv, clock)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	srv.RegisterInfo("Persistence", recovered.Log.Info)

	aofw, err := aof.Open(*dir, policy, clock.NowMs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer aofw.Close()
	srv.Appenders = append(srv.Appenders, aofw)
	srv.OnTick(aofw.Tick)
	srv.RegisterInfo("Persistence", aofw.Info)

	saver, err := snapshot.NewSaver(srv, st, *dir, time.Duration(*bgsaveDelay)*time.Millisecond, clock.NowMs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer saver.Close()
	srv.SetBackgroundSaver(saver)
	srv.RegisterInfo("Persistence", saver.Info)

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
