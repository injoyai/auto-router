package store

type RoutingConfig struct {
	ID                       uint  `gorm:"primaryKey" json:"id"`
	JudgeModelID             *uint `json:"judge_model_id"`
	DefaultModelID           *uint `json:"default_model_id"`
	EnableNextModelDirective bool  `gorm:"default:true" json:"enable_next_model_directive"`
	SessionTTLSeconds        int   `gorm:"default:1800" json:"session_ttl_seconds"`
	JudgeMaxInputChars       int   `gorm:"default:2000" json:"judge_max_input_chars"`
}

func (s *Store) GetRoutingConfig() (*RoutingConfig, error) {
	var rc RoutingConfig
	if err := s.DB.First(&rc, 1).Error; err != nil {
		return nil, err
	}
	return &rc, nil
}

func (s *Store) UpdateRoutingConfig(rc *RoutingConfig) error {
	rc.ID = 1
	return s.DB.Save(rc).Error
}
