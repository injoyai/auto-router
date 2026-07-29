package store

import "time"

type Session struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	NextModel string    `json:"next_model"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Store) GetSession(id string) (*Session, error) {
	var sess Session
	if err := s.DB.Where("id = ? AND expires_at > ?", id, time.Now()).First(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) SetNextModel(id, model string, ttl time.Duration) error {
	sess := Session{
		ID:        id,
		NextModel: model,
		ExpiresAt: time.Now().Add(ttl),
	}
	return s.DB.Save(&sess).Error
}

func (s *Store) CleanExpiredSessions() (int64, error) {
	res := s.DB.Where("expires_at < ?", time.Now()).Delete(&Session{})
	return res.RowsAffected, res.Error
}
