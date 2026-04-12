# go-nanobot 学习功能测试和验证指南

本指南提供完整的测试方案，帮助你验证自学习循环和强化学习功能的有效性。

## 🎯 **测试目标**

验证以下关键功能：
1. **技能自动提取**：从复杂对话中学习技能
2. **技能智能注入**：在相关场景中复用技能
3. **轨迹跟踪记录**：完整记录执行过程
4. **轨迹智能压缩**：长对话的有效压缩
5. **性能和稳定性**：确保不影响现有功能

## 📋 **测试前准备**

### **环境要求**

```bash
# 检查系统环境
go version  # 需要 Go 1.21+
df -h       # 确保有足够磁盘空间 (>2GB)
free -h     # 确保有足够内存 (>2GB)

# 设置环境变量
export DEEPSEEK_API_KEY="sk-your-key-here"
export SEARCH_API_KEY="your-search-key"  # 可选
```

### **编译项目**

```bash
# 确保项目可以正常编译
go mod tidy
go build -o go-nanobot

# 验证可执行文件
./go-nanobot --help
```

### **配置测试权限**

```bash
# 为测试脚本添加执行权限
chmod +x scripts/test_learning.sh
chmod +x scripts/ab_test_learning.sh
```

## 🧪 **测试方案**

### **方案 1：基础功能测试**

验证各个组件是否正常工作：

```bash
# 运行基础功能测试
./scripts/test_learning.sh

# 检查测试结果
cat test_learning/test_report.md
```

**验证要点**：
- [ ] 服务正常启动，无错误日志
- [ ] 技能存储目录创建成功
- [ ] 轨迹存储目录创建成功
- [ ] 配置文件正确加载

### **方案 2：学习效果验证**

通过实际对话验证学习功能：

#### **2.1 复杂任务对话测试**

```bash
# 启动 go-nanobot
./go-nanobot --config=config/examples/learning-example.yaml

# 在另一个终端中进行复杂对话
# 示例：SEO 分析任务
```

**测试对话示例**：

```
用户: 帮我分析 https://example.com 的SEO问题

助手: 我来帮你分析这个网站的SEO问题。首先让我检查网站的基本信息。

[系统会调用 web_search 工具]

助手: 搜索遇到了限制，让我直接获取页面内容来分析。

[系统会调用 web_fetch 工具]

助手: 我已经获取了页面内容，发现了以下SEO问题：
1. 缺少meta description
2. title标签需要优化
3. H1标签结构不够清晰

让我为你生成一份详细的SEO分析报告。

[系统会调用 write_file 工具]

助手: SEO分析报告已经生成。总结一下主要的优化建议：[详细建议...]
```

**验证标准**：
- 对话包含多个工具调用 (≥3次)
- 包含问题解决过程的思考
- 有试错或策略调整的过程
- 任务具有一定复杂性和可重用性

#### **2.2 技能学习验证**

完成复杂对话后，检查技能是否被学习：

```bash
# 检查技能文件
ls -la data/skills/assistant/
cat data/skills/assistant/*.yaml

# 检查轨迹文件
ls -la data/trajectories/assistant/
```

**期望结果**：
- 在 `data/skills/assistant/` 下生成 `.yaml` 技能文件
- 技能文件包含有意义的名称和描述
- 技能内容包含可重用的解决方案
- 轨迹文件记录完整的对话过程

#### **2.3 技能复用验证**

进行类似的对话，验证技能是否被注入：

```
用户: 帮我优化另一个网站 https://test.com 的SEO

# 观察系统提示词是否被增强
# 检查响应是否更加专业和结构化
```

**验证方法**：
```bash
# 查看日志中的技能注入记录
tail -f logs/app.log | grep -E "(技能|skill|inject)"
```

### **方案 3：A/B 对比测试**

对比启用/禁用学习功能的效果差异：

```bash
# 运行 A/B 对比测试
./scripts/ab_test_learning.sh

# 查看对比报告
cat ab_test_*/ab_test_report.md
```

**关键指标对比**：
- **响应时间**：学习功能对性能的影响
- **任务成功率**：技能复用对准确性的提升
- **回答质量**：结构化程度和专业性
- **一致性**：相似问题的回答一致性

