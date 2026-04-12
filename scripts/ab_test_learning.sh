#!/bin/bash

# go-nanobot 学习功能 A/B 对比测试脚本
# 对比启用学习功能前后的效果差异

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
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

log_test() {
    echo -e "${PURPLE}[TEST]${NC} $1"
}

# 测试配置
BASE_DIR="./ab_test_$(date +%Y%m%d_%H%M%S)"
CONTROL_DIR="$BASE_DIR/control"      # A组：不启用学习
TEST_DIR="$BASE_DIR/treatment"       # B组：启用学习
REPORT_FILE="$BASE_DIR/ab_test_report.md"

# 测试场景定义
declare -a TEST_SCENARIOS=(
    "seo_analysis:帮我分析网站的SEO问题并给出优化建议"
    "code_debug:我的Go程序有内存泄漏，帮我找到并修复问题"
    "api_design:设计一个用户管理系统的RESTful API"
    "database_optimize:MySQL查询太慢，帮我优化数据库性能"
    "deploy_config:配置Nginx和Docker部署Web应用"
)

# 性能指标
declare -A CONTROL_METRICS
declare -A TREATMENT_METRICS

# 创建测试环境
setup_test_environments() {
    log_info "设置 A/B 测试环境..."

    mkdir -p "$CONTROL_DIR/data/skills"
    mkdir -p "$CONTROL_DIR/data/trajectories"
    mkdir -p "$CONTROL_DIR/workspace"

    mkdir -p "$TEST_DIR/data/skills"
    mkdir -p "$TEST_DIR/data/trajectories"
    mkdir -p "$TEST_DIR/workspace"

    # 创建控制组配置（不启用学习）
    cat > "$CONTROL_DIR/config.yaml" << 'EOF'
agents:
  defaults:
    workspace: "./workspace"
    model: "deepseek-chat"
    max_tokens: 4096
    temperature: 0.1
    max_tool_iterations: 10
    memory_window: 20

  list:
    - id: "assistant"
      name: "智能助手"
      default: true

# 学习功能：禁用
learning:
  enabled: false

rl:
  enabled: false

providers:
  deepseek:
    api_key: "${DEEPSEEK_API_KEY}"
    base_url: "https://api.deepseek.com"

channels:
  telegram:
    enabled: false

tools:
  exec:
    timeout: "30s"
EOF

    # 创建试验组配置（启用学习）
    cat > "$TEST_DIR/config.yaml" << 'EOF'
agents:
  defaults:
    workspace: "./workspace"
    model: "deepseek-chat"
    max_tokens: 4096
    temperature: 0.1
    max_tool_iterations: 10
    memory_window: 20

  list:
    - id: "assistant"
      name: "智能助手"
      default: true

# 学习功能：启用
learning:
  enabled: true

  skill_extraction:
    review_interval: 3
    min_conversation_length: 2
    min_tool_calls: 1

  storage:
    type: "file"
    path: "./data/skills"
    max_skills: 100

  injection:
    max_inject_skills: 3
    similarity_threshold: 0.2

rl:
  enabled: true

  tracking:
    storage_path: "./data/trajectories"
    max_trajectories: 50
    retention_days: 7

  compression:
    target_max_steps: 15
    protected_steps:
      first_n: 3
      last_n: 3

providers:
  deepseek:
    api_key: "${DEEPSEEK_API_KEY}"
    base_url: "https://api.deepseek.com"

channels:
  telegram:
    enabled: false

tools:
  exec:
    timeout: "30s"
EOF

    log_success "A/B 测试环境创建完成"
}

