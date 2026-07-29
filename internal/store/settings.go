package store

type Setting struct {
	Key   string `gorm:"primaryKey" json:"key"`
	Value string `json:"value"`
}

func (s *Store) GetSetting(key string) (string, error) {
	var st Setting
	if err := s.DB.First(&st, "key = ?", key).Error; err != nil {
		return "", err
	}
	return st.Value, nil
}

func (s *Store) SetSetting(key, value string) error {
	return s.DB.Save(&Setting{Key: key, Value: value}).Error
}
