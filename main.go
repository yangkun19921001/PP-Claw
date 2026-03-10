package main

import (
	"fmt"
	"os"

	"github.com/yangkun19921001/PP-Claw/agent"
	"github.com/yangkun19921001/PP-Claw/cli"
)

func main() {
	// 注册内嵌资源 (skills + templates)
	agent.SetEmbeddedAssets(EmbeddedSkillsFS, EmbeddedTemplatesFS)
	agent.SetVersionInfo(Version, Commit, BuildTime)

	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
