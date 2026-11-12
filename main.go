package main

import (
	"os"

	"github.com/Martins6/opencode-jobs/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
