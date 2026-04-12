// Package learning - 技能存储库的具体实现
// 遵循 Google Go 编程规范，实现 SkillRepository 接口
package learning

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// FileBasedSkillRepository 基于文件系统的技能存储库实现
// 采用文件系统存储，支持并发安全的技能管理
type FileBasedSkillRepository struct {
	basePath     string      // 技能存储基础路径
	logger       *zap.Logger // 日志器
	mu           sync.RWMutex // 读写锁，保护文件操作
	skillsIndex  map[string]*SkillMetadata // 内存索引，提升查询性能
	indexLoaded  bool        // 索引是否已加载
}

// NewFileBasedSkillRepository 创建文件系统技能存储库
func NewFileBasedSkillRepository(basePath string, logger *zap.Logger) (*FileBasedSkillRepository, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	// 确保目录存在
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("创建技能存储目录失败: %w", err)
	}

	repo := &FileBasedSkillRepository{
		basePath:    basePath,
		logger:      logger,
		skillsIndex: make(map[string]*SkillMetadata),
	}

	// 初始化时构建索引
	if err := repo.buildIndex(); err != nil {
		logger.Warn("构建技能索引失败", zap.Error(err))
		// 不返回错误，允许仓库正常工作
	}

	return repo, nil
}

// SaveSkill 实现 SkillRepository.SaveSkill
// 保存技能到文件系统，支持创建和更新
func (r *FileBasedSkillRepository) SaveSkill(ctx context.Context, skill *Skill) error {
	if skill == nil {
		return fmt.Errorf("技能不能为空")
	}
	if skill.Name == "" {
		return fmt.Errorf("技能名称不能为空")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 生成安全的文件名
	fileName := r.generateFileName(skill.Name)
	filePath := filepath.Join(r.basePath, fileName)

	// 检查是否是更新操作
	isUpdate := r.skillExists(skill.Name)
	if isUpdate {
		skill.UpdatedAt = time.Now()
		// 版本号递增逻辑可以在这里实现
		r.logger.Info("更新技能",
			zap.String("skill_name", skill.Name),
			zap.String("file_path", filePath),
		)
	} else {
		skill.CreatedAt = time.Now()
		skill.UpdatedAt = time.Now()
		if skill.Version == "" {
			skill.Version = "1.0.0"
		}
		r.logger.Info("创建新技能",
			zap.String("skill_name", skill.Name),
			zap.String("file_path", filePath),
		)
	}

	// 序列化技能到 YAML
	data, err := yaml.Marshal(skill)
	if err != nil {
		return fmt.Errorf("序列化技能失败: %w", err)
	}

	// 原子性写入文件
	if err := r.atomicWriteFile(filePath, data); err != nil {
		return fmt.Errorf("写入技能文件失败: %w", err)
	}

	// 更新内存索引
	r.updateIndex(skill)

	r.logger.Info("技能保存成功",
		zap.String("skill_name", skill.Name),
		zap.String("version", skill.Version),
		zap.Bool("is_update", isUpdate),
	)

	return nil
}

// GetSkill 实现 SkillRepository.GetSkill
// 从文件系统加载指定技能
func (r *FileBasedSkillRepository) GetSkill(ctx context.Context, name string) (*Skill, error) {
	if name == "" {
		return nil, fmt.Errorf("技能名称不能为空")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	fileName := r.generateFileName(name)
	filePath := filepath.Join(r.basePath, fileName)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("技能 %q 不存在", name)
	}

	// 读取并解析技能文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取技能文件失败: %w", err)
	}

	var skill Skill
	if err := yaml.Unmarshal(data, &skill); err != nil {
		return nil, fmt.Errorf("解析技能文件失败: %w", err)
	}

	r.logger.Debug("成功加载技能",
		zap.String("skill_name", name),
		zap.String("version", skill.Version),
	)

	return &skill, nil
}

// ListSkills 实现 SkillRepository.ListSkills
// 列出所有技能的元数据
func (r *FileBasedSkillRepository) ListSkills(ctx context.Context) ([]*SkillMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 如果索引未加载，重新构建
	if !r.indexLoaded {
		r.mu.RUnlock() // 临时释放读锁
		r.mu.Lock()   // 获取写锁
		if !r.indexLoaded { // 双重检查
			if err := r.buildIndex(); err != nil {
				r.mu.Unlock()
				return nil, fmt.Errorf("构建技能索引失败: %w", err)
			}
		}
		r.mu.Unlock()
		r.mu.RLock() // 重新获取读锁
	}

	// 从索引复制元数据
	skills := make([]*SkillMetadata, 0, len(r.skillsIndex))
	for _, metadata := range r.skillsIndex {
		// 深拷贝以避免并发问题
		skillCopy := *metadata
		skills = append(skills, &skillCopy)
	}

	r.logger.Debug("列出技能",
		zap.Int("total_skills", len(skills)),
	)

	return skills, nil
}

