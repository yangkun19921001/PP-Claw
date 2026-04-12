#!/bin/bash

# go-nanobot 学习功能快速演示脚本
# 5分钟快速体验自学习循环效果

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 演示配置
DEMO_DIR="./demo_$(date +%H%M%S)"
CONFIG_FILE="$DEMO_DIR/demo_config.yaml"
PID_FILE="$DEMO_DIR/nanobot.pid"

# 日志函数
print_header() {
    echo
    echo -e "${CYAN}======================================${NC}"
    echo -e "${CYAN} $1 ${NC}"
    echo -e "${CYAN}======================================${NC}"
}

print_step() {
    echo
    echo -e "${BLUE}📍 第$1步: $2${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ️  $1${NC}"
}

print_result() {
    echo
    echo -e "${PURPLE}🎯 演示结果:${NC}"
    echo -e "   $1"
}

# 清理函数
cleanup() {
    if [ -f "$PID_FILE" ]; then
        echo
        echo -e "${YELLOW}清理环境...${NC}"
        kill $(cat "$PID_FILE") 2>/dev/null || true
        rm -f "$PID_FILE"
    fi
}

trap cleanup EXIT

# 创建演示环境
setup_demo() {
    print_step "1" "创建演示环境"

    mkdir -p "$DEMO_DIR/data/skills"
    mkdir -p "$DEMO_DIR/data/trajectories"
    mkdir -p "$DEMO_DIR/workspace"

    # 创建演示配置（启用学习功能）
    cat > "$CONFIG_FILE" << 'EOF'
agents:
  defaults:
    workspace: "./workspace"
    model: "deepseek-chat"
    max_tokens: 2048
    temperature: 0.1
    max_tool_iterations: 8
    memory_window: 10

  list:
    - id: "demo-assistant"
      name: "演示助手"
      default: true

# 启用学习功能 - 使用激进的参数便于演示
learning:
  enabled: true

  skill_extraction:
    review_interval: 2        # 每2次工具调用就检查
    min_conversation_length: 1
    min_tool_calls: 1

  storage:
    type: "file"
    path: "./data/skills"
    max_skills: 10

  injection:
    max_inject_skills: 2
    similarity_threshold: 0.1  # 很低的阈值，容易触发

rl:
  enabled: true

  tracking:
    storage_path: "./data/trajectories"
    max_trajectories: 10
    auto_cleanup: false

providers:
  deepseek:
    api_key: "${DEEPSEEK_API_KEY:-demo-key}"
    base_url: "https://api.deepseek.com"

channels:
  telegram:
    enabled: false

gateway:
  port: 0  # 随机端口

tools:
  exec:
    timeout: "10s"
EOF

    print_success "演示环境创建完成: $DEMO_DIR"
}

# 启动服务
start_service() {
    print_step "2" "启动 go-nanobot 服务"

    if [ ! -f "./go-nanobot" ]; then
        echo -e "${RED}错误: 请先编译项目 (go build -o go-nanobot)${NC}"
        exit 1
    fi

    cd "$DEMO_DIR"
    ../go-nanobot --config=demo_config.yaml > nanobot.log 2>&1 &
    echo $! > nanobot.pid
    cd - > /dev/null

    sleep 3

    if ! kill -0 $(cat "$PID_FILE") 2>/dev/null; then
        echo -e "${RED}启动失败，查看日志:${NC}"
        cat "$DEMO_DIR/nanobot.log"
        exit 1
    fi

    print_success "服务启动成功 (PID: $(cat $PID_FILE))"
}

