// The standalone development client shares the binary's loadtest command.
package main

import (
	"github.com/HashNuke/morsecode-audio/internal/loadtest"
	"os"
)

func main() { os.Exit(loadtest.Run(os.Args[1:], os.Stdout, os.Stderr)) }
