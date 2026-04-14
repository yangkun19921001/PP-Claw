package learning

import (
	"reflect"
	"testing"
)

func TestParseSkillResponseSupportsChineseColonAndMarkdown(t *testing.T) {
	extractor := &DefaultSkillExtractor{}

	response := "```markdown\n**技能名称：** 日志排查\n**描述：** 快速定位 Go 服务中的学习模块解析异常\n**标签：** go，debug\n**内容：**\n1. 先看错误栈定位到具体文件和行号\n2. 再检查解析器是否过度依赖固定格式\n```"

	skill, err := extractor.parseSkillResponse(response)
	if err != nil {
		t.Fatalf("parseSkillResponse returned error: %v", err)
	}
	if skill == nil {
		t.Fatal("expected skill, got nil")
	}
	if skill.Name != "日志排查" {
		t.Fatalf("unexpected skill name: %q", skill.Name)
	}
	if skill.Description != "快速定位 Go 服务中的学习模块解析异常" {
		t.Fatalf("unexpected description: %q", skill.Description)
	}
	if got, want := skill.Tags, []string{"go", "debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tags: got %v want %v", got, want)
	}
	if skill.Content != "1. 先看错误栈定位到具体文件和行号\n2. 再检查解析器是否过度依赖固定格式" {
		t.Fatalf("unexpected content: %q", skill.Content)
	}
}

func TestParseSkillResponseSupportsSectionHeadings(t *testing.T) {
	extractor := &DefaultSkillExtractor{}

	response := "## 技能名称\n批量重命名文件\n## 描述\n按固定规则批量重命名文件并校验结果\n## 标签\nshell, files\n## 内容\n1. 先预览匹配范围\n2. 再执行重命名\n3. 最后抽样检查"

	skill, err := extractor.parseSkillResponse(response)
	if err != nil {
		t.Fatalf("parseSkillResponse returned error: %v", err)
	}
	if skill == nil {
		t.Fatal("expected skill, got nil")
	}
	if skill.Name != "批量重命名文件" {
		t.Fatalf("unexpected skill name: %q", skill.Name)
	}
	if got, want := skill.Tags, []string{"shell", "files"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tags: got %v want %v", got, want)
	}
	if skill.Content != "1. 先预览匹配范围\n2. 再执行重命名\n3. 最后抽样检查" {
		t.Fatalf("unexpected content: %q", skill.Content)
	}
}

func TestParseSkillResponseSupportsJSONAliases(t *testing.T) {
	extractor := &DefaultSkillExtractor{}

	response := "```json\n{\"技能名称\":\"日志排查\",\"描述\":\"定位学习模块中的提取异常\",\"内容\":\"1. 查看日志\\n2. 校验解析分支\",\"标签\":[\"go\",\"learning\"]}\n```"

	skill, err := extractor.parseSkillResponse(response)
	if err != nil {
		t.Fatalf("parseSkillResponse returned error: %v", err)
	}
	if skill == nil {
		t.Fatal("expected skill, got nil")
	}
	if skill.Name != "日志排查" {
		t.Fatalf("unexpected skill name: %q", skill.Name)
	}
	if skill.Description != "定位学习模块中的提取异常" {
		t.Fatalf("unexpected description: %q", skill.Description)
	}
	if got, want := skill.Tags, []string{"go", "learning"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tags: got %v want %v", got, want)
	}
}

func TestParseSkillResponseReturnsNilForNoSkillResponses(t *testing.T) {
	extractor := &DefaultSkillExtractor{}

	response := "这段对话主要是基础确认和常规执行，未发现值得保存的可复用技能。"

	skill, err := extractor.parseSkillResponse(response)
	if err != nil {
		t.Fatalf("parseSkillResponse returned error: %v", err)
	}
	if skill != nil {
		t.Fatalf("expected nil skill, got %+v", skill)
	}
}

func TestParseSkillResponseSupportsSkillAlias(t *testing.T) {
	extractor := &DefaultSkillExtractor{}

	response := "技能：日志排查\n描述：定位学习模块中的提取异常\n标签：go, debug\n内容：\n1. 查看日志\n2. 校验解析分支"

	skill, err := extractor.parseSkillResponse(response)
	if err != nil {
		t.Fatalf("parseSkillResponse returned error: %v", err)
	}
	if skill == nil {
		t.Fatal("expected skill, got nil")
	}
	if skill.Name != "日志排查" {
		t.Fatalf("unexpected skill name: %q", skill.Name)
	}
	if skill.Content != "1. 查看日志\n2. 校验解析分支" {
		t.Fatalf("unexpected content: %q", skill.Content)
	}
}

func TestParseSkillResponseReturnsNilForIncompleteSkillDefinition(t *testing.T) {
	extractor := &DefaultSkillExtractor{}

	response := "描述：定位学习模块中的提取异常\n标签：go, debug\n内容：\n1. 查看日志\n2. 校验解析分支"

	skill, err := extractor.parseSkillResponse(response)
	if err != nil {
		t.Fatalf("parseSkillResponse returned error: %v", err)
	}
	if skill != nil {
		t.Fatalf("expected nil skill, got %+v", skill)
	}
}

func TestNewDefaultSkillExtractorUsesCustomReviewPrompt(t *testing.T) {
	extractor := NewDefaultSkillExtractor(nil, nil, &SkillExtractionConfig{
		ReviewPrompt: "custom prompt",
	})

	if extractor.reviewPrompt != "custom prompt" {
		t.Fatalf("unexpected review prompt: %q", extractor.reviewPrompt)
	}
}
