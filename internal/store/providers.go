package store

import "time"

type Provider struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	BaseURL   string    `gorm:"not null" json:"base_url"`
	APIKey    string    `gorm:"not null" json:"-"`        // encrypted; never JSON-exposed
	HasAPIKey bool      `gorm:"-" json:"has_api_key"`     // non-persisted; true if APIKey is set
	RetryMax      int       `gorm:"default:0" json:"retry_max"`          // max retry attempts (0 = no retry)
	RetryBackoffMs int     `gorm:"default:500" json:"retry_backoff_ms"` // backoff base in ms, exponential: base * 2^(attempt-1)
	Protocol  string    `gorm:"not null" json:"protocol"` // openai | claude
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListProviders() ([]Provider, error) {
	var ps []Provider
	err := s.DB.Order("id desc").Find(&ps).Error
	return ps, err
}

func (s *Store) GetProvider(id uint) (*Provider, error) {
	var p Provider
	if err := s.DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateProvider(p *Provider) error {
	return s.DB.Create(p).Error
}

func (s *Store) UpdateProvider(p *Provider) error {
	return s.DB.Save(p).Error
}

func (s *Store) DeleteProvider(id uint) error {
	return s.DB.Delete(&Provider{}, id).Error
}

// CountModelsByProvider returns the number of models referencing the provider.
// Used by I10 to reject deletion of a provider that still has models.
func (s *Store) CountModelsByProvider(providerID uint) (int64, error) {
	var n int64
	err := s.DB.Model(&Model{}).Where("provider_id = ?", providerID).Count(&n).Error
	return n, err
}
