package main

import "embed"

// 版本信息 (通过 -ldflags 注入)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// EmbeddedSkillsFS 内嵌技能资源
// 嵌入所有 SKILL.md、shell 脚本和 JS 脚本，排除 node_modules/
//
//go:embed skills/*/SKILL.md
//go:embed skills/*/*.sh
//go:embed skills/*/scripts/*.js
//go:embed skills/*/scripts/*.sh
//go:embed skills/*/scripts/package.json
var EmbeddedSkillsFS embed.FS

// EmbeddedTemplatesFS 内嵌模板资源
// 嵌入 templates/ 下所有 .md 文件
//
//go:embed templates/*.md templates/memory/*.md
var EmbeddedTemplatesFS embed.FS
