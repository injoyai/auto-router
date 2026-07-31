package store

import "gorm.io/gorm"

type RoutingConfig struct {
	ID                 uint  `gorm:"primaryKey" json:"id"`
	JudgeModelID       *uint `json:"judge_model_id"`
	DefaultModelID     *uint `json:"default_model_id"` // deprecated: 保留列以兼容旧库,代码不再使用
	DefaultGroupID     *uint `json:"default_group_id"` // 默认兜底队列
	JudgeMaxInputChars int   `gorm:"default:2000" json:"judge_max_input_chars"`
}

func (s *Store) GetRoutingConfig() (*RoutingConfig, error) {
	var rc RoutingConfig
	if err := s.DB.First(&rc, 1).Error; err != nil {
		return nil, err
	}
	return &rc, nil
}

// UpdateRoutingConfig saves the routing config singleton. I1: when JudgeModelID
// is non-nil, it transactionally mirrors the selection onto models.is_judge
// (clearing others) so the engine's GetJudgeModel() — the single source of
// truth — honors admin updates via PUT /admin/routing. When JudgeModelID is
// nil, is_judge is left untouched (e.g. a direct POST /admin/models/:id/judge
// still works independently).
func (s *Store) UpdateRoutingConfig(rc *RoutingConfig) error {
	rc.ID = 1
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(rc).Error; err != nil {
			return err
		}
		if rc.JudgeModelID == nil {
			return nil
		}
		if err := tx.Model(&Model{}).Where("1 = 1").Update("is_judge", false).Error; err != nil {
			return err
		}
		return tx.Model(&Model{}).Where("id = ?", *rc.JudgeModelID).Update("is_judge", true).Error
	})
}
