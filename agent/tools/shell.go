package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// ExecTool Shell 命令执行 (对标 pp-claw/agent/tools/shell.py:ExecTool)
type ExecTool struct {
	WorkingDir          string
	Timeout             int // 秒
	RestrictToWorkspace bool
}

func (t *ExecTool) Name() string        { return "execute" }
func (t *ExecTool) Description() string { return "Execute a shell command and return its output." }
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
		},
		"required": []any{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	command, _ := params["command"].(string)
	if command == "" {
		return "", fmt.Errorf("ERROR: 'command' parameter is required and must be a non-empty string. " +
			"Please provide the shell command to execute, e.g. {\"command\": \"python3 /tmp/script.py\"}. " +
			"Do NOT retry with empty parameters.")
	}

	// 安全检查
	if err := guardCommand(command, t.WorkingDir, t.RestrictToWorkspace); err != nil {
		return "", fmt.Errorf("command blocked for safety: %s", err.Error())
	}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 60
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = t.WorkingDir

	// 创建新进程组，超时时 kill 整个进程组（包括 sh 的子进程如 du、find 等）
	// 否则 exec.CommandContext 只 kill sh，子进程变孤儿，pipe 不关闭导致 cmd.Run() 永久挂起
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// 负 PID = kill 整个进程组
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	// 安全网：即使进程组 kill 后 pipe 仍未关闭，5 秒后强制关闭 pipe
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var result strings.Builder
	if stdout.Len() > 0 {
		result.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("STDERR: " + stderr.String())
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %d seconds", timeout)
		}
		if result.Len() > 0 {
			return fmt.Sprintf("Error (exit code non-zero):\n%s", result.String()), nil
		}
		return "", fmt.Errorf("command failed: %w", err)
	}

	if result.Len() == 0 {
		return "(no output)", nil
	}

	// 截断过长输出
	output := result.String()
	const maxLen = 50000
	if len(output) > maxLen {
		output = output[:maxLen] + "\n... (output truncated)"
	}

	return output, nil
}

// denyPatterns 正则安全检查模式列表 (对齐 Python pp-claw)
var denyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+-[rf]{1,2}\b`),
	regexp.MustCompile(`(?i)\bdel\s+/[fq]\b`),
	regexp.MustCompile(`(?i)\brmdir\s+/s\b`),
	regexp.MustCompile(`(?i)(?:^|[;&|]\s*)format\b`),
	regexp.MustCompile(`(?i)\b(mkfs|diskpart)\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=`),
	regexp.MustCompile(`(?i)>\s*/dev/sd`),
	regexp.MustCompile(`(?i)\b(shutdown|reboot|poweroff)\b`),
	regexp.MustCompile(`:\(\)\s*\{.*\};\s*:`),
}

// guardCommand 安全检查命令 (对标 shell.py:guard_command)
func guardCommand(command, cwd string, restrictToWorkspace bool) error {
	// 1. 正则 deny 模式检查
	for _, pattern := range denyPatterns {
		if pattern.MatchString(command) {
			return fmt.Errorf("potentially destructive operation detected")
		}
	}

	// 2. 路径遍历检测（在 RestrictToWorkspace 模式下）
	if restrictToWorkspace && cwd != "" {
		// 检查 ../ 路径遍历
		if strings.Contains(command, "../") {
			return fmt.Errorf("path traversal (../) not allowed in workspace-restricted mode")
		}

		// 提取命令中的绝对路径并校验是否在 workspace 内
		absPaths := extractAbsolutePaths(command)
		cwdAbs, err := filepath.Abs(cwd)
		if err == nil {
			for _, p := range absPaths {
				pAbs, err := filepath.Abs(p)
				if err != nil {
					continue
				}
				if !strings.HasPrefix(pAbs, cwdAbs) {
					return fmt.Errorf("path %q is outside workspace %q", p, cwd)
				}
			}
		}
	}

	return nil
}

// absolutePathRe 匹配命令中的绝对路径
var absolutePathRe = regexp.MustCompile(`(?:^|\s)(/[^\s;|&>]+)`)

// extractAbsolutePaths 从命令字符串中提取绝对路径
func extractAbsolutePaths(command string) []string {
	matches := absolutePathRe.FindAllStringSubmatch(command, -1)
	var paths []string
	for _, m := range matches {
		if len(m) > 1 {
			p := m[1]
			// 排除常见的非路径模式
			if p == "/dev/null" || p == "/dev/stdin" || p == "/dev/stdout" || p == "/dev/stderr" {
				continue
			}
			paths = append(paths, p)
		}
	}
	return paths
}
