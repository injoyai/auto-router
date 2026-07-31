package store

type RoutingConfig struct {
	ID                 uint  `gorm:"primaryKey" json:"id"`
	JudgeGroupID       *uint `json:"judge_group_id"`        // 判定队列，指向 ModelGroup
	DefaultGroupID     *uint `json:"default_group_id"`      // default fallback queue
	JudgeMaxInputChars int   `gorm:"default:2000" json:"judge_max_input_chars"`
}

func (s *Store) GetRoutingConfig() (*RoutingConfig, error) {
	var rc RoutingConfig
	if err := s.DB.First(&rc, 1).Error; err != nil {
		return nil, err
	}
	return &rc, nil
}

// UpdateRoutingConfig saves the routing config singleton. 判定完全由
// judge_group_id 驱动；旧 is_judge 镜像逻辑已随判定队列化移除。
func (s *Store) UpdateRoutingConfig(rc *RoutingConfig) error {
	rc.ID = 1
	return s.DB.Save(rc).Error
}