### **方案 4：压力和稳定性测试**

验证学习功能在高负载下的表现：

```bash
# 创建压力测试脚本
cat > stress_test.sh << 'EOF'
#!/bin/bash

# 并发启动多个会话
for i in {1..10}; do
    (
        echo "会话 $i 开始"
        # 模拟复杂对话
        curl -X POST http://localhost:8080/api/chat \
          -H "Content-Type: application/json" \
          -d "{\"message\": \"分析网站SEO问题 - 会话$i\"}"
        echo "会话 $i 完成"
    ) &
done

wait
echo "压力测试完成"
EOF

chmod +x stress_test.sh
./stress_test.sh
```

**监控指标**：
- 内存使用情况
- 响应时间变化
- 错误率
- 技能学习质量

## 📊 **效果评估标准**

### **定量指标**

| 指标类别 | 指标名称 | 期望值 | 测量方法 |
|----------|----------|--------|----------|
| **性能** | 响应时间增加 | <20% | A/B对比测试 |
| **性能** | 内存使用增加 | <100MB | 系统监控 |
| **性能** | 启动时间影响 | <5秒 | 启动时间测量 |
| **学习** | 技能提取成功率 | >60% | 复杂对话测试 |
| **学习** | 技能注入准确率 | >80% | 相似度匹配测试 |
| **学习** | 轨迹记录完整性 | >95% | 数据完整性检查 |

### **定性指标**

| 评估维度 | 评分标准 | 测试方法 |
|----------|----------|----------|
| **技能质量** | 技能是否有意义且可重用 | 人工评估技能内容 |
| **回答改善** | 第二次回答是否更好 | 对比同类问题回答 |
| **系统稳定** | 是否影响现有功能 | 回归测试 |
| **用户体验** | 是否提升交互体验 | 用户反馈收集 |

## 🔍 **详细测试用例**

### **用例 1：SEO分析技能学习**

**目标**：验证复杂SEO分析任务的技能学习

**步骤**：
1. 进行复杂的SEO分析对话
2. 确认技能被成功提取
3. 进行类似SEO分析任务
4. 验证技能被正确注入和应用

**预期结果**：
- 生成名为类似 `seo-website-analysis` 的技能
- 技能包含SEO检查流程和优化建议
- 后续SEO任务响应更专业和结构化

### **用例 2：编程调试技能学习**

**目标**：验证程序调试流程的技能学习

**测试对话**：
```
用户: 我的Python程序运行很慢，帮我找出性能瓶颈

助手: 性能问题需要系统性分析。让我帮你用profiling工具定位瓶颈。

[使用 exec 工具运行 profiler]
[使用 read_file 分析代码]
[使用 write_file 生成优化建议]

助手: 经过分析，主要瓶颈在于...，我已经生成了详细的优化方案。
```

**验证要点**：
- 学习到系统性的调试流程
- 包含工具使用的最佳实践
- 能在类似场景中复用

### **用例 3：API设计技能学习**

**目标**：验证复杂设计任务的技能学习

**测试场景**：
- 用户管理系统API设计
- 电商系统API设计
- 内容管理系统API设计

**验证**：技能是否能捕捉设计模式和最佳实践

### **用例 4：错误处理和边界情况**

**目标**：验证系统在异常情况下的稳定性

**测试场景**：
- LLM API调用失败
- 磁盘空间不足
- 无效的技能文件
- 损坏的轨迹数据

**验证**：系统是否能优雅降级，不影响主功能

## 🚨 **故障诊断**

### **常见问题和解决方案**

#### **问题 1：技能没有被学习**

**可能原因**：
- 对话不够复杂（工具调用次数少）
- 学习触发间隔设置过大
- LLM模型无法正确识别技能

**诊断方法**：
```bash
# 检查学习配置
grep -A 10 "skill_extraction:" config.yaml

# 查看学习相关日志
grep "学习\|skill\|extraction" logs/app.log

# 检查工具调用统计
grep "tool_call" logs/app.log | wc -l
```

**解决方案**：
- 降低 `review_interval` (如 10 → 5)
- 降低 `min_tool_calls` (如 3 → 1)
- 增加对话复杂度
- 检查LLM模型配置

