#!/bin/bash
# pp-claw 学习功能监控脚本

echo "🧠 pp-claw 学习功能实时监控"
echo "================================"

while true; do
    clear
    echo "🧠 pp-claw 学习功能实时监控 - $(date)"
    echo "================================"

    # 技能统计
    echo "💡 已学习的技能："
    SKILL_COUNT=0
    for agent_dir in ~/.pp-claw/data/skills/*/; do
        if [ -d "$agent_dir" ]; then
            agent_name=$(basename "$agent_dir")
            agent_skills=$(find "$agent_dir" -name "*.yaml" 2>/dev/null | wc -l)
            SKILL_COUNT=$((SKILL_COUNT + agent_skills))
            if [ "$agent_skills" -gt 0 ]; then
                echo "  ├─ $agent_name: $agent_skills 个技能"
                # 显示最新的技能
                newest_skill=$(find "$agent_dir" -name "*.yaml" -type f -exec ls -t {} + 2>/dev/null | head -1)
                if [ -n "$newest_skill" ]; then
                    skill_name=$(basename "$newest_skill" .yaml)
                    echo "     └─ 最新: $skill_name"
                fi
            fi
        fi
    done

    if [ "$SKILL_COUNT" -eq 0 ]; then
        echo "  └─ 暂无技能 (需要进行复杂对话触发学习)"
    fi

    echo ""

    # 轨迹统计
    echo "📈 执行轨迹记录："
    TRAJECTORY_COUNT=0
    for agent_dir in ~/.pp-claw/data/trajectories/*/; do
        if [ -d "$agent_dir" ]; then
            agent_name=$(basename "$agent_dir")
            agent_trajectories=$(find "$agent_dir" -name "*.json" 2>/dev/null | wc -l)
            TRAJECTORY_COUNT=$((TRAJECTORY_COUNT + agent_trajectories))
            if [ "$agent_trajectories" -gt 0 ]; then
                echo "  ├─ $agent_name: $agent_trajectories 条轨迹"
            fi
        fi
    done

    if [ "$TRAJECTORY_COUNT" -eq 0 ]; then
        echo "  └─ 暂无轨迹记录"
    fi

    echo ""
    echo "📊 总计: $SKILL_COUNT 个技能, $TRAJECTORY_COUNT 条轨迹"

    # 最近的学习活动
    echo ""
    echo "🔥 最近学习活动 (最近5分钟)："
    recent_skills=$(find ~/.pp-claw/data/skills -name "*.yaml" -mmin -5 2>/dev/null)
    recent_trajectories=$(find ~/.pp-claw/data/trajectories -name "*.json" -mmin -5 2>/dev/null)

    if [ -n "$recent_skills" ]; then
        echo "$recent_skills" | while read skill; do
            if [ -n "$skill" ]; then
                skill_name=$(basename "$skill" .yaml)
                agent_name=$(basename $(dirname "$skill"))
                echo "  ✨ [$agent_name] 学会了: $skill_name"
            fi
        done
    fi

    if [ -n "$recent_trajectories" ]; then
        echo "$recent_trajectories" | while read trajectory; do
            if [ -n "$trajectory" ]; then
                trajectory_name=$(basename "$trajectory" .json)
                agent_name=$(basename $(dirname "$trajectory"))
                echo "  📝 [$agent_name] 记录轨迹: $trajectory_name"
            fi
        done
    fi

    if [ -z "$recent_skills" ] && [ -z "$recent_trajectories" ]; then
        echo "  └─ 无最近活动 (尝试与Agent进行复杂对话)"
    fi

    echo ""
    echo "按 Ctrl+C 退出监控"
    echo "================================"

    sleep 5
done