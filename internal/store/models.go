package store

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

type Model struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	DisplayName string    `gorm:"not null" json:"display_name"`
	ProviderID  uint      `gorm:"not null" json:"provider_id"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	IsJudge     bool      `gorm:"default:false" json:"is_judge"`
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

func (s *Store) GetModelByName(name string) (*Model, error) {
	var m Model
	err := s.DB.Where("name = ? OR display_name = ?", name, name).First(&m).Error
	return &m, err
}

func (s *Store) CreateModel(m *Model) error {
	return s.DB.Create(m).Error
}

func (s *Store) UpdateModel(m *Model) error {
	return s.DB.Save(m).Error
}

func (s *Store) DeleteModel(id uint) error {
	return s.DB.Delete(&Model{}, id).Error
}

// SetJudgeModel marks the given model as the sole judge, unsetting others.
func (s *Store) SetJudgeModel(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var m Model
		if err := tx.First(&m, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&Model{}).Where("1 = 1").Update("is_judge", false).Error; err != nil {
			return err
		}
		return tx.Model(&Model{}).Where("id = ?", id).Update("is_judge", true).Error
	})
}

func (s *Store) GetJudgeModel() (*Model, error) {
	var m Model
	err := s.DB.Where("is_judge = ? AND enabled = ?", true, true).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, err
}