#### **问题 2：技能注入不生效**

**可能原因**：
- 相似度阈值过高
- 技能缓存过期
- 查询关键词不匹配

**诊断方法**：
```bash
# 检查技能文件内容
cat data/skills/assistant/*.yaml

# 检查注入配置
grep -A 5 "injection:" config.yaml

# 测试技能匹配
# 手动查看技能名称、描述、标签是否包含查询关键词
```

**解决方案**：
- 降低 `similarity_threshold` (如 0.3 → 0.2)
- 清理技能缓存
- 优化技能描述和标签
- 检查查询词与技能内容的相关性

#### **问题 3：轨迹压缩失败**

**可能原因**：
- 轨迹数据格式错误
- 压缩模型配置问题
- 保护区域设置过大

**诊断方法**：
```bash
# 检查轨迹文件
ls -la data/trajectories/assistant/
cat data/trajectories/assistant/*.json | jq '.'

# 检查压缩配置
grep -A 10 "compression:" config.yaml
```

**解决方案**：
- 验证轨迹数据JSON格式
- 调整压缩参数
- 检查LLM模型配置

### **性能问题诊断**

#### **内存使用过高**

```bash
# 监控内存使用
ps aux | grep go-nanobot
top -p $(pgrep go-nanobot)

# 检查数据目录大小
du -sh data/
```

**优化建议**：
- 启用自动清理
- 调整数据保留期限
- 限制最大技能/轨迹数量

#### **响应时间变长**

```bash
# 分析响应时间
time curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "测试查询"}'
```

**优化建议**：
- 调整技能缓存设置
- 优化相似度计算算法
- 减少注入的技能数量

## 📈 **持续监控**

### **生产环境监控指标**

```bash
# 创建监控脚本
cat > monitor_learning.sh << 'EOF'
#!/bin/bash

echo "=== 学习功能监控报告 $(date) ==="

# 技能数量统计
echo "技能数量: $(find data/skills -name "*.yaml" | wc -l)"

# 轨迹数量统计
echo "轨迹数量: $(find data/trajectories -name "*.json" | wc -l)"

# 磁盘使用
echo "数据目录大小: $(du -sh data/)"

# 内存使用
echo "内存使用: $(ps -o rss= -p $(pgrep go-nanobot) | awk '{print $1/1024 "MB"}')"

# 最近学习活动
echo "最近学习的技能:"
find data/skills -name "*.yaml" -mtime -1 -exec basename {} .yaml \; | head -5

# 错误日志
echo "最近错误:"
grep -i error logs/app.log | tail -3
EOF

chmod +x monitor_learning.sh

# 设置定期监控
crontab -e
# 添加：0 */6 * * * /path/to/monitor_learning.sh >> monitoring.log
```

### **告警设置**

- 技能学习停止 (24小时无新技能)
- 存储空间过大 (>10GB)
- 内存使用异常 (>1GB)
- 错误率过高 (>5%)

## 🎉 **测试成功标准**

满足以下条件即可认为学习功能测试成功：

✅ **功能正确性**
- [ ] 复杂对话能触发技能学习
- [ ] 生成的技能内容有意义且可重用
- [ ] 技能能在相关场景中被正确注入
- [ ] 轨迹数据完整记录执行过程

✅ **性能可接受**
- [ ] 响应时间增加 <20%
- [ ] 内存使用增加 <100MB
- [ ] 不影响现有功能正常运行

✅ **效果显著**
- [ ] 相同类型任务的第二次回答质量更高
- [ ] 回答结构化和专业性明显提升
- [ ] 用户体验有明显改善

✅ **稳定可靠**
- [ ] 长时间运行无异常
- [ ] 高并发场景下表现稳定
- [ ] 错误情况下能优雅降级

完成以上验证后，你就可以确信学习功能已经成功集成并能带来实际价值！

## 📚 **相关资源**

- [配置示例](../config/examples/learning-example.yaml)
- [迁移指南](./LEARNING_MIGRATION.md)
- [故障排除](./TROUBLESHOOTING.md)
- [性能优化](./PERFORMANCE_TUNING.md)