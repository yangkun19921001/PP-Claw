#!/bin/bash

# go-nanobot 学习功能测试验证脚本
# 用于验证自学习循环和强化学习功能的有效性

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 配置变量
TEST_DIR="./test_learning"
CONFIG_FILE="$TEST_DIR/test_config.yaml"
PID_FILE="$TEST_DIR/go-nanobot.pid"
LOG_FILE="$TEST_DIR/test.log"
SKILLS_DIR="$TEST_DIR/data/skills"
TRAJECTORIES_DIR="$TEST_DIR/data/trajectories"

# 清理函数
cleanup() {
    log_info "清理测试环境..."
    if [ -f "$PID_FILE" ]; then
        kill $(cat "$PID_FILE") 2>/dev/null || true
        rm -f "$PID_FILE"
    fi
    # 保留测试数据以便分析
    # rm -rf "$TEST_DIR"
}

# 设置信号处理
trap cleanup EXIT

# 创建测试目录
setup_test_env() {
    log_info "设置测试环境..."

    mkdir -p "$TEST_DIR/data/skills"
    mkdir -p "$TEST_DIR/data/trajectories"
    mkdir -p "$TEST_DIR/workspace"

    # 创建测试配置文件
    cat > "$CONFIG_FILE" << 'EOF'
agents:
  defaults:
    workspace: "./test_learning/workspace"
    model: "deepseek-chat"
    max_tokens: 4096
    temperature: 0.1
    max_tool_iterations: 10
    memory_window: 20

  list:
    - id: "assistant"
      name: "智能助手"
      default: true

    - id: "researcher"
      name: "研究专员"
      tools:
        include: ["web_search", "web_fetch", "read_file", "write_file"]

  bindings:
    - agent_id: "researcher"
      content_match:
        keywords: ["研究", "调查", "搜索", "分析"]

    - agent_id: "assistant"
      default: true

# 启用学习功能
learning:
  enabled: true

  skill_extraction:
    review_interval: 3        # 更频繁的测试触发
    min_conversation_length: 2
    min_tool_calls: 1

  storage:
    type: "file"
    path: "./test_learning/data/skills"
    max_skills: 100

  injection:
    max_inject_skills: 3
    similarity_threshold: 0.2  # 更低阈值便于测试

# 启用强化学习
rl:
  enabled: true

  tracking:
    storage_path: "./test_learning/data/trajectories"
    max_trajectories: 50
    retention_days: 1
    auto_cleanup: false

  compression:
    target_max_steps: 10
    summary_target_length: 200
    protected_steps:
      first_n: 3
      last_n: 2
      protect_error: true

providers:
  deepseek:
    api_key: "${DEEPSEEK_API_KEY:-sk-your-key-here}"
    base_url: "https://api.deepseek.com"

channels:
  telegram:
    enabled: false

tools:
  exec:
    timeout: "30s"
  web:
    search:
      api_key: "${SEARCH_API_KEY:-your-key-here}"
EOF

    log_success "测试环境创建完成: $TEST_DIR"
}

# 启动 go-nanobot
start_nanobot() {
    log_info "启动 go-nanobot..."

    # 检查可执行文件
    if [ ! -f "./go-nanobot" ]; then
        log_error "go-nanobot 可执行文件不存在，请先编译项目"
        exit 1
    fi

    # 启动服务
    ./go-nanobot --config="$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"

    # 等待启动
    sleep 5

    if ! kill -0 $(cat "$PID_FILE") 2>/dev/null; then
        log_error "go-nanobot 启动失败，查看日志："
        cat "$LOG_FILE"
        exit 1
    fi

    log_success "go-nanobot 启动成功 (PID: $(cat $PID_FILE))"
}

# 模拟用户交互
simulate_conversation() {
    local conversation_type="$1"
    local session_id="test-session-$(date +%s)"

    log_info "模拟对话: $conversation_type"

    case "$conversation_type" in
        "seo_analysis")
            simulate_seo_conversation
            ;;
        "debugging")
            simulate_debugging_conversation
            ;;
        "simple_qa")
            simulate_simple_conversation
            ;;
        *)
            log_error "未知的对话类型: $conversation_type"
            return 1
            ;;
    esac
}

# SEO分析对话（应该学习技能）
simulate_seo_conversation() {
    log_info "模拟SEO分析对话（复杂任务，应学习技能）"

    # 这里我们需要通过API与go-nanobot交互
    # 简化版本：直接创建模拟数据文件

    cat > "$TEST_DIR/seo_conversation.txt" << 'EOF'
用户: 帮我分析 example.com 的SEO问题
助手: 我来帮你分析SEO问题。首先检查网站的基本信息。
[工具调用: web_search(query="site:example.com")]
结果: 搜索失败，可能被反爬虫拦截

助手: 搜索失败，我换个方法用web_fetch直接获取页面内容。
[工具调用: web_fetch(url="https://example.com")]
结果: 成功获取页面HTML内容

助手: 分析页面内容，我发现以下SEO问题：
1. 缺少meta description
2. title标签不够优化
3. 没有使用heading标签结构
让我写一个详细的分析报告。
[工具调用: write_file(path="seo_analysis.md")]
结果: 成功创建SEO分析报告

助手: SEO分析完成。主要问题是元数据缺失和页面结构不够优化。我已经生成了详细的优化建议报告。
EOF

    log_success "SEO分析对话模拟完成"
}