// DeleteSkill 实现 SkillRepository.DeleteSkill
// 删除指定技能
func (r *FileBasedSkillRepository) DeleteSkill(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("技能名称不能为空")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	fileName := r.generateFileName(name)
	filePath := filepath.Join(r.basePath, fileName)

	// 检查技能是否存在
	if !r.skillExists(name) {
		return fmt.Errorf("技能 %q 不存在", name)
	}

	// 删除文件
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除技能文件失败: %w", err)
	}

	// 从索引中移除
	delete(r.skillsIndex, name)

	r.logger.Info("技能已删除",
		zap.String("skill_name", name),
		zap.String("file_path", filePath),
	)

	return nil
}

// FindSimilar 实现 SkillRepository.FindSimilar
// 基于简单的字符串匹配查找相似技能
func (r *FileBasedSkillRepository) FindSimilar(ctx context.Context, query string, limit int) ([]*SkillMetadata, error) {
	if query == "" {
		return []*SkillMetadata{}, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var matches []*SkillMetadata

	for _, metadata := range r.skillsIndex {
		// 简单的相似度计算：检查名称和描述中是否包含查询词
		nameMatch := strings.Contains(strings.ToLower(metadata.Name), queryLower)
		descMatch := strings.Contains(strings.ToLower(metadata.Description), queryLower)
		tagMatch := r.tagsContain(metadata.Tags, queryLower)

		if nameMatch || descMatch || tagMatch {
			// 深拷贝以避免并发问题
			matchCopy := *metadata
			matches = append(matches, &matchCopy)
		}

		// 限制结果数量
		if len(matches) >= limit {
			break
		}
	}

	r.logger.Debug("查找相似技能",
		zap.String("query", query),
		zap.Int("matches_found", len(matches)),
		zap.Int("limit", limit),
	)

	return matches, nil
}

// buildIndex 构建技能索引
func (r *FileBasedSkillRepository) buildIndex() error {
	r.skillsIndex = make(map[string]*SkillMetadata)

	err := filepath.WalkDir(r.basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// 只处理 .yaml 和 .yml 文件
		if d.IsDir() || (!strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml")) {
			return nil
		}

		// 读取技能文件
		data, err := os.ReadFile(path)
		if err != nil {
			r.logger.Warn("读取技能文件失败",
				zap.String("file_path", path),
				zap.Error(err),
			)
			return nil // 继续处理其他文件
		}

		// 解析技能
		var skill Skill
		if err := yaml.Unmarshal(data, &skill); err != nil {
			r.logger.Warn("解析技能文件失败",
				zap.String("file_path", path),
				zap.Error(err),
			)
			return nil // 继续处理其他文件
		}

		// 添加到索引
		metadata := &SkillMetadata{
			Name:        skill.Name,
			Description: skill.Description,
			Version:     skill.Version,
			Tags:        skill.Tags,
			CreatedAt:   skill.CreatedAt,
			UsageCount:  skill.UsageCount,
			SuccessRate: skill.SuccessRate,
		}
		r.skillsIndex[skill.Name] = metadata

		return nil
	})

	if err != nil {
		return err
	}

	r.indexLoaded = true
	r.logger.Info("技能索引构建完成",
		zap.Int("total_skills", len(r.skillsIndex)),
		zap.String("base_path", r.basePath),
	)

	return nil
}

// generateFileName 生成安全的文件名
func (r *FileBasedSkillRepository) generateFileName(skillName string) string {
	// 替换不安全的字符
	safe := strings.ReplaceAll(skillName, "/", "_")
	safe = strings.ReplaceAll(safe, "\\", "_")
	safe = strings.ReplaceAll(safe, ":", "_")
	safe = strings.ReplaceAll(safe, "*", "_")
	safe = strings.ReplaceAll(safe, "?", "_")
	safe = strings.ReplaceAll(safe, "\"", "_")
	safe = strings.ReplaceAll(safe, "<", "_")
	safe = strings.ReplaceAll(safe, ">", "_")
	safe = strings.ReplaceAll(safe, "|", "_")

	return safe + ".yaml"
}

// skillExists 检查技能是否存在（需要在锁内调用）
func (r *FileBasedSkillRepository) skillExists(name string) bool {
	_, exists := r.skillsIndex[name]
	return exists
}

// updateIndex 更新内存索引（需要在锁内调用）
func (r *FileBasedSkillRepository) updateIndex(skill *Skill) {
	metadata := &SkillMetadata{
		Name:        skill.Name,
		Description: skill.Description,
		Version:     skill.Version,
		Tags:        skill.Tags,
		CreatedAt:   skill.CreatedAt,
		UsageCount:  skill.UsageCount,
		SuccessRate: skill.SuccessRate,
	}
	r.skillsIndex[skill.Name] = metadata
}

// tagsContain 检查标签列表是否包含查询词
func (r *FileBasedSkillRepository) tagsContain(tags []string, query string) bool {
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

// atomicWriteFile 原子性写入文件
func (r *FileBasedSkillRepository) atomicWriteFile(filePath string, data []byte) error {
	// 写入临时文件
	tempFile := filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	// 原子性重命名
	if err := os.Rename(tempFile, filePath); err != nil {
		// 清理临时文件
		os.Remove(tempFile)
		return fmt.Errorf("重命名文件失败: %w", err)
	}

	return nil
}