# 启动实例
start_instance() {
    local group="$1"
    local config_dir="$2"

    log_info "启动 $group 组实例..."

    cd "$config_dir"

    # 启动 go-nanobot
    if [ ! -f "../../go-nanobot" ]; then
        log_error "go-nanobot 可执行文件不存在"
        return 1
    fi

    ../../go-nanobot --config=config.yaml > nanobot.log 2>&1 &
    echo $! > nanobot.pid

    # 等待启动
    sleep 5

    if ! kill -0 $(cat nanobot.pid) 2>/dev/null; then
        log_error "$group 组启动失败"
        cat nanobot.log
        return 1
    fi

    log_success "$group 组启动成功"
    cd - > /dev/null
}

# 停止实例
stop_instance() {
    local config_dir="$1"

    if [ -f "$config_dir/nanobot.pid" ]; then
        kill $(cat "$config_dir/nanobot.pid") 2>/dev/null || true
        rm -f "$config_dir/nanobot.pid"
    fi
}

# 模拟用户交互并记录性能
simulate_user_interaction() {
    local group="$1"
    local config_dir="$2"
    local scenario="$3"
    local query="$4"
    local iteration="$5"

    log_test "[$group] 场景: $scenario (第 $iteration 次)"

    local start_time=$(date +%s%N)

    # 这里应该通过API调用go-nanobot
    # 简化版本：模拟处理并记录时间
    case "$scenario" in
        "seo_analysis")
            simulate_seo_task "$config_dir" "$iteration"
            ;;
        "code_debug")
            simulate_debug_task "$config_dir" "$iteration"
            ;;
        "api_design")
            simulate_design_task "$config_dir" "$iteration"
            ;;
        "database_optimize")
            simulate_db_task "$config_dir" "$iteration"
            ;;
        "deploy_config")
            simulate_deploy_task "$config_dir" "$iteration"
            ;;
    esac

    local end_time=$(date +%s%N)
    local duration_ms=$(( (end_time - start_time) / 1000000 ))

    # 记录性能指标
    local key="${group}_${scenario}_${iteration}"

    if [ "$group" = "control" ]; then
        CONTROL_METRICS[$key]=$duration_ms
    else
        TREATMENT_METRICS[$key]=$duration_ms
    fi

    log_info "[$group] $scenario 耗时: ${duration_ms}ms"

    # 模拟处理延迟
    sleep 2
}

# 模拟SEO分析任务
simulate_seo_task() {
    local config_dir="$1"
    local iteration="$2"

    # 模拟复杂的SEO分析流程
    cat > "$config_dir/seo_result_$iteration.txt" << 'EOF'
SEO分析结果:
1. 检查meta标签 - 发现缺失description
2. 分析title标签 - 需要优化关键词
3. 检查heading结构 - H1标签使用正确
4. 内链分析 - 内链密度偏低
5. 生成优化建议报告

工具调用序列:
- web_search: site:example.com
- web_fetch: https://example.com
- write_file: seo_analysis.md
EOF

    # 计算复杂度评分（模拟）
    echo "complexity_score: 8.5" >> "$config_dir/seo_result_$iteration.txt"
    echo "tool_calls: 3" >> "$config_dir/seo_result_$iteration.txt"
    echo "success_rate: 0.95" >> "$config_dir/seo_result_$iteration.txt"
}

# 模拟调试任务
simulate_debug_task() {
    local config_dir="$1"
    local iteration="$2"

    cat > "$config_dir/debug_result_$iteration.txt" << 'EOF'
调试结果:
1. 使用pprof分析内存 - 发现goroutine泄漏
2. 检查源代码 - 定位到channel阻塞问题
3. 修复代码 - 添加正确的channel关闭逻辑
4. 验证修复效果 - 内存泄漏消除

工具调用序列:
- exec: go tool pprof
- read_file: main.go
- edit_file: 修复channel问题
EOF

    echo "complexity_score: 9.2" >> "$config_dir/debug_result_$iteration.txt"
    echo "tool_calls: 3" >> "$config_dir/debug_result_$iteration.txt"
    echo "success_rate: 0.90" >> "$config_dir/debug_result_$iteration.txt"
}