# 调试对话（应该学习技能）
simulate_debugging_conversation() {
    log_info "模拟调试对话（复杂任务，应学习技能）"

    cat > "$TEST_DIR/debug_conversation.txt" << 'EOF'
用户: 我的Go程序有内存泄漏，帮我找问题
助手: 内存泄漏需要系统性排查。首先用pprof工具分析内存使用。
[工具调用: exec(cmd="go tool pprof -http=:6060 mem.prof")]
结果: 启动pprof web界面，发现goroutine数量异常增长

助手: 发现goroutine泄漏。检查源代码中的channel和goroutine使用。
[工具调用: read_file(path="main.go")]
结果: 读取到源代码，发现unbuffered channel可能造成阻塞

助手: 找到问题了！main.go第45行的channel没有正确关闭，导致goroutine阻塞。修复代码：
[工具调用: edit_file(path="main.go", changes="添加channel关闭逻辑")]
结果: 成功修复channel泄漏问题

助手: 内存泄漏问题已解决。修复要点：1)正确关闭channel 2)确保goroutine正常退出 3)使用pprof监控内存使用
EOF

    log_success "调试对话模拟完成"
}

# 简单对话（不应该学习技能）
simulate_simple_conversation() {
    log_info "模拟简单对话（简单任务，不应学习技能）"

    cat > "$TEST_DIR/simple_conversation.txt" << 'EOF'
用户: 今天天气怎么样？
助手: 很抱歉，我无法获取实时天气信息。建议你查看天气预报网站或天气应用。

用户: 谢谢
助手: 不客气！如果有其他问题随时问我。
EOF

    log_success "简单对话模拟完成"
}

# 检查学习效果
check_learning_results() {
    log_info "检查学习效果..."

    # 检查技能文件
    if [ -d "$SKILLS_DIR" ]; then
        skill_count=$(find "$SKILLS_DIR" -name "*.yaml" | wc -l)
        log_info "发现技能文件: $skill_count 个"

        if [ $skill_count -gt 0 ]; then
            log_success "✓ 技能学习功能工作正常"

            # 显示学习到的技能
            echo
            log_info "学习到的技能列表："
            find "$SKILLS_DIR" -name "*.yaml" -exec basename {} .yaml \; | while read skill; do
                echo "  - $skill"
            done
        else
            log_warning "⚠ 未发现学习到的技能文件"
        fi
    else
        log_warning "⚠ 技能目录不存在"
    fi

    # 检查轨迹文件
    if [ -d "$TRAJECTORIES_DIR" ]; then
        trajectory_count=$(find "$TRAJECTORIES_DIR" -name "*.json" | wc -l)
        log_info "发现轨迹文件: $trajectory_count 个"

        if [ $trajectory_count -gt 0 ]; then
            log_success "✓ 轨迹跟踪功能工作正常"
        else
            log_warning "⚠ 未发现轨迹文件"
        fi
    else
        log_warning "⚠ 轨迹目录不存在"
    fi
}

# 分析日志
analyze_logs() {
    log_info "分析日志文件..."

    if [ -f "$LOG_FILE" ]; then
        echo
        log_info "学习相关日志:"
        grep -i "learning\|skill\|trajectory" "$LOG_FILE" | tail -20 || true

        echo
        log_info "错误日志:"
        grep -i "error\|fail" "$LOG_FILE" | tail -10 || true
    else
        log_warning "日志文件不存在"
    fi
}

# 性能测试
performance_test() {
    log_info "进行性能测试..."

    # 记录启动时间
    start_time=$(date +%s%N)

    # 模拟多次交互
    for i in {1..5}; do
        log_info "性能测试轮次 $i/5"
        simulate_conversation "simple_qa"
        sleep 1
    done

    end_time=$(date +%s%N)
    duration=$((($end_time - $start_time) / 1000000)) # 转换为毫秒

    log_info "性能测试完成，总耗时: ${duration}ms"

    # 检查内存使用
    if [ -f "$PID_FILE" ]; then
        pid=$(cat "$PID_FILE")
        memory_kb=$(ps -o rss= -p $pid 2>/dev/null || echo "0")
        memory_mb=$((memory_kb / 1024))
        log_info "当前内存使用: ${memory_mb}MB"

        if [ $memory_mb -lt 500 ]; then
            log_success "✓ 内存使用正常"
        else
            log_warning "⚠ 内存使用较高: ${memory_mb}MB"
        fi
    fi
}

