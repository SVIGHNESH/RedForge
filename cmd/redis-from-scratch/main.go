// Command redis-from-scratch is the RedForge server entry point.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print the server version and exit")
		bind        = flag.String("bind", "127.0.0.1", "address to bind")
		port        = flag.Int("port", 6380, "port to listen on")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(server.Version)
		return
	}

	// The wall clock is the only production clock (MASTER-PLAN 3.4). The
	// store and the dispatcher share it so passive expiry and Ctx.Now agree.
	clock := store.RealClock{}
	srv := server.New()
	command.Install(srv, store.New(clock), clock)
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