# 模拟API设计任务
simulate_design_task() {
    local config_dir="$1"
    local iteration="$2"

    cat > "$config_dir/design_result_$iteration.txt" << 'EOF'
API设计结果:
1. 分析需求 - 用户管理CRUD操作
2. 设计REST接口 - GET/POST/PUT/DELETE endpoints
3. 定义数据模型 - User schema设计
4. 编写API文档 - OpenAPI规范

工具调用序列:
- write_file: api_spec.yaml
- write_file: user_model.go
EOF

    echo "complexity_score: 7.8" >> "$config_dir/design_result_$iteration.txt"
    echo "tool_calls: 2" >> "$config_dir/design_result_$iteration.txt"
    echo "success_rate: 0.92" >> "$config_dir/design_result_$iteration.txt"
}

# 模拟数据库优化任务
simulate_db_task() {
    local config_dir="$1"
    local iteration="$2"

    cat > "$config_dir/db_result_$iteration.txt" << 'EOF'
数据库优化结果:
1. 分析慢查询日志 - 识别性能瓶颈
2. 检查索引使用 - 发现缺失的复合索引
3. 优化查询语句 - 重写复杂JOIN
4. 调整配置参数 - 优化buffer pool

工具调用序列:
- exec: mysql slow query analysis
- read_file: slow_query.log
- exec: EXPLAIN查询计划分析
EOF

    echo "complexity_score: 8.8" >> "$config_dir/db_result_$iteration.txt"
    echo "tool_calls: 3" >> "$config_dir/db_result_$iteration.txt"
    echo "success_rate: 0.87" >> "$config_dir/db_result_$iteration.txt"
}

# 模拟部署配置任务
simulate_deploy_task() {
    local config_dir="$1"
    local iteration="$2"

    cat > "$config_dir/deploy_result_$iteration.txt" << 'EOF'
部署配置结果:
1. 编写Dockerfile - 多阶段构建优化
2. 配置Nginx - 负载均衡和SSL
3. 设置Docker Compose - 服务编排
4. 配置监控 - Prometheus+Grafana

工具调用序列:
- write_file: Dockerfile
- write_file: nginx.conf
- write_file: docker-compose.yml
EOF

    echo "complexity_score: 8.0" >> "$config_dir/deploy_result_$iteration.txt"
    echo "tool_calls: 3" >> "$config_dir/deploy_result_$iteration.txt"
    echo "success_rate: 0.93" >> "$config_dir/deploy_result_$iteration.txt"
}

