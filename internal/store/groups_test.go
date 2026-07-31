package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelGroupCRUD(t *testing.T) {
	s := newTestStore(t)
	g := &ModelGroup{Name: "deepseek-v4-flash", DisplayName: "DSV4", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))
	assert.NotZero(t, g.ID)

	got, err := s.GetModelGroupByName("deepseek-v4-flash")
	assert.NoError(t, err)
	assert.Equal(t, "DSV4", got.DisplayName)

	got.Description = "fast"
	assert.NoError(t, s.UpdateModelGroup(got))
	assert.NoError(t, s.DeleteModelGroup(got.ID))
}

func TestReplaceGroupItemsOrderAndDedup(t *testing.T) {
	s := newTestStore(t)
	prov := &Provider{Name: "p", BaseURL: "http://x", Protocol: "openai", Enabled: true}
	assert.NoError(t, s.CreateProvider(prov))
	m1 := &Model{Name: "m1", DisplayName: "1", ProviderID: prov.ID, Enabled: true}
	m2 := &Model{Name: "m2", DisplayName: "2", ProviderID: prov.ID, Enabled: true}
	assert.NoError(t, s.CreateModel(m1))
	assert.NoError(t, s.CreateModel(m2))
	g := &ModelGroup{Name: "q", DisplayName: "Q", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))

	// 重复 m2 应去重,顺序保留首次出现
	assert.NoError(t, s.ReplaceGroupItems(g.ID, []uint{m2.ID, m1.ID, m2.ID}))

	items, err := s.GetGroupItemsOrdered(g.ID)
	assert.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, m2.ID, items[0].ModelID)
	assert.Equal(t, 0, items[0].Position)
	assert.Equal(t, m1.ID, items[1].ModelID)
	assert.Equal(t, 1, items[1].Position)
}

func TestGetGroupChainFiltersDisabled(t *testing.T) {
	s := newTestStore(t)
	prov := &Provider{Name: "p", BaseURL: "http://x", Protocol: "openai", Enabled: true}
	assert.NoError(t, s.CreateProvider(prov))
	on := &Model{Name: "on", DisplayName: "on", ProviderID: prov.ID, Enabled: true}
	off := &Model{Name: "off", DisplayName: "off", ProviderID: prov.ID, Enabled: true}
	assert.NoError(t, s.CreateModel(on))
	assert.NoError(t, s.CreateModel(off))
	// Model.Enabled 带 gorm:"default:true",Create 时零值 false 会被默认值覆盖为 true,
	// 故先创建为启用,再用显式列更新关闭,确保 DB 中 enabled=false。
	assert.NoError(t, s.DB.Model(&Model{}).Where("id = ?", off.ID).Update("enabled", false).Error)
	g := &ModelGroup{Name: "q", DisplayName: "Q", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))
	assert.NoError(t, s.ReplaceGroupItems(g.ID, []uint{on.ID, off.ID}))

	chain, err := s.GetGroupChain(g.ID)
	assert.NoError(t, err)
	assert.Len(t, chain, 1)
	assert.Equal(t, "on", chain[0].Name)
}

func TestDeleteModelGroupCascadesItems(t *testing.T) {
	s := newTestStore(t)
	prov := &Provider{Name: "p", BaseURL: "http://x", Protocol: "openai", Enabled: true}
	assert.NoError(t, s.CreateProvider(prov))
	m := &Model{Name: "m", DisplayName: "m", ProviderID: prov.ID, Enabled: true}
	assert.NoError(t, s.CreateModel(m))
	g := &ModelGroup{Name: "q", DisplayName: "Q", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))
	assert.NoError(t, s.ReplaceGroupItems(g.ID, []uint{m.ID}))

	assert.NoError(t, s.DeleteModelGroup(g.ID))
	items, _ := s.GetGroupItemsOrdered(g.ID)
	assert.Empty(t, items)
}