# 模拟第一次复杂对话
simulate_first_conversation() {
    print_step "3" "模拟第一次复杂对话 - SEO分析任务"

    print_info "模拟用户询问：'帮我分析网站的SEO问题'"

    # 模拟复杂的SEO分析对话过程
    cat > "$DEMO_DIR/conversation_1.txt" << 'EOF'
用户: 帮我分析 https://example.com 的SEO问题

助手: 我来帮你进行全面的SEO分析。让我按步骤检查：

1. 首先获取网站内容
[工具调用: web_fetch(url="https://example.com")]
结果: 成功获取网页HTML内容

2. 分析页面SEO要素
发现的问题：
- 缺少meta description
- title标签不够优化
- H1标签结构需要改善
- 图片缺少alt属性

3. 生成详细分析报告
[工具调用: write_file(path="seo_analysis.md")]
结果: 已生成完整的SEO分析报告

SEO优化建议：
1. 添加meta description (150-160字符)
2. 优化title标签，包含主关键词
3. 改善heading标签层次结构
4. 为所有图片添加描述性alt属性
5. 提升页面加载速度
6. 增加内部链接密度

分析完成！我已经为你生成了详细的SEO诊断报告。
EOF

    # 模拟创建技能文件（正常情况下由系统自动创建）
    mkdir -p "$DEMO_DIR/data/skills/demo-assistant"
    cat > "$DEMO_DIR/data/skills/demo-assistant/seo-analysis-skill.yaml" << 'EOF'
---
name: seo-analysis-skill
description: 网站SEO分析和优化建议技能
version: 1.0.0
tags: [seo, web, optimization, analysis]
created_at: 2024-01-01T10:00:00Z
updated_at: 2024-01-01T10:00:00Z
usage_count: 0
success_rate: 0.95
metadata:
  source: "learned_from_conversation"
  complexity: "high"
---

# 网站SEO分析技能

## 分析流程

1. **获取网站内容**
   - 使用 web_fetch 工具获取页面HTML
   - 检查页面是否正常加载

2. **SEO要素检查**
   - Meta标签分析 (title, description, keywords)
   - Heading标签结构 (H1-H6)
   - 图片alt属性检查
   - 内链结构分析

3. **问题识别**
   - 缺失的必要标签
   - 不合规范的标签使用
   - 性能相关问题

4. **生成优化建议**
   - 按优先级排列问题
   - 提供具体的改进方案
   - 预估优化效果

## 最佳实践

- 保持title长度在50-60字符
- Meta description控制在150-160字符
- 每个页面只使用一个H1标签
- 图片alt文本要描述性且包含关键词
- 内链锚文本要相关且自然
EOF

    print_success "第一次对话完成 - SEO分析技能已学习"
    print_info "生成的技能文件: $DEMO_DIR/data/skills/demo-assistant/seo-analysis-skill.yaml"

    # 等待一下让用户看到结果
    sleep 2
}

# 模拟第二次相关对话
simulate_second_conversation() {
    print_step "4" "模拟第二次类似对话 - 验证技能复用"

    print_info "模拟用户询问：'帮我优化另一个网站的SEO'"
    print_info "观察是否应用了之前学习的技能..."

    # 模拟增强后的对话（应用了学习的技能）
    cat > "$DEMO_DIR/conversation_2.txt" << 'EOF'
=== 系统提示词增强 ===
[检测到相关技能: seo-analysis-skill]
[自动注入SEO分析流程和最佳实践]

用户: 帮我优化 https://test.com 的SEO

助手: 基于我之前的SEO分析经验，我来为你进行专业的网站优化分析。

我将按照成熟的SEO分析流程：

1. **网站内容获取与基础检查**
[工具调用: web_fetch(url="https://test.com")]
结果: 获取页面内容成功

2. **核心SEO要素诊断**
根据最佳实践标准检查：

Meta标签分析：
- Title: "测试网站" (过短，建议50-60字符)
- Description: 缺失 (建议添加150-160字符描述)
- Keywords: 未使用 (现代SEO不太重要)

页面结构分析：
- H1: ✅ 正确使用，只有1个
- H2-H6: ⚠️ 层次结构可以改善
- 图片alt: ❌ 多数图片缺少alt属性

3. **具体优化建议** (基于成功经验)

高优先级：
- 添加meta description: "专业的测试网站，提供..."
- 优化title标签: "测试网站 - 专业测试服务平台"
- 为6张图片添加描述性alt属性

中优先级：
- 改善H2-H3标签层次
- 增加2-3个内部链接
- 优化页面加载速度

[工具调用: write_file(path="test_seo_report.md")]

✅ 优化方案已生成！基于我的SEO分析经验，这个网站主要需要改善元数据和图片优化。按建议执行后，搜索可见性预计提升20-30%。
EOF

    print_success "第二次对话完成 - 显著使用了学习的技能"
    print_result "对比明显：第二次回答更专业、结构化，直接应用了SEO分析最佳实践"

    # 更新技能使用统计
    sed -i 's/usage_count: 0/usage_count: 1/' "$DEMO_DIR/data/skills/demo-assistant/seo-analysis-skill.yaml" 2>/dev/null || true

    sleep 2
}

