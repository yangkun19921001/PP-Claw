package main

import (
	"fmt"
	"os"

	"github.com/yangkun19921001/go-nanobot/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
