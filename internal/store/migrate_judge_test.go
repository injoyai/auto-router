package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// openLegacyDB builds a DB with the PRE-migration schema (is_judge +
// judge_model_id columns) but WITHOUT running AutoMigrate on the new structs,
// so the legacy columns remain as the source of truth for migration.
func openLegacyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	// Minimal legacy schema. Column names are backtick-quoted to match the
	// DDL that GORM's AutoMigrate actually produces — the glebarez/sqlite
	// migrator's removeColumn regex only matches quoted column names.
	db.Exec("CREATE TABLE `providers` (`id` INTEGER PRIMARY KEY, `name` TEXT, `base_url` TEXT, `api_key` TEXT, `protocol` TEXT, `enabled` INTEGER, `retry_max` INTEGER, `retry_backoff_ms` INTEGER, `proxy_url` TEXT, `created_at` DATETIME)")
	db.Exec("CREATE TABLE `models` (`id` INTEGER PRIMARY KEY, `name` TEXT, `provider_id` INTEGER, `description` TEXT, `enabled` INTEGER, `is_judge` INTEGER, `created_at` DATETIME)")
	db.Exec("CREATE TABLE `routing_configs` (`id` INTEGER PRIMARY KEY, `judge_model_id` INTEGER, `default_group_id` INTEGER, `judge_max_input_chars` INTEGER, `judge_group_id` INTEGER)")
	db.Exec("CREATE TABLE `model_groups` (`id` INTEGER PRIMARY KEY, `name` TEXT UNIQUE, `remark` TEXT, `enabled` INTEGER, `created_at` DATETIME)")
	db.Exec("CREATE TABLE `model_group_items` (`id` INTEGER PRIMARY KEY, `group_id` INTEGER, `model_id` INTEGER, `position` INTEGER)")
	db.Exec("CREATE TABLE `request_logs` (`id` INTEGER PRIMARY KEY)")
	db.Exec("CREATE TABLE `settings` (`id` INTEGER PRIMARY KEY, `key` TEXT, `value` TEXT)")
	return db
}

func TestMigrateLegacyJudgeFromIsJudge(t *testing.T) {
	db := openLegacyDB(t)
	db.Exec("INSERT INTO providers (id, name, enabled, created_at) VALUES (1, 'p', 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (1, 'judge-mini', 1, 1, 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (2, 'gpt-4o', 1, 1, 0, datetime())")
	db.Exec("INSERT INTO routing_configs (id, judge_model_id, judge_group_id) VALUES (1, 2, NULL)")

	err := migrateLegacyJudge(db)
	assert.NoError(t, err)

	// Judge queue created with the legacy judge model.
	var g ModelGroup
	assert.NoError(t, db.Where("name = ?", "judge").First(&g).Error)
	assert.Equal(t, "migrated from legacy judge model", g.Remark)

	// judge_group_id written.
	var rc RoutingConfig
	assert.NoError(t, db.First(&rc, 1).Error)
	if assert.NotNil(t, rc.JudgeGroupID) {
		assert.Equal(t, g.ID, *rc.JudgeGroupID)
	}

	// Queue contains the legacy judge model (is_judge=true winner, id=1).
	var item ModelGroupItem
	assert.NoError(t, db.Where("group_id = ?", g.ID).First(&item).Error)
	assert.Equal(t, uint(1), item.ModelID)

	// Legacy columns dropped.
	assert.False(t, db.Migrator().HasColumn(&Model{}, "is_judge"))
	assert.False(t, db.Migrator().HasColumn(&RoutingConfig{}, "judge_model_id"))
}

func TestMigrateLegacyJudgeIdempotent(t *testing.T) {
	db := openLegacyDB(t)
	db.Exec("INSERT INTO providers (id, name, enabled, created_at) VALUES (1, 'p', 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (1, 'judge-mini', 1, 1, 1, datetime())")
	db.Exec("INSERT INTO routing_configs (id, judge_model_id, judge_group_id) VALUES (1, 1, NULL)")

	assert.NoError(t, migrateLegacyJudge(db))
	// Second run: both legacy columns gone -> early return, no error, no side effects.
	assert.NoError(t, migrateLegacyJudge(db))

	var n int64
	db.Model(&ModelGroup{}).Where("name = ?", "judge").Count(&n)
	assert.Equal(t, int64(1), n) // still exactly one judge queue
}

func TestMigrateLegacyJudgeFallsBackToJudgeModelID(t *testing.T) {
	// is_judge column exists but no row has is_judge=true; fall back to judge_model_id.
	db := openLegacyDB(t)
	db.Exec("INSERT INTO providers (id, name, enabled, created_at) VALUES (1, 'p', 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (2, 'gpt-4o', 1, 1, 0, datetime())")
	db.Exec("INSERT INTO routing_configs (id, judge_model_id, judge_group_id) VALUES (1, 2, NULL)")

	err := migrateLegacyJudge(db)
	assert.NoError(t, err)

	var g ModelGroup
	assert.NoError(t, db.Where("name = ?", "judge").First(&g).Error)
	var item ModelGroupItem
	assert.NoError(t, db.Where("group_id = ?", g.ID).First(&item).Error)
	assert.Equal(t, uint(2), item.ModelID) // fell back to judge_model_id=2
}

func TestMigrateLegacyColumns(t *testing.T) {
	// Build a DB in the state AutoMigrate leaves behind on an OLD database:
	// new columns (remark) added, but removed columns (display_name, description)
	// still linger because GORM AutoMigrate never drops columns.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.Exec("CREATE TABLE `models` (`id` INTEGER PRIMARY KEY, `name` TEXT, `display_name` TEXT NOT NULL, `provider_id` INTEGER, `description` TEXT, `enabled` INTEGER, `created_at` DATETIME)")
	db.Exec("CREATE TABLE `model_groups` (`id` INTEGER PRIMARY KEY, `name` TEXT UNIQUE, `display_name` TEXT NOT NULL, `description` TEXT, `remark` TEXT, `enabled` INTEGER, `created_at` DATETIME)")

	// Sanity: INSERT without display_name fails before migration (NOT NULL).
	err = db.Exec("INSERT INTO `model_groups` (`name`, `enabled`, `created_at`) VALUES ('x', 1, datetime())").Error
	assert.Error(t, err)

	assert.NoError(t, migrateLegacyColumns(db))

	// Legacy columns dropped.
	assert.False(t, db.Migrator().HasColumn(&ModelGroup{}, "display_name"))
	assert.False(t, db.Migrator().HasColumn(&ModelGroup{}, "description"))
	assert.False(t, db.Migrator().HasColumn(&Model{}, "display_name"))

	// After migration, INSERT via the current (slim) struct succeeds.
	g := ModelGroup{Name: "test-group", Enabled: true}
	assert.NoError(t, db.Create(&g).Error)

	// Idempotent: second run is a no-op.
	assert.NoError(t, migrateLegacyColumns(db))
}

func TestMigrateLegacyJudgeNoOpWhenNoLegacy(t *testing.T) {
	// Neither legacy column present -> no-op.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&Model{}, &RoutingConfig{}, &ModelGroup{}, &ModelGroupItem{}))
	db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1})

	assert.NoError(t, migrateLegacyJudge(db))
	assert.False(t, db.Migrator().HasColumn(&Model{}, "is_judge"))
	assert.False(t, db.Migrator().HasColumn(&RoutingConfig{}, "judge_model_id"))

	var n int64
	db.Model(&ModelGroup{}).Count(&n)
	assert.Equal(t, int64(0), n) // migration must not create any group when there's no legacy config
}