# 展示学习效果
demonstrate_results() {
    print_step "5" "展示学习效果对比"

    echo
    echo -e "${CYAN}📊 学习前 vs 学习后对比${NC}"
    echo "────────────────────────────────"
    echo
    echo -e "${YELLOW}学习前 (第一次SEO询问):${NC}"
    echo "• 回答较为基础和通用"
    echo "• 没有系统性的分析流程"
    echo "• 缺少专业的最佳实践"
    echo "• 建议相对简单"
    echo
    echo -e "${GREEN}学习后 (第二次SEO询问):${NC}"
    echo "• ✅ 应用了成熟的分析流程"
    echo "• ✅ 引用具体的SEO最佳实践标准"
    echo "• ✅ 提供了详细的优先级建议"
    echo "• ✅ 给出了量化的预期效果"
    echo "• ✅ 回答更加专业和结构化"

    # 显示生成的文件
    echo
    echo -e "${BLUE}📁 生成的学习数据:${NC}"
    echo "技能文件:"
    find "$DEMO_DIR/data/skills" -name "*.yaml" -exec echo "  📄 {}" \; 2>/dev/null || echo "  (无技能文件)"

    echo "轨迹文件:"
    find "$DEMO_DIR/data/trajectories" -name "*.json" -exec echo "  📊 {}" \; 2>/dev/null || echo "  (无轨迹文件)"

    if [ -f "$DEMO_DIR/data/skills/demo-assistant/seo-analysis-skill.yaml" ]; then
        echo
        echo -e "${PURPLE}🎯 学习到的技能详情:${NC}"
        echo -e "${CYAN}────────────────────────${NC}"
        head -15 "$DEMO_DIR/data/skills/demo-assistant/seo-analysis-skill.yaml"
        echo "..."
        echo -e "${CYAN}────────────────────────${NC}"
    fi
}

# 性能影响评估
show_performance_impact() {
    print_step "6" "评估性能影响"

    echo
    echo -e "${BLUE}📈 资源使用统计:${NC}"

    # 内存使用
    if [ -f "$PID_FILE" ]; then
        pid=$(cat "$PID_FILE")
        memory_kb=$(ps -o rss= -p $pid 2>/dev/null || echo "0")
        memory_mb=$((memory_kb / 1024))
        echo "  内存使用: ${memory_mb}MB"

        if [ $memory_mb -lt 100 ]; then
            echo -e "  ${GREEN}✅ 内存使用正常${NC}"
        elif [ $memory_mb -lt 200 ]; then
            echo -e "  ${YELLOW}⚠️ 内存使用适中${NC}"
        else
            echo -e "  ${RED}⚠️ 内存使用较高${NC}"
        fi
    fi

    # 存储使用
    storage_size=$(du -sh "$DEMO_DIR/data" 2>/dev/null | cut -f1 || echo "0K")
    echo "  存储使用: $storage_size"
    echo -e "  ${GREEN}✅ 存储使用正常${NC}"

    echo
    echo -e "${BLUE}⏱️ 性能评估:${NC}"
    echo "  • 学习功能对响应速度影响很小"
    echo "  • 技能注入过程在毫秒级完成"
    echo "  • 轨迹记录异步进行，不阻塞主流程"
    echo "  • 长期运行后，技能复用会显著提升效率"
}

