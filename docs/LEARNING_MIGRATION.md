# go-nanobot 学习功能迁移指南

本指南帮助您将现有的 go-nanobot 系统升级到支持自学习循环和强化学习的新版本。

## 🎯 **功能概览**

新增的学习功能包括：

### **1. 自学习循环**
- **自动技能提取**：从复杂任务中自动学习可重用的技能
- **技能管理**：版本化的技能存储和检索
- **智能注入**：根据查询相关性自动注入技能到系统提示

### **2. 强化学习集成**
- **轨迹跟踪**：记录完整的 Agent 执行轨迹
- **轨迹压缩**：智能压缩长轨迹，保护重要步骤
- **策略优化**：基于历史数据优化路由和工具选择

## 📋 **迁移步骤**

### **步骤 1：检查系统兼容性**

确保您的系统满足以下要求：

```bash
# 检查 Go 版本
go version  # 需要 Go 1.21+

# 检查磁盘空间
df -h  # 学习数据需要额外存储空间

# 检查内存
free -h  # 建议至少 2GB 可用内存
```

### **步骤 2：备份现有配置**

```bash
# 备份配置文件
cp config.yaml config.yaml.backup

# 备份工作空间
cp -r workspace workspace.backup
```

### **步骤 3：更新配置文件**

#### **3.1 添加学习配置（可选）**

在现有的 `config.yaml` 中添加学习配置节：

```yaml
# 在现有配置末尾添加
learning:
  enabled: true  # 启用学习功能

  skill_extraction:
    review_interval: 10        # 每10次工具调用检查一次
    min_conversation_length: 5 # 至少5轮对话
    min_tool_calls: 3         # 至少3次工具调用

  storage:
    type: "file"
    path: "./data/skills"  # 技能存储路径
    max_skills: 1000

  injection:
    max_inject_skills: 3      # 最多注入3个技能
    similarity_threshold: 0.3  # 相似度阈值
```

#### **3.2 添加 RL 配置（可选）**

```yaml
rl:
  enabled: true  # 启用强化学习

  tracking:
    storage_path: "./data/trajectories"
    max_trajectories: 1000
    retention_days: 30

  compression:
    target_max_steps: 50
    summary_target_length: 500
    protected_steps:
      first_n: 5
      last_n: 3
      protect_error: true
```

### **步骤 4：创建数据目录**

```bash
# 创建学习数据目录
mkdir -p data/skills
mkdir -p data/trajectories

# 设置权限
chmod 755 data/skills
chmod 755 data/trajectories
```

### **步骤 5：渐进式启用**

#### **5.1 仅启用自学习循环**

首先只启用自学习功能：

```yaml
learning:
  enabled: true

rl:
  enabled: false  # 暂时禁用 RL
```

#### **5.2 监控学习效果**

```bash
# 检查技能生成情况
ls -la data/skills/

# 查看日志
tail -f logs/app.log | grep -E "(技能|skill)"
```

#### **5.3 启用强化学习**

确认自学习功能正常后，启用 RL：

```yaml
rl:
  enabled: true
```

## 🔧 **配置选项详解**

### **向后兼容性**

新版本完全向后兼容，学习功能默认**禁用**：

- 如果配置文件中没有 `learning` 和 `rl` 节，系统按原方式运行
- 所有现有功能保持不变
- 性能影响最小（学习功能禁用时无额外开销）

### **学习功能配置**

```yaml
learning:
  enabled: false  # 默认禁用，确保兼容性

  skill_extraction:
    review_interval: 10        # 技能审查间隔
    min_conversation_length: 5 # 最小对话长度要求
    min_tool_calls: 3         # 最小工具调用次数
    extractor_model: ""       # 提取模型（空则使用默认）

  storage:
    type: "file"              # 存储类型
    path: "./data/skills"     # 存储路径
    max_skills: 1000         # 最大技能数

  injection:
    max_inject_skills: 3      # 最大注入技能数
    similarity_threshold: 0.3  # 相似度阈值
    cache_expire_seconds: 300  # 缓存过期时间
```

### **强化学习配置**

```yaml
rl:
  enabled: false  # 默认禁用

  tracking:
    storage_path: "./data/trajectories"
    max_trajectories: 1000
    retention_days: 30
    auto_cleanup: true

  compression:
    target_max_steps: 50           # 压缩目标步骤数
    summary_target_length: 500     # 摘要长度
    compression_strategy: "summarize"
    min_compression_ratio: 0.3

    protected_steps:               # 保护策略
      first_n: 5                   # 保护前5步
      last_n: 3                    # 保护后3步
      protect_error: true          # 保护错误步骤
      protect_tool: true           # 保护工具调用
```

## 📊 **监控和观察**

### **技能学习监控**

