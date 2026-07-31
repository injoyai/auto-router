package store

import (
	"time"

	"gorm.io/gorm"
)

type Model struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	ProviderID  uint      `gorm:"not null" json:"provider_id"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) ListModels() ([]Model, error) {
	var ms []Model
	err := s.DB.Order("id desc").Find(&ms).Error
	return ms, err
}

func (s *Store) ListEnabledModels() ([]Model, error) {
	var ms []Model
	err := s.DB.Where("enabled = ?", true).Find(&ms).Error
	return ms, err
}

func (s *Store) GetModel(id uint) (*Model, error) {
	var m Model
	if err := s.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) CreateModel(m *Model) error {
	return s.DB.Create(m).Error
}

func (s *Store) UpdateModel(m *Model) error {
	return s.DB.Save(m).Error
}

func (s *Store) DeleteModel(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("model_id = ?", id).Delete(&ModelGroupItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Model{}, id).Error
	})
}