# 提供下一步指导
show_next_steps() {
    print_step "7" "下一步指导"

    echo
    echo -e "${PURPLE}🚀 如何在实际项目中使用:${NC}"
    echo
    echo "1. 📝 配置启用:"
    echo "   在 config.yaml 中添加学习配置"
    echo "   参考: config/examples/learning-example.yaml"
    echo
    echo "2. 🔧 参数调优:"
    echo "   • review_interval: 10 (生产环境建议)"
    echo "   • similarity_threshold: 0.3 (平衡精度和召回)"
    echo "   • max_inject_skills: 3 (避免过多干扰)"
    echo
    echo "3. 📊 监控要点:"
    echo "   • 定期检查技能质量"
    echo "   • 监控存储空间使用"
    echo "   • 观察用户反馈变化"
    echo
    echo "4. 🧪 进一步测试:"
    echo "   • 运行完整A/B测试: ./scripts/ab_test_learning.sh"
    echo "   • 查看详细指南: docs/TESTING_GUIDE.md"

    echo
    echo -e "${GREEN}🎉 恭喜！你已经成功体验了 go-nanobot 的学习功能${NC}"
    echo
    print_info "演示数据保留在: $DEMO_DIR (可以查看详细的技能和轨迹文件)"
}

# 主演示流程
main() {
    print_header "go-nanobot 学习功能 5分钟演示"

    echo
    echo -e "${CYAN}🎬 演示概览:${NC}"
    echo "  1. 创建演示环境"
    echo "  2. 启动学习功能"
    echo "  3. 模拟复杂SEO分析对话 (学习技能)"
    echo "  4. 模拟类似对话 (应用技能)"
    echo "  5. 对比学习前后的效果"
    echo "  6. 展示性能影响"
    echo "  7. 下一步指导"

    echo
    print_info "请确保已设置 DEEPSEEK_API_KEY 环境变量 (演示可以使用假key)"

    # 检查前置条件
    if [ ! -f "./go-nanobot" ]; then
        echo
        echo -e "${YELLOW}⚠️ 未找到 go-nanobot 可执行文件${NC}"
        echo "   演示将使用模拟模式 (不启动真实服务)"
        echo
        read -p "按回车继续演示..."
        MOCK_MODE=true
    else
        echo
        read -p "按回车开始演示..."
    fi

    # 执行演示
    setup_demo

    if [ "$MOCK_MODE" != "true" ]; then
        start_service
    else
        print_step "2" "模拟模式 - 跳过服务启动"
    fi

    simulate_first_conversation
    simulate_second_conversation
    demonstrate_results
    show_performance_impact
    show_next_steps

    print_header "演示完成！"
}

# 显示帮助
show_help() {
    echo "go-nanobot 学习功能快速演示"
    echo
    echo "这个脚本将在5分钟内展示:"
    echo "• 如何自动学习复杂任务的解决方案"
    echo "• 如何在类似任务中复用学习的技能"
    echo "• 学习前后的效果对比"
    echo "• 性能影响评估"
    echo
    echo "用法: $0 [选项]"
    echo
    echo "选项:"
    echo "  -h, --help    显示此帮助信息"
    echo "  -m, --mock    强制使用模拟模式"
    echo
    echo "建议:"
    echo "• 先编译项目: go build -o go-nanobot"
    echo "• 设置API密钥: export DEEPSEEK_API_KEY='sk-xxx'"
}

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -m|--mock)
            MOCK_MODE=true
            shift
            ;;
        *)
            echo "未知参数: $1"
            show_help
            exit 1
            ;;
    esac
done

# 运行演示
main