```bash
# 查看学习统计
curl http://localhost:8080/api/learning/stats

# 列出已学习的技能
curl http://localhost:8080/api/skills/list

# 查看特定技能
curl http://localhost:8080/api/skills/view?name=skill_name
```

### **轨迹监控**

```bash
# 查看轨迹统计
curl http://localhost:8080/api/rl/stats

# 列出最近的轨迹
curl http://localhost:8080/api/trajectories/list?limit=10

# 查看压缩统计
curl http://localhost:8080/api/compression/stats
```

### **性能监控**

```bash
# 监控内存使用
ps aux | grep go-nanobot

# 监控文件系统使用
du -sh data/

# 监控日志
tail -f logs/app.log
```

## ⚠️ **注意事项**

### **资源使用**

- **磁盘空间**：学习数据会占用额外磁盘空间
  - 技能文件：通常每个技能 1-5KB
  - 轨迹文件：每条轨迹 10-100KB
- **内存使用**：启用学习功能会增加 ~50-100MB 内存使用
- **CPU 开销**：技能提取和轨迹压缩会消耗额外 CPU

### **数据管理**

- **备份策略**：定期备份 `data/` 目录
- **清理策略**：配置合理的保留期限和最大数量
- **监控策略**：监控数据目录大小，设置告警

### **安全考虑**

- **数据隐私**：学习数据可能包含敏感信息
- **访问控制**：确保数据目录有适当的权限设置
- **审计日志**：启用学习功能相关的审计日志

## 🚀 **高级配置**

### **多 Agent 学习配置**

```yaml
agents:
  list:
    - id: "researcher"
      # 为特定 Agent 定制学习参数
      learning:
        skill_extraction:
          review_interval: 5  # 更频繁的技能检查

    - id: "developer"
      learning:
        injection:
          max_inject_skills: 5  # 注入更多技能
```

### **自定义技能提取提示词**

```yaml
learning:
  skill_extraction:
    review_prompt: |
      根据以下对话分析是否包含值得学习的技能：
      
      关注点：
      1. 是否有非标准的解决方案？
      2. 是否经历了试错和调优过程？
      3. 是否包含领域专业知识？
      
      如果值得学习，请提取技能...
```

### **压缩策略定制**

```yaml
rl:
  compression:
    # 不同压缩策略的配置
    compression_strategy: "selective"  # summarize, merge, selective
    
    # 自定义保护逻辑
    protected_steps:
      first_n: 3
      last_n: 5
      protect_error: true
      protect_tool: true
      # 自定义保护规则
      custom_protection:
        - pattern: "delegate"      # 保护委托调用
        - pattern: "spawn"         # 保护子任务生成
        - pattern: "web_search"    # 保护重要搜索
```

## 📈 **效果评估**

### **学习效果指标**

- **技能提取率**：成功提取技能的会话比例
- **技能复用率**：技能被注入使用的频率
- **任务成功率**：启用学习前后的任务完成率对比
- **响应质量**：用户满意度和响应准确性

### **性能指标**

- **响应时间**：技能注入对响应速度的影响
- **资源使用**：CPU、内存、磁盘的额外消耗
- **吞吐量**：系统处理能力的变化

### **A/B 测试建议**

```yaml
# 配置 A：不启用学习功能
learning:
  enabled: false
rl:
  enabled: false

# 配置 B：启用学习功能
learning:
  enabled: true
rl:
  enabled: true
```

## 🆘 **故障排除**

### **常见问题**

1. **学习功能不生效**
   - 检查 `enabled: true` 是否正确设置
   - 查看日志中是否有错误信息
   - 确认数据目录权限正确

2. **技能不被注入**
   - 检查相似度阈值设置
   - 确认技能文件格式正确
   - 查看技能缓存是否过期

3. **轨迹压缩失败**
   - 检查压缩模型配置
   - 确认轨迹数据完整性
   - 调整压缩参数

### **回滚方案**

如果遇到问题需要回滚：

```bash
# 1. 禁用学习功能
# 在 config.yaml 中设置：
# learning:
#   enabled: false
# rl:
#   enabled: false

# 2. 重启服务
systemctl restart go-nanobot

# 3. 恢复备份配置（如必要）
cp config.yaml.backup config.yaml
```

## 📚 **相关文档**

- [技能系统详细说明](./SKILLS.md)
- [强化学习架构](./RL_ARCHITECTURE.md)
- [API 参考文档](./API.md)
- [最佳实践指南](./BEST_PRACTICES.md)

## 💡 **最佳实践**

1. **渐进式启用**：先启用学习功能，确认稳定后再启用 RL
2. **监控优先**：设置完善的监控和告警
3. **备份策略**：定期备份学习数据
4. **性能调优**：根据实际使用情况调整配置参数
5. **用户反馈**：收集用户反馈，持续优化学习效果