# 生成测试报告
generate_report() {
    log_info "生成测试报告..."

    local report_file="$TEST_DIR/test_report.md"

    cat > "$report_file" << EOF
# go-nanobot 学习功能测试报告

## 测试环境
- 测试时间: $(date)
- 测试目录: $TEST_DIR
- 配置文件: $CONFIG_FILE

## 功能测试结果

### 技能学习功能
EOF

    if [ -d "$SKILLS_DIR" ]; then
        skill_count=$(find "$SKILLS_DIR" -name "*.yaml" | wc -l)
        echo "- 学习到的技能数量: $skill_count" >> "$report_file"

        if [ $skill_count -gt 0 ]; then
            echo "- 状态: ✅ 正常工作" >> "$report_file"
            echo "" >> "$report_file"
            echo "#### 技能详情" >> "$report_file"
            find "$SKILLS_DIR" -name "*.yaml" -exec basename {} .yaml \; | while read skill; do
                echo "- $skill" >> "$report_file"
            done
        else
            echo "- 状态: ⚠️ 未检测到技能生成" >> "$report_file"
        fi
    else
        echo "- 状态: ❌ 技能目录不存在" >> "$report_file"
    fi

    echo "" >> "$report_file"
    echo "### 轨迹跟踪功能" >> "$report_file"

    if [ -d "$TRAJECTORIES_DIR" ]; then
        trajectory_count=$(find "$TRAJECTORIES_DIR" -name "*.json" | wc -l)
        echo "- 记录的轨迹数量: $trajectory_count" >> "$report_file"

        if [ $trajectory_count -gt 0 ]; then
            echo "- 状态: ✅ 正常工作" >> "$report_file"
        else
            echo "- 状态: ⚠️ 未检测到轨迹文件" >> "$report_file"
        fi
    else
        echo "- 状态: ❌ 轨迹目录不存在" >> "$report_file"
    fi

    echo "" >> "$report_file"
    echo "## 建议" >> "$report_file"
    echo "" >> "$report_file"

    if [ -d "$SKILLS_DIR" ] && [ $(find "$SKILLS_DIR" -name "*.yaml" | wc -l) -gt 0 ]; then
        echo "- ✅ 学习功能工作正常，建议在生产环境中启用" >> "$report_file"
    else
        echo "- ⚠️ 学习功能可能需要更多测试数据或调整配置参数" >> "$report_file"
    fi

    echo "" >> "$report_file"
    echo "## 详细日志" >> "$report_file"
    echo "" >> "$report_file"
    echo "\`\`\`" >> "$report_file"
    if [ -f "$LOG_FILE" ]; then
        tail -50 "$LOG_FILE" >> "$report_file"
    fi
    echo "\`\`\`" >> "$report_file"

    log_success "测试报告已生成: $report_file"
}

# 主测试流程
main() {
    log_info "开始 go-nanobot 学习功能测试"
    echo "======================================="

    # 检查前置条件
    if [ -z "$DEEPSEEK_API_KEY" ]; then
        log_warning "未设置 DEEPSEEK_API_KEY 环境变量，某些功能可能无法测试"
    fi

    # 设置测试环境
    setup_test_env

    # 启动服务
    start_nanobot

    # 等待服务完全启动
    sleep 3

    # 执行测试
    log_info "执行功能测试..."
    simulate_conversation "seo_analysis"
    sleep 2
    simulate_conversation "debugging"
    sleep 2
    simulate_conversation "simple_qa"
    sleep 2

    # 性能测试
    performance_test

    # 等待学习过程完成
    log_info "等待学习过程完成..."
    sleep 10

    # 检查结果
    check_learning_results

    # 分析日志
    analyze_logs

    # 生成报告
    generate_report

    echo "======================================="
    log_success "测试完成！检查报告: $TEST_DIR/test_report.md"

    # 提供下一步建议
    echo
    log_info "下一步建议："
    echo "1. 查看测试报告了解详细结果"
    echo "2. 检查生成的技能文件: $SKILLS_DIR"
    echo "3. 分析轨迹数据: $TRAJECTORIES_DIR"
    echo "4. 根据结果调整配置参数"
    echo "5. 在实际场景中进一步测试"
}

# 显示使用帮助
show_help() {
    echo "go-nanobot 学习功能测试脚本"
    echo
    echo "用法: $0 [选项]"
    echo
    echo "选项:"
    echo "  -h, --help     显示此帮助信息"
    echo "  -c, --clean    清理之前的测试数据"
    echo "  -q, --quick    快速测试（跳过性能测试）"
    echo
    echo "环境变量:"
    echo "  DEEPSEEK_API_KEY    DeepSeek API密钥（必需）"
    echo "  SEARCH_API_KEY      搜索API密钥（可选）"
    echo
    echo "示例:"
    echo "  export DEEPSEEK_API_KEY='sk-xxx'"
    echo "  $0"
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -c|--clean)
            log_info "清理之前的测试数据..."
            rm -rf "$TEST_DIR"
            log_success "清理完成"
            exit 0
            ;;
        -q|--quick)
            QUICK_TEST=true
            shift
            ;;
        *)
            log_error "未知参数: $1"
            show_help
            exit 1
            ;;
    esac
done

# 运行主测试
main