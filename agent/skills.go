package agent

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// 内嵌资源 FS (由 main 包通过 SetEmbeddedAssets 注册)
var (
	embeddedSkillsFS    embed.FS
	embeddedTemplatesFS embed.FS
	hasEmbeddedAssets   bool
)

// SetEmbeddedAssets 注册内嵌资源 (在 main 中调用)
func SetEmbeddedAssets(skills, templates embed.FS) {
	embeddedSkillsFS = skills
	embeddedTemplatesFS = templates
	hasEmbeddedAssets = true
}

// 版本信息 (由 main 包 ldflags 注入后通过 SetVersionInfo 设置)
var (
	versionInfo   = "dev"
	commitInfo    = "unknown"
	buildTimeInfo = "unknown"
)

// SetVersionInfo 设置版本信息 (在 main 中调用)
func SetVersionInfo(version, commit, buildTime string) {
	versionInfo = version
	commitInfo = commit
	buildTimeInfo = buildTime
}

// GetVersion 获取版本号
func GetVersion() string { return versionInfo }

// GetCommit 获取 Git commit
func GetCommit() string { return commitInfo }

// GetBuildTime 获取构建时间
func GetBuildTime() string { return buildTimeInfo }

// SkillsLoader 技能加载器 (对标 pp-claw/agent/skills.py:SkillsLoader)
type SkillsLoader struct {
	workspace       string
	workspaceSkills string
	builtinSkills   string // 内置技能目录
}

// NewSkillsLoader 创建技能加载器
func NewSkillsLoader(workspace string) *SkillsLoader {
	// 内置技能目录: 与二进制同目录的 skills/ 或 绝对路径
	execPath, _ := os.Executable()
	builtinDir := filepath.Join(filepath.Dir(execPath), "skills")
	// 开发模式: 检查当前目录
	if _, err := os.Stat(builtinDir); err != nil {
		cwd, _ := os.Getwd()
		builtinDir = filepath.Join(cwd, "skills")
	}

	return &SkillsLoader{
		workspace:       workspace,
		workspaceSkills: filepath.Join(workspace, "skills"),
		builtinSkills:   builtinDir,
	}
}

// SkillInfo 技能信息
type SkillInfo struct {
	Name   string
	Path   string
	Source string // "workspace", "builtin", or "embedded"
}

// ListSkills 列出所有技能 (支持 workspace + builtin + embedded 三级优先级)
func (l *SkillsLoader) ListSkills() []SkillInfo {
	var skills []SkillInfo
	knownNames := make(map[string]bool)

	// Workspace 技能 (最高优先级)
	if entries, err := os.ReadDir(l.workspaceSkills); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillFile := filepath.Join(l.workspaceSkills, e.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				skills = append(skills, SkillInfo{
					Name:   e.Name(),
					Path:   skillFile,
					Source: "workspace",
				})
				knownNames[e.Name()] = true
			}
		}
	}

	// 内置技能 (优先级低于 workspace, 不覆盖同名技能)
	if l.builtinSkills != "" {
		if entries, err := os.ReadDir(l.builtinSkills); err == nil {
			for _, e := range entries {
				if !e.IsDir() || knownNames[e.Name()] {
					continue
				}
				skillFile := filepath.Join(l.builtinSkills, e.Name(), "SKILL.md")
				if _, err := os.Stat(skillFile); err == nil {
					skills = append(skills, SkillInfo{
						Name:   e.Name(),
						Path:   skillFile,
						Source: "builtin",
					})
					knownNames[e.Name()] = true
				}
			}
		}
	}

	// 内嵌技能 (最低优先级 — 仅当 workspace 和 builtin 都没有时使用)
	if hasEmbeddedAssets {
		if entries, err := fs.ReadDir(embeddedSkillsFS, "skills"); err == nil {
			for _, e := range entries {
				if !e.IsDir() || knownNames[e.Name()] {
					continue
				}
				skillPath := "skills/" + e.Name() + "/SKILL.md"
				if _, err := fs.Stat(embeddedSkillsFS, skillPath); err == nil {
					skills = append(skills, SkillInfo{
						Name:   e.Name(),
						Path:   "embedded://skills/" + e.Name() + "/SKILL.md",
						Source: "embedded",
					})
					knownNames[e.Name()] = true
				}
			}
		}
	}

	return skills
}

// LoadSkill 加载技能内容 (支持 workspace + builtin + embedded)
func (l *SkillsLoader) LoadSkill(name string) string {
	// 先检查 workspace
	skillFile := filepath.Join(l.workspaceSkills, name, "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err == nil {
		return string(data)
	}
	// 再检查 builtin
	if l.builtinSkills != "" {
		builtinFile := filepath.Join(l.builtinSkills, name, "SKILL.md")
		data, err = os.ReadFile(builtinFile)
		if err == nil {
			return string(data)
		}
	}
	// 最后检查 embedded
	if hasEmbeddedAssets {
		embeddedPath := "skills/" + name + "/SKILL.md"
		data, err = fs.ReadFile(embeddedSkillsFS, embeddedPath)
		if err == nil {
			return string(data)
		}
	}
	return ""
}

