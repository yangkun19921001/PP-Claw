package main

import (
	"fmt"
	"os"

	"github.com/yangkun19921001/PP-Claw/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
