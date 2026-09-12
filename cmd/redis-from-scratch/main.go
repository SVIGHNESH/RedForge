// Command redis-from-scratch is the RedForge server entry point.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SVIGHNESH/RedForge/internal/server"
)

func main() {
	showVersion := flag.Bool("version", false, "print the server version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(server.Version)
		return
	}

	fmt.Fprintln(os.Stderr, "redis-from-scratch "+server.Version+": server not implemented yet, see docs/tasks/README.md")
	os.Exit(1)
}
