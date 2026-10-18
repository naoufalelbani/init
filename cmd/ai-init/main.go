package main

import (
	"fmt"
	"github.com/naoufalelbani/init/internal/cli"
	"os"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "ai-init:", err)
		os.Exit(1)
	}
}