// LoadSkillsForContext 加载指定技能用于上下文注入
func (l *SkillsLoader) LoadSkillsForContext(names []string) string {
	var parts []string
	for _, name := range names {
		content := l.LoadSkill(name)
		if content != "" {
			content = stripFrontmatter(content)
			parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", name, content))
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// BuildSkillsSummary 构建技能摘要 (XML 格式, 对标 skills.py:build_skills_summary)
func (l *SkillsLoader) BuildSkillsSummary() string {
	skills := l.ListSkills()
	if len(skills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "<skills>")
	for _, s := range skills {
		desc := l.getSkillDescription(s.Name)
		meta := l.getSkillMeta(s.Name)
		available := l.checkRequirements(meta)

		lines = append(lines, fmt.Sprintf(`  <skill available="%v">`, available))
		lines = append(lines, fmt.Sprintf("    <name>%s</name>", escapeXML(s.Name)))
		lines = append(lines, fmt.Sprintf("    <description>%s</description>", escapeXML(desc)))
		lines = append(lines, fmt.Sprintf("    <location>%s</location>", s.Path))

		// 显示缺失的依赖 (对标 skills.py:_get_missing_requirements)
		if !available {
			missing := l.getMissingRequirements(meta)
			if missing != "" {
				lines = append(lines, fmt.Sprintf("    <requires>%s</requires>", escapeXML(missing)))
			}
		}

		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</skills>")
	return strings.Join(lines, "\n")
}

// GetAlwaysSkills 获取 always=true 的技能
func (l *SkillsLoader) GetAlwaysSkills() []string {
	var result []string
	for _, s := range l.ListSkills() {
		meta := l.getSkillMetadata(s.Name)
		if meta["always"] == "true" {
			result = append(result, s.Name)
		}
	}
	return result
}

// getSkillDescription 获取技能描述
func (l *SkillsLoader) getSkillDescription(name string) string {
	meta := l.getSkillMetadata(name)
	if desc, ok := meta["description"]; ok && desc != "" {
		return desc
	}
	return name
}

// getSkillMetadata 解析 frontmatter
func (l *SkillsLoader) getSkillMetadata(name string) map[string]string {
	content := l.LoadSkill(name)
	if content == "" || !strings.HasPrefix(content, "---") {
		return nil
	}

	re := regexp.MustCompile(`(?s)^---\n(.*?)\n---`)
	match := re.FindStringSubmatch(content)
	if match == nil {
		return nil
	}

	metadata := make(map[string]string)
	for _, line := range strings.Split(match[1], "\n") {
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, `"'`)
			metadata[key] = value
		}
	}
	return metadata
}

// getSkillMeta 获取 pp-claw 元数据
func (l *SkillsLoader) getSkillMeta(name string) map[string]string {
	return l.getSkillMetadata(name)
}

// getMissingRequirements 获取缺失的依赖描述 (对标 skills.py:_get_missing_requirements)
func (l *SkillsLoader) getMissingRequirements(meta map[string]string) string {
	if meta == nil {
		return ""
	}
	var missing []string
	if bins, ok := meta["requires_bins"]; ok {
		for _, b := range strings.Split(bins, ",") {
			b = strings.TrimSpace(b)
			if b != "" {
				if _, err := exec.LookPath(b); err != nil {
					missing = append(missing, "CLI: "+b)
				}
			}
		}
	}
	if envs, ok := meta["requires_env"]; ok {
		for _, e := range strings.Split(envs, ",") {
			e = strings.TrimSpace(e)
			if e != "" && os.Getenv(e) == "" {
				missing = append(missing, "ENV: "+e)
			}
		}
	}
	return strings.Join(missing, ", ")
}

// checkRequirements 检查技能依赖
func (l *SkillsLoader) checkRequirements(meta map[string]string) bool {
	if meta == nil {
		return true
	}
	// 检查 bins 依赖
	if bins, ok := meta["requires_bins"]; ok {
		for _, b := range strings.Split(bins, ",") {
			b = strings.TrimSpace(b)
			if b != "" {
				if _, err := exec.LookPath(b); err != nil {
					return false
				}
			}
		}
	}
	// 检查环境变量
	if envs, ok := meta["requires_env"]; ok {
		for _, e := range strings.Split(envs, ",") {
			e = strings.TrimSpace(e)
			if e != "" && os.Getenv(e) == "" {
				return false
			}
		}
	}
	return true
}

// ExtractEmbeddedSkills 将内嵌技能释放到目标目录 (用于需要文件系统路径的场景)
func ExtractEmbeddedSkills(targetDir string) error {
	if !hasEmbeddedAssets {
		return nil
	}
	return fs.WalkDir(embeddedSkillsFS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 忽略错误，继续遍历
		}
		// 目标路径: 去掉 "skills/" 前缀后拼接到 targetDir
		rel := strings.TrimPrefix(path, "skills/")
		if rel == "" || rel == "." {
			return nil
		}
		targetPath := filepath.Join(targetDir, rel)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// 如果文件已存在则跳过 (不覆盖用户修改)
		if _, err := os.Stat(targetPath); err == nil {
			return nil
		}

		data, err := fs.ReadFile(embeddedSkillsFS, path)
		if err != nil {
			return nil
		}
		os.MkdirAll(filepath.Dir(targetPath), 0755)
		return os.WriteFile(targetPath, data, 0644)
	})
}

// stripFrontmatter 移除 YAML frontmatter
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	re := regexp.MustCompile(`(?s)^---\n.*?\n---\n`)
	return strings.TrimSpace(re.ReplaceAllString(content, ""))
}

// escapeXML 转义 XML 特殊字符
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
