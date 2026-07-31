package store

import (
	"time"

	"gorm.io/gorm"
)

// ModelGroup 是对外可路由的具名队列,映射到一组有序 Model。
type ModelGroup struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	DisplayName string    `gorm:"not null" json:"display_name"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

// ModelGroupItem 是队列内模型的有序关联,Position 升序即请求顺序。
type ModelGroupItem struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	GroupID  uint `gorm:"not null;uniqueIndex:idx_group_model,priority:1;uniqueIndex:idx_group_pos,priority:1" json:"group_id"`
	ModelID  uint `gorm:"not null;uniqueIndex:idx_group_model,priority:2" json:"model_id"`
	Position int  `gorm:"uniqueIndex:idx_group_pos,priority:2" json:"position"`
}

func (s *Store) ListModelGroups() ([]ModelGroup, error) {
	var gs []ModelGroup
	err := s.DB.Order("id desc").Find(&gs).Error
	return gs, err
}

func (s *Store) ListEnabledModelGroups() ([]ModelGroup, error) {
	var gs []ModelGroup
	err := s.DB.Where("enabled = ?", true).Find(&gs).Error
	return gs, err
}

func (s *Store) GetModelGroup(id uint) (*ModelGroup, error) {
	var g ModelGroup
	if err := s.DB.First(&g, id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) GetModelGroupByName(name string) (*ModelGroup, error) {
	var g ModelGroup
	err := s.DB.Where("name = ?", name).First(&g).Error
	return &g, err
}

func (s *Store) CreateModelGroup(g *ModelGroup) error {
	return s.DB.Create(g).Error
}

func (s *Store) UpdateModelGroup(g *ModelGroup) error {
	return s.DB.Save(g).Error
}

// DeleteModelGroup 删除队列并级联删除其 items。
func (s *Store) DeleteModelGroup(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&ModelGroupItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ModelGroup{}, id).Error
	})
}

// GetGroupItemsOrdered 返回队列内 items,按 Position 升序。
func (s *Store) GetGroupItemsOrdered(groupID uint) ([]ModelGroupItem, error) {
	var items []ModelGroupItem
	err := s.DB.Where("group_id = ?", groupID).Order("position asc").Find(&items).Error
	return items, err
}

// GetGroupChain 返回队列内启用且其 Provider 启用的模型,按 Position 升序。
// 这是路由引擎实际尝试的有序链。
func (s *Store) GetGroupChain(groupID uint) ([]Model, error) {
	items, err := s.GetGroupItemsOrdered(groupID)
	if err != nil {
		return nil, err
	}
	var chain []Model
	for _, it := range items {
		var m Model
		if err := s.DB.First(&m, it.ModelID).Error; err != nil {
			continue
		}
		if !m.Enabled {
			continue
		}
		var p Provider
		if err := s.DB.First(&p, m.ProviderID).Error; err != nil {
			continue
		}
		if !p.Enabled {
			continue
		}
		chain = append(chain, m)
	}
	return chain, nil
}

// ReplaceGroupItems 事务内整体替换队列 items,modelIDs 数组下标即 Position。
// 自动去重(重复取首次出现),跳过 0。
func (s *Store) ReplaceGroupItems(groupID uint, modelIDs []uint) error {
	seen := make(map[uint]bool)
	uniq := make([]uint, 0, len(modelIDs))
	for _, id := range modelIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&ModelGroupItem{}).Error; err != nil {
			return err
		}
		for i, mid := range uniq {
			var m Model
			if err := tx.First(&m, mid).Error; err != nil {
				continue // model does not exist; skip to avoid dangling item
			}
			_ = m
			if err := tx.Create(&ModelGroupItem{GroupID: groupID, ModelID: mid, Position: i}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// CountGroupsByModel 返回引用该模型的队列数(诊断/删除提示用)。
func (s *Store) CountGroupsByModel(modelID uint) (int64, error) {
	var n int64
	err := s.DB.Model(&ModelGroupItem{}).Where("model_id = ?", modelID).Count(&n).Error
	return n, err
}