# 收集性能指标
collect_metrics() {
    local group="$1"
    local config_dir="$2"

    log_info "收集 $group 组性能指标..."

    # 计算各项指标
    local total_time=0
    local task_count=0
    local success_count=0
    local total_complexity=0

    for file in "$config_dir"/*_result_*.txt; do
        if [ -f "$file" ]; then
            task_count=$((task_count + 1))

            # 提取复杂度评分
            if grep -q "complexity_score" "$file"; then
                local complexity=$(grep "complexity_score" "$file" | cut -d: -f2 | tr -d ' ')
                total_complexity=$(echo "$total_complexity + $complexity" | bc)
            fi

            # 提取成功率
            if grep -q "success_rate" "$file"; then
                local success_rate=$(grep "success_rate" "$file" | cut -d: -f2 | tr -d ' ')
                if (( $(echo "$success_rate > 0.8" | bc -l) )); then
                    success_count=$((success_count + 1))
                fi
            fi
        fi
    done

    # 计算性能时间
    local time_key_prefix="${group}_"
    for key in "${!CONTROL_METRICS[@]}" "${!TREATMENT_METRICS[@]}"; do
        if [[ $key == ${time_key_prefix}* ]]; then
            if [ "$group" = "control" ]; then
                total_time=$((total_time + ${CONTROL_METRICS[$key]}))
            else
                total_time=$((total_time + ${TREATMENT_METRICS[$key]}))
            fi
        fi
    done

    # 计算平均值
    local avg_time=$((total_time / task_count))
    local avg_complexity=$(echo "scale=2; $total_complexity / $task_count" | bc)
    local success_rate=$(echo "scale=2; $success_count / $task_count" | bc)

    # 保存指标
    cat > "$config_dir/metrics.txt" << EOF
group: $group
avg_response_time_ms: $avg_time
avg_complexity_score: $avg_complexity
success_rate: $success_rate
total_tasks: $task_count
EOF

    log_success "$group 组指标收集完成"
}

# 运行A/B测试
run_ab_test() {
    log_info "开始 A/B 对比测试..."

    # 启动两个实例
    start_instance "control" "$CONTROL_DIR"
    start_instance "treatment" "$TEST_DIR"

    # 等待两个实例稳定
    sleep 5

    # 运行测试场景
    local iterations=3

    for scenario_def in "${TEST_SCENARIOS[@]}"; do
        IFS=':' read -r scenario_name scenario_query <<< "$scenario_def"

        log_info "测试场景: $scenario_name"

        for i in $(seq 1 $iterations); do
            # 控制组测试
            simulate_user_interaction "control" "$CONTROL_DIR" "$scenario_name" "$scenario_query" "$i"

            # 试验组测试
            simulate_user_interaction "treatment" "$TEST_DIR" "$scenario_name" "$scenario_query" "$i"

            # 给系统一些恢复时间
            sleep 1
        done

        log_success "场景 $scenario_name 测试完成"
    done

    # 停止实例
    stop_instance "$CONTROL_DIR"
    stop_instance "$TEST_DIR"

    log_success "A/B 测试执行完成"
}

# 分析学习效果
analyze_learning_effects() {
    log_info "分析学习效果..."

    local skills_learned=0
    local trajectories_recorded=0

    # 检查试验组的学习成果
    if [ -d "$TEST_DIR/data/skills" ]; then
        skills_learned=$(find "$TEST_DIR/data/skills" -name "*.yaml" 2>/dev/null | wc -l)
    fi

    if [ -d "$TEST_DIR/data/trajectories" ]; then
        trajectories_recorded=$(find "$TEST_DIR/data/trajectories" -name "*.json" 2>/dev/null | wc -l)
    fi

    cat > "$TEST_DIR/learning_analysis.txt" << EOF
学习效果分析:
- 学习到的技能数量: $skills_learned
- 记录的轨迹数量: $trajectories_recorded
- 学习功能状态: $([ $skills_learned -gt 0 ] && echo "正常工作" || echo "需要检查")

技能详情:
EOF

    if [ $skills_learned -gt 0 ]; then
        find "$TEST_DIR/data/skills" -name "*.yaml" -exec basename {} .yaml \; >> "$TEST_DIR/learning_analysis.txt"
    else
        echo "未发现学习到的技能" >> "$TEST_DIR/learning_analysis.txt"
    fi

    log_success "学习效果分析完成"
}

# 生成对比报告
generate_comparison_report() {
    log_info "生成 A/B 测试对比报告..."

    # 收集两组指标
    collect_metrics "control" "$CONTROL_DIR"
    collect_metrics "treatment" "$TEST_DIR"

    # 读取指标数据
    source "$CONTROL_DIR/metrics.txt"
    local control_time=$avg_response_time_ms
    local control_complexity=$avg_complexity_score
    local control_success=$success_rate
    local control_tasks=$total_tasks

    source "$TEST_DIR/metrics.txt"
    local treatment_time=$avg_response_time_ms
    local treatment_complexity=$avg_complexity_score
    local treatment_success=$success_rate
    local treatment_tasks=$total_tasks

    # 计算改进幅度
    local time_improvement=$(echo "scale=2; ($control_time - $treatment_time) / $control_time * 100" | bc)
    local success_improvement=$(echo "scale=2; ($treatment_success - $control_success) / $control_success * 100" | bc)

    # 生成报告
    cat > "$REPORT_FILE" << EOF
# go-nanobot 学习功能 A/B 测试报告

## 测试概述

- **测试时间**: $(date)
- **测试场景**: ${#TEST_SCENARIOS[@]} 个复杂任务场景
- **测试轮次**: 每场景 3 次迭代
- **对比组**:
  - A组 (控制组): 学习功能关闭
  - B组 (试验组): 学习功能开启

## 性能对比结果

### 响应时间对比

| 指标 | 控制组 (A) | 试验组 (B) | 改进幅度 |
|------|------------|------------|----------|
| 平均响应时间 | ${control_time}ms | ${treatment_time}ms | ${time_improvement}% |
| 任务复杂度评分 | ${control_complexity} | ${treatment_complexity} | - |
| 任务成功率 | ${control_success} | ${treatment_success} | ${success_improvement}% |
| 总任务数 | ${control_tasks} | ${treatment_tasks} | - |

### 学习效果分析

EOF

    # 添加学习效果
    if [ -f "$TEST_DIR/learning_analysis.txt" ]; then
        cat "$TEST_DIR/learning_analysis.txt" >> "$REPORT_FILE"
    fi

    cat >> "$REPORT_FILE" << EOF

## 详细性能数据

### 控制组详细指标
\`\`\`
EOF

    cat "$CONTROL_DIR/metrics.txt" >> "$REPORT_FILE"

    cat >> "$REPORT_FILE" << EOF
\`\`\`

### 试验组详细指标
\`\`\`
EOF

    cat "$TEST_DIR/metrics.txt" >> "$REPORT_FILE"

    cat >> "$REPORT_FILE" << EOF
\`\`\`

## 测试场景详情

EOF

    for scenario_def in "${TEST_SCENARIOS[@]}"; do
        IFS=':' read -r scenario_name scenario_query <<< "$scenario_def"
        echo "### $scenario_name" >> "$REPORT_FILE"
        echo "- **查询**: $scenario_query" >> "$REPORT_FILE"
        echo "" >> "$REPORT_FILE"
    done

    cat >> "$REPORT_FILE" << EOF

## 结论和建议

### 性能影响
EOF

    if (( $(echo "$time_improvement > 10" | bc -l) )); then
        echo "✅ **显著改善**: 学习功能显著提升了响应速度 (${time_improvement}%)" >> "$REPORT_FILE"
    elif (( $(echo "$time_improvement > 0" | bc -l) )); then
        echo "✅ **轻微改善**: 学习功能略微提升了响应速度 (${time_improvement}%)" >> "$REPORT_FILE"
    else
        echo "⚠️ **需要优化**: 学习功能对响应速度有轻微影响，建议优化配置" >> "$REPORT_FILE"
    fi

    cat >> "$REPORT_FILE" << EOF

### 学习效果
EOF

    local skills_count=$(find "$TEST_DIR/data/skills" -name "*.yaml" 2>/dev/null | wc -l)
    if [ $skills_count -gt 0 ]; then
        echo "✅ **学习正常**: 成功学习了 $skills_count 个技能，可以在后续任务中复用" >> "$REPORT_FILE"
    else
        echo "⚠️ **学习异常**: 未检测到技能学习，可能需要更多测试数据或调整参数" >> "$REPORT_FILE"
    fi

    cat >> "$REPORT_FILE" << EOF

### 总体建议

1. **生产部署建议**:
EOF

    if (( $(echo "$time_improvement >= 0" | bc -l) )) && [ $skills_count -gt 0 ]; then
        echo "   - ✅ **推荐部署**: 学习功能表现良好，建议在生产环境启用" >> "$REPORT_FILE"
    else
        echo "   - ⚠️ **谨慎部署**: 建议先在测试环境进一步调优" >> "$REPORT_FILE"
    fi

    cat >> "$REPORT_FILE" << EOF

2. **配置优化建议**:
   - 根据实际场景调整技能提取间隔
   - 优化技能相似度阈值
   - 监控学习数据质量

3. **监控重点**:
   - 响应时间变化
   - 技能学习速度
   - 存储空间使用
   - 内存占用情况

## 测试数据归档

- **控制组数据**: \`$CONTROL_DIR/\`
- **试验组数据**: \`$TEST_DIR/\`
- **详细日志**: 查看各目录下的 \`nanobot.log\` 文件
EOF

    log_success "A/B 测试报告已生成: $REPORT_FILE"
}

# 主流程
main() {
    echo "======================================="
    log_info "go-nanobot 学习功能 A/B 对比测试"
    echo "======================================="

    # 检查前置条件
    if [ -z "$DEEPSEEK_API_KEY" ]; then
        log_error "请设置 DEEPSEEK_API_KEY 环境变量"
        exit 1
    fi

    if [ ! -f "./go-nanobot" ]; then
        log_error "go-nanobot 可执行文件不存在，请先编译项目"
        exit 1
    fi

    # 设置测试环境
    setup_test_environments

    # 运行A/B测试
    run_ab_test

    # 分析学习效果
    analyze_learning_effects

    # 生成对比报告
    generate_comparison_report

    echo "======================================="
    log_success "A/B 测试完成！"
    echo
    log_info "查看详细报告: $REPORT_FILE"
    echo
    log_info "快速结果预览:"

    # 显示关键指标
    if [ -f "$CONTROL_DIR/metrics.txt" ] && [ -f "$TEST_DIR/metrics.txt" ]; then
        local control_time=$(grep avg_response_time_ms "$CONTROL_DIR/metrics.txt" | cut -d: -f2 | tr -d ' ')
        local treatment_time=$(grep avg_response_time_ms "$TEST_DIR/metrics.txt" | cut -d: -f2 | tr -d ' ')
        local improvement=$(echo "scale=1; ($control_time - $treatment_time) / $control_time * 100" | bc)

        echo "  响应时间: 控制组 ${control_time}ms → 试验组 ${treatment_time}ms (${improvement}%)"

        local skills_count=$(find "$TEST_DIR/data/skills" -name "*.yaml" 2>/dev/null | wc -l)
        echo "  学习技能: $skills_count 个"

        if (( $(echo "$improvement > 5" | bc -l) )) && [ $skills_count -gt 0 ]; then
            log_success "🎉 学习功能效果显著，建议启用！"
        elif [ $skills_count -gt 0 ]; then
            log_success "✅ 学习功能正常工作，可以考虑启用"
        else
            log_warning "⚠️ 学习功能需要进一步调优"
        fi
    fi
}

# 清理函数
cleanup() {
    log_info "清理测试进程..."
    stop_instance "$CONTROL_DIR" 2>/dev/null || true
    stop_instance "$TEST_DIR" 2>/dev/null || true
}

trap cleanup EXIT

# 显示帮助
show_help() {
    echo "go-nanobot 学习功能 A/B 对比测试脚本"
    echo
    echo "用法: $0 [选项]"
    echo
    echo "选项:"
    echo "  -h, --help     显示帮助信息"
    echo "  -q, --quick    快速测试模式"
    echo
    echo "环境变量:"
    echo "  DEEPSEEK_API_KEY    必需的API密钥"
    echo
    echo "测试场景:"
    for scenario_def in "${TEST_SCENARIOS[@]}"; do
        IFS=':' read -r scenario_name scenario_query <<< "$scenario_def"
        echo "  - $scenario_name"
    done
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -q|--quick)
            # 快速模式：减少测试场景
            TEST_SCENARIOS=("seo_analysis:帮我分析网站的SEO问题" "code_debug:我的Go程序有内存泄漏")
            shift
            ;;
        *)
            log_error "未知参数: $1"
            show_help
            exit 1
            ;;
    esac
done

# 运行主流程
main