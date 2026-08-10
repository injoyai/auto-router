package store

type Setting struct {
	Key   string `gorm:"primaryKey;size:255" json:"key"`
	Value string `json:"value"`
}

func (s *Store) GetSetting(key string) (string, error) {
	var st Setting
	// key 是 MySQL 保留字,必须加反引号,否则 WHERE key = ? 语法错误,
	// 导致每次都读不到 crypto_seed,重启后生成新 seed,旧 API key 无法解密
	if err := s.DB.First(&st, "`key` = ?", key).Error; err != nil {
		return "", err
	}
	return st.Value, nil
}

func (s *Store) SetSetting(key, value string) error {
	return s.DB.Save(&Setting{Key: key, Value: value}).Error
}
