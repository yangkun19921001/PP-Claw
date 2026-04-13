#!/bin/bash
# 检查学习功能状态

echo "🔍 学习功能状态检查"
echo "===================="

# 1. 检查配置
echo "📋 配置状态："
grep -A 3 "review_interval\|min_tool_calls" ~/.pp-claw/pp-claw.yaml

# 2. 检查数据目录
echo ""
echo "📂 数据目录状态："
echo "技能目录: $(find ~/.pp-claw/data/skills -type f | wc -l) 个文件"
echo "轨迹目录: $(find ~/.pp-claw/data/trajectories -type f | wc -l) 个文件"

# 3. 检查最近的学习相关日志
echo ""
echo "📝 最近学习活动："
tail -200 ~/.pp-claw/pp-claw.log | grep -E "(学习任务|轨迹跟踪|ProcessConversation|shouldTriggerLearning)" | tail -5

# 4. 检查轨迹记录
echo ""
echo "📈 轨迹统计："
tail -100 ~/.pp-claw/pp-claw.log | grep "轨迹跟踪完成" | wc -l | awk '{print "完成的会话数: " $1}'

echo ""
echo "💡 建议："
echo "- 如果完成会话数 < 5，继续进行复杂对话"
echo "- 如果 ≥ 5 但无技能文件，可能需要降低阈值"
echo "- 下次测试时尝试: '帮我设计一个 RESTful API，包含用户管理功能'"