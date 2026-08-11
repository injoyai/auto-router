package store

import (
	"fmt"

	"gorm.io/gorm"
)

type Store struct {
	DB *gorm.DB
}

// Open 使用给定 Dialer 打开数据库并完成通用初始化
// （AutoMigrate + seed routing_config 单例行 + 遗留判定配置迁移）。
// 驱动特定的初始化（PRAGMA、连接池等）由 Dialer 实现负责。
func Open(dialer Dialer, dsn string) (*Store, error) {
	db, err := dialer.Open(dsn)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &RequestLog{}, &Setting{}, &ModelGroup{}, &ModelGroupItem{}); err != nil {
		return nil, err
	}
	// seed routing_config singleton row
	if err := db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1}).Error; err != nil {
		return nil, err
	}
	if err := migrateLegacyColumns(db); err != nil {
		return nil, err
	}
	if err := migrateLegacyJudge(db); err != nil {
		return nil, err
	}
	// Clean up stale in-progress logs (status=0) left by crashed/interrupted requests.
	db.Where("status = 0").Delete(&RequestLog{})
	return &Store{DB: db}, nil
}

// migrateLegacyColumns drops columns that were removed from structs in prior
// refactors but linger in existing databases because GORM AutoMigrate only
// adds columns, never drops them. NOT NULL legacy columns (e.g. display_name)
// break INSERTs that use the current slimmer struct, so this must run before
// any startup-time INSERT — notably migrateLegacyJudge creating the judge group.
func migrateLegacyColumns(db *gorm.DB) error {
	legacy := []struct {
		model interface{}
		col   string
	}{
		{&ModelGroup{}, "display_name"},
		{&ModelGroup{}, "description"},
		{&Model{}, "display_name"},
	}
	for _, l := range legacy {
		if db.Migrator().HasColumn(l.model, l.col) {
			if err := db.Migrator().DropColumn(l.model, l.col); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateLegacyJudge 把旧"单模型判定"配置迁移为判定队列。
// 旧引擎真正的单一事实源是 models.is_judge=true AND enabled=true
// （routing_configs.judge_model_id 仅由 UpdateRoutingConfig 镜像写入，
// 且 POST /admin/models/:id/judge 只改 is_judge 不改 judge_model_id，
// 两者可能不一致），因此以 is_judge 为首选源，judge_model_id 为回退。
// 流程：
//  1. 检测 models.is_judge 与 routing_configs.judge_model_id 两列是否仍存在；
//     都已不存在 -> 直接返回（幂等）。
//  2. 确定旧判定模型 ID（is_judge 首选，judge_model_id 回退）。
//  3. 若有旧判定模型且当前 judge_group_id 为空：创建名为 judge 的队列
//     （重名则 judge-2/judge-3…），把旧判定模型加入为 Position 0，写回 judge_group_id。
//  4. DropColumn 丢弃两列。
//
// SQLite DROP COLUMN 需 SQLite >= 3.35（2021-03-12），项目所用现代驱动均满足。
func migrateLegacyJudge(db *gorm.DB) error {
	hasModelIsJudge := false
	hasRcJudgeModelID := false
	if cols, err := db.Migrator().ColumnTypes(&Model{}); err == nil {
		for _, c := range cols {
			if c.Name() == "is_judge" {
				hasModelIsJudge = true
			}
		}
	}
	if cols, err := db.Migrator().ColumnTypes(&RoutingConfig{}); err == nil {
		for _, c := range cols {
			if c.Name() == "judge_model_id" {
				hasRcJudgeModelID = true
			}
		}
	}
	if !hasModelIsJudge && !hasRcJudgeModelID {
		return nil // already migrated
	}

	// Determine legacy judge model id (prefer is_judge source of truth).
	var legacyJudgeID *uint
	if hasModelIsJudge {
		var id uint
		if err := db.Raw("SELECT id FROM models WHERE is_judge = ? AND enabled = ? LIMIT 1", true, true).Scan(&id).Error; err == nil && id > 0 {
			legacyJudgeID = &id
		}
	}
	if legacyJudgeID == nil && hasRcJudgeModelID {
		var id uint
		if err := db.Raw("SELECT judge_model_id FROM routing_configs WHERE id = 1").Scan(&id).Error; err == nil && id > 0 {
			legacyJudgeID = &id
		}
	}

	// Read current judge_group_id (column always exists after AutoMigrate).
	var rc RoutingConfig
	if err := db.First(&rc, 1).Error; err == nil {
		// rc.JudgeGroupID may be nil
	}

	if legacyJudgeID != nil && (rc.JudgeGroupID == nil) {
		name := "judge"
		for i := 2; ; i++ {
			var exist ModelGroup
			if err := db.Where("name = ?", name).First(&exist).Error; err != nil {
				break // name available (record not found)
			}
			name = fmt.Sprintf("judge-%d", i)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			g := ModelGroup{Name: name, Remark: "migrated from legacy judge model", Enabled: true}
			if err := tx.Create(&g).Error; err != nil {
				return err
			}
			if err := tx.Create(&ModelGroupItem{GroupID: g.ID, ModelID: *legacyJudgeID, Position: 0}).Error; err != nil {
				return err
			}
			return tx.Model(&RoutingConfig{}).Where("id = ?", 1).Update("judge_group_id", g.ID).Error
		}); err != nil {
			return err
		}
	}

	// Drop legacy columns.
	if hasRcJudgeModelID && db.Migrator().HasColumn(&RoutingConfig{}, "judge_model_id") {
		if err := db.Migrator().DropColumn(&RoutingConfig{}, "judge_model_id"); err != nil {
			return err
		}
	}
	if hasModelIsJudge && db.Migrator().HasColumn(&Model{}, "is_judge") {
		if err := db.Migrator().DropColumn(&Model{}, "is_judge"); err != nil {
			return err
		}
	}
	return nil
}
