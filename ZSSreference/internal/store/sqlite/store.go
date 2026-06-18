// SQLite持久化实现：GORM操作+CRUD方法
//
// 本文件是 SimulSpeak 的数据持久层，使用 GORM + SQLite 记录同传业务的全部关键数据：
//   - 同传会话（interpret_sessions）
//   - AI Provider 信息（providers）
//   - ASR 识别回调记录（asr_callbacks）
//   - 机器翻译记录（mt_translations）
//   - LLM 纠错记录（llm_revisions）
//
// 所有写入操作均通过参数校验保证数据完整性，支持缺失数据库文件自动创建父目录。
package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gormsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DefaultDSN 默认 SQLite 数据源名称：WAL 模式 + 5s 忙等待超时。
const DefaultDSN = "file:./data/simulspeak.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

// ErrInvalidRecord 表示传入的记录参数不符合业务约束（必填字段为空等）。
var ErrInvalidRecord = errors.New("invalid sqlite store record")

// InitializationStatus 数据库初始化状态：是否已初始化 + 缺失的表和索引列表。
type InitializationStatus struct {
	Initialized    bool
	MissingTables  []string
	MissingIndexes []string
}

// InitializationResult 初始化结果：是否已初始化 + 是否执行了迁移 + 最终缺失项。
type InitializationResult struct {
	AlreadyInitialized bool
	Migrated           bool
	MissingTables      []string
	MissingIndexes     []string
}

type SessionListQuery struct {
	TenantID string
	State    string
	Limit    int
	Offset   int
}

type SessionListResult struct {
	Sessions []InterpretSession
	Total    int64
	Limit    int
	Offset   int
}

type SessionDetail struct {
	Session        InterpretSession
	ASRCallbacks   []ASRCallback
	MTTranslations []MTTranslationRecord
	LLMRevisions   []LLMRevisionRecord
}

const (
	VocabularyTaskStatusPending   = "pending"
	VocabularyTaskStatusRunning   = "running"
	VocabularyTaskStatusSucceeded = "succeeded"
	VocabularyTaskStatusFailed    = "failed"
	VocabularyTaskStatusCancelled = "cancelled"

	DefaultVocabularyMaxWords = 30
	MaxVocabularyMaxWords     = 100
	DefaultVocabularySource   = "auto"
)

type VocabularyTaskListQuery struct {
	SessionID string
	Status    string
	Limit     int
	Offset    int
}

type VocabularyTaskListResult struct {
	Tasks  []VocabularyTask
	Total  int64
	Limit  int
	Offset int
}

type VocabularyTaskDetail struct {
	Task    VocabularyTask
	Entries []VocabularyEntry
}

// Store SQLite 持久化存储：包装 GORM DB 实例，提供业务数据的 CRUD 操作。
type Store struct {
	db *gorm.DB
}

// Open 打开 SQLite 数据库连接。DSN 为空时使用默认路径，自动创建数据目录。
// 返回已建立连接的 Store 实例。
func Open(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		dsn = DefaultDSN
	}
	// 确保 SQLite 文件父目录存在（如 ./data/）
	if err := ensureSQLiteParentDir(dsn); err != nil {
		return nil, err
	}

	db, err := gorm.Open(gormsqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}
	return New(db), nil
}

// New 基于已有的 GORM DB 实例创建 Store（用于测试注入或自定义配置）。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// DB 返回底层 GORM DB 实例，供需要直接 GORM 操作的场景使用。
func (s *Store) DB() *gorm.DB {
	return s.db
}

// Close 关闭底层 SQLite 数据库连接。
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Migrate 执行数据库 Schema 迁移：创建业务表和索引。
// 幂等操作，多次调用不会重复创建。
func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("migrate sqlite store: %w", ErrInvalidRecord)
	}
	// 依次执行建表语句和建索引语句
	if err := executeSQL(ctx, s.db, SchemaSQL); err != nil {
		return err
	}
	if err := executeSQL(ctx, s.db, IndexesSQL); err != nil {
		return err
	}
	return nil
}

// InitializationStatus 检查数据库是否已完整初始化：验证所有必需表和索引是否存在。
// 返回缺失表名列表和缺失索引名列表。
func (s *Store) InitializationStatus(ctx context.Context) (InitializationStatus, error) {
	if s == nil || s.db == nil {
		return InitializationStatus{}, fmt.Errorf("inspect sqlite store: %w", ErrInvalidRecord)
	}

	// 检查必需表
	missingTables, err := s.missingSQLiteObjects(ctx, "table", RequiredTables)
	if err != nil {
		return InitializationStatus{}, err
	}
	// 检查必需索引
	missingIndexes, err := s.missingSQLiteObjects(ctx, "index", RequiredIndexes)
	if err != nil {
		return InitializationStatus{}, err
	}

	return InitializationStatus{
		Initialized:    len(missingTables) == 0 && len(missingIndexes) == 0,
		MissingTables:  missingTables,
		MissingIndexes: missingIndexes,
	}, nil
}

// EnsureInitialized 确保数据库已初始化：先检查状态，未初始化则执行迁移，迁移后再次验证。
// 这是服务启动时调用的标准初始化入口。
func (s *Store) EnsureInitialized(ctx context.Context) (InitializationResult, error) {
	status, err := s.InitializationStatus(ctx)
	if err != nil {
		return InitializationResult{}, err
	}
	result := InitializationResult{
		AlreadyInitialized: status.Initialized,
		MissingTables:      status.MissingTables,
		MissingIndexes:     status.MissingIndexes,
	}
	if status.Initialized {
		return result, nil
	}

	// 执行迁移
	if err := s.Migrate(ctx); err != nil {
		return result, err
	}

	// 迁移后再次验证
	after, err := s.InitializationStatus(ctx)
	if err != nil {
		return result, err
	}
	if !after.Initialized {
		return result, fmt.Errorf("sqlite store initialization incomplete: missing tables=%v indexes=%v", after.MissingTables, after.MissingIndexes)
	}
	result.Migrated = true
	return result, nil
}

// UpsertProvider 插入或更新 AI Provider 记录（按 name+capability 唯一约束）。
// 返回 provider 的数据库主键 ID，供后续关联使用。
func (s *Store) UpsertProvider(ctx context.Context, provider Provider) (int64, error) {
	provider.Name = strings.TrimSpace(provider.Name)
	provider.Capability = strings.TrimSpace(provider.Capability)
	if provider.Name == "" || provider.Capability == "" {
		return 0, fmt.Errorf("%w: provider name and capability are required", ErrInvalidRecord)
	}

	now := nowText()
	if provider.CreatedAt == "" {
		provider.CreatedAt = now
	}
	provider.UpdatedAt = now
	if provider.Enabled == 0 {
		provider.Enabled = 1
	}

	// ON CONFLICT 时更新除 name/capability 外的所有字段
	updates := map[string]any{
		"vendor":        provider.Vendor,
		"endpoint_url":  provider.EndpointURL,
		"model":         provider.Model,
		"api_key_ref":   provider.APIKeyRef,
		"enabled":       provider.Enabled,
		"is_default":    provider.IsDefault,
		"config_json":   provider.ConfigJSON,
		"metadata_json": provider.MetadataJSON,
		"updated_at":    provider.UpdatedAt,
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "name"},
			{Name: "capability"},
		},
		DoUpdates: clause.Assignments(updates),
	}).Create(&provider).Error
	if err != nil {
		return 0, fmt.Errorf("upsert provider: %w", err)
	}

	// 回读获取自动生成的 ID
	var stored Provider
	if err := s.db.WithContext(ctx).
		Where("name = ? AND capability = ?", provider.Name, provider.Capability).
		Take(&stored).Error; err != nil {
		return 0, fmt.Errorf("load provider id: %w", err)
	}
	return stored.ID, nil
}

// CreateSession 创建一条同传会话记录，包含会话ID、租户、翻译策略、配音开关等核心字段。
func (s *Store) CreateSession(ctx context.Context, session InterpretSession) error {
	session.ID = strings.TrimSpace(session.ID)
	session.TenantID = strings.TrimSpace(session.TenantID)
	if session.ID == "" || session.TenantID == "" {
		return fmt.Errorf("%w: session id and tenant id are required", ErrInvalidRecord)
	}

	now := nowText()
	// 填充默认值
	if session.State == "" {
		session.State = "active"
	}
	if session.ProviderIDsJSON == "" {
		session.ProviderIDsJSON = "{}"
	}
	if session.TranslateStrategy == "" {
		session.TranslateStrategy = "tmt"
	}
	if session.StartedAt == "" {
		session.StartedAt = now
	}
	if session.CreatedAt == "" {
		session.CreatedAt = now
	}
	session.UpdatedAt = now

	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return fmt.Errorf("create interpret session: %w", err)
	}
	return nil
}

// EndSession 将同传会话标记为结束，并记录 ended_at；重复调用不会覆盖首次结束时间。
func (s *Store) EndSession(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidRecord)
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session InterpretSession
		if err := tx.Where("id = ?", id).Take(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("load interpret session: %w", err)
		}
		endedAt := session.EndedAt
		if strings.TrimSpace(endedAt) == "" {
			endedAt = nowText()
		}
		if err := tx.Model(&InterpretSession{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"state":       "ended",
				"media_state": "ended",
				"ended_at":    endedAt,
				"updated_at":  nowText(),
			}).Error; err != nil {
			return fmt.Errorf("end interpret session: %w", err)
		}
		return nil
	})
}

// AddProviderToSession 将 AI Provider 关联到指定同传会话（多对多关系）。
// 使用事务保证原子性：读取→追加→更新 provider_ids_json。
func (s *Store) AddProviderToSession(ctx context.Context, sessionID, capability string, providerID int64) error {
	sessionID = strings.TrimSpace(sessionID)
	capability = strings.TrimSpace(capability)
	if sessionID == "" || capability == "" || providerID <= 0 {
		return fmt.Errorf("%w: session id, capability, and provider id are required", ErrInvalidRecord)
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 读取当前会话
		var session InterpretSession
		if err := tx.Where("id = ?", sessionID).Take(&session).Error; err != nil {
			return fmt.Errorf("load interpret session: %w", err)
		}

		// 解析已有 provider 映射
		providerIDs, err := decodeProviderIDs(session.ProviderIDsJSON)
		if err != nil {
			return err
		}
		// 避免重复关联
		existing := providerIDs[capability]
		for _, id := range existing {
			if id == providerID {
				return nil
			}
		}
		providerIDs[capability] = append(existing, providerID)

		data, err := json.Marshal(providerIDs)
		if err != nil {
			return fmt.Errorf("encode session provider ids: %w", err)
		}

		return tx.Model(&InterpretSession{}).
			Where("id = ?", sessionID).
			Updates(map[string]any{
				"provider_ids_json": string(data),
				"updated_at":        nowText(),
			}).Error
	})
}

// InsertASRCallback 记录一次 ASR 识别回调（partial 或 final），返回记录 ID。
// 此记录用于追溯每句话的识别历史，以及关联后续的翻译和纠错记录。
func (s *Store) InsertASRCallback(ctx context.Context, callback ASRCallback) (int64, error) {
	callback.SessionID = strings.TrimSpace(callback.SessionID)
	callback.CallID = strings.TrimSpace(callback.CallID)
	callback.UtteranceID = strings.TrimSpace(callback.UtteranceID)
	if callback.SessionID == "" || callback.CallID == "" || callback.UtteranceID == "" || callback.Text == "" {
		return 0, fmt.Errorf("%w: asr session id, call id, utterance id, and text are required", ErrInvalidRecord)
	}
	if callback.ReceivedAt == "" {
		callback.ReceivedAt = nowText()
	}

	if err := s.db.WithContext(ctx).Create(&callback).Error; err != nil {
		return 0, fmt.Errorf("insert asr callback: %w", err)
	}
	return callback.ID, nil
}

// InsertMTTranslation 记录一次机器翻译（TMT）结果，关联到对应的 ASR 回调记录。
// 包含原文、译文、是否为 final、状态等信息，用于翻译质量追溯。
func (s *Store) InsertMTTranslation(ctx context.Context, record MTTranslationRecord) (int64, error) {
	if record.ASRCallbackID <= 0 || strings.TrimSpace(record.SourceText) == "" {
		return 0, fmt.Errorf("%w: mt asr callback id and source text are required", ErrInvalidRecord)
	}
	// 填充默认值
	if record.ASRPhase == "" {
		record.ASRPhase = "partial"
	}
	if record.SourceLang == "" {
		record.SourceLang = "en"
	}
	if record.TargetLang == "" {
		record.TargetLang = "zh"
	}
	if record.Status == "" {
		record.Status = "ok"
	}
	if record.RequestedAt == "" {
		record.RequestedAt = nowText()
	}

	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return 0, fmt.Errorf("insert mt translation record: %w", err)
	}
	return record.ID, nil
}

// InsertLLMRevision 记录一次 LLM（DeepSeek Flash）纠错结果，关联到对应的 ASR 回调记录。
// 包含纠错前后文本、是否 revised、耗时、上下文、术语表等信息。
func (s *Store) InsertLLMRevision(ctx context.Context, record LLMRevisionRecord) (int64, error) {
	if record.ASRCallbackID <= 0 || strings.TrimSpace(record.SourceText) == "" {
		return 0, fmt.Errorf("%w: llm asr callback id and source text are required", ErrInvalidRecord)
	}
	if record.Status == "" {
		record.Status = "ok"
	}
	if record.RequestedAt == "" {
		record.RequestedAt = nowText()
	}

	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return 0, fmt.Errorf("insert llm revision record: %w", err)
	}
	return record.ID, nil
}

// Session 按 ID 查询单条同传会话记录。
func (s *Store) Session(ctx context.Context, id string) (InterpretSession, error) {
	var session InterpretSession
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&session).Error
	return session, err
}

// ListSessions 按租户/状态分页查询历史同传会话。
func (s *Store) ListSessions(ctx context.Context, query SessionListQuery) (SessionListResult, error) {
	if s == nil || s.db == nil {
		return SessionListResult{}, fmt.Errorf("list interpret sessions: %w", ErrInvalidRecord)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	db := s.db.WithContext(ctx).Model(&InterpretSession{})
	if tenantID := strings.TrimSpace(query.TenantID); tenantID != "" {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if state := strings.TrimSpace(query.State); state != "" {
		db = db.Where("state = ?", state)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return SessionListResult{}, fmt.Errorf("count interpret sessions: %w", err)
	}
	var sessions []InterpretSession
	if err := db.Order("started_at DESC").Order("id DESC").Limit(limit).Offset(offset).Find(&sessions).Error; err != nil {
		return SessionListResult{}, fmt.Errorf("list interpret sessions: %w", err)
	}
	return SessionListResult{Sessions: sessions, Total: total, Limit: limit, Offset: offset}, nil
}

// SessionDetail 查询单次同传会话及其 ASR/TMT/LLM 明细。
func (s *Store) SessionDetail(ctx context.Context, id string) (SessionDetail, error) {
	session, err := s.Session(ctx, id)
	if err != nil {
		return SessionDetail{}, err
	}
	var asrCallbacks []ASRCallback
	if err := s.db.WithContext(ctx).
		Where("session_id = ? AND is_final = ?", id, 1).
		Order("sequence_no ASC").
		Order("id ASC").
		Find(&asrCallbacks).Error; err != nil {
		return SessionDetail{}, fmt.Errorf("list asr callbacks: %w", err)
	}
	asrIDs := make([]int64, 0, len(asrCallbacks))
	for _, callback := range asrCallbacks {
		if callback.ID > 0 {
			asrIDs = append(asrIDs, callback.ID)
		}
	}

	detail := SessionDetail{Session: session, ASRCallbacks: asrCallbacks}
	if len(asrIDs) == 0 {
		return detail, nil
	}
	if err := s.db.WithContext(ctx).
		Where("asr_callback_id IN ?", asrIDs).
		Order("requested_at ASC").
		Order("id ASC").
		Find(&detail.MTTranslations).Error; err != nil {
		return SessionDetail{}, fmt.Errorf("list mt translation records: %w", err)
	}
	if err := s.db.WithContext(ctx).
		Where("asr_callback_id IN ?", asrIDs).
		Order("requested_at ASC").
		Order("id ASC").
		Find(&detail.LLMRevisions).Error; err != nil {
		return SessionDetail{}, fmt.Errorf("list llm revision records: %w", err)
	}
	return detail, nil
}

// CreateVocabularyTask 创建一条单词本任务，并取消同会话尚未消费的旧 pending 任务。
func (s *Store) CreateVocabularyTask(ctx context.Context, task VocabularyTask) (VocabularyTask, error) {
	if s == nil || s.db == nil {
		return VocabularyTask{}, fmt.Errorf("create vocabulary task: %w", ErrInvalidRecord)
	}
	task.ID = strings.TrimSpace(task.ID)
	task.SessionID = strings.TrimSpace(task.SessionID)
	if task.ID == "" || task.SessionID == "" {
		return VocabularyTask{}, fmt.Errorf("%w: vocabulary task id and session id are required", ErrInvalidRecord)
	}
	if task.MaxWords <= 0 {
		task.MaxWords = DefaultVocabularyMaxWords
	}
	if task.MaxWords > MaxVocabularyMaxWords {
		task.MaxWords = MaxVocabularyMaxWords
	}
	if strings.TrimSpace(task.EnglishSource) == "" {
		task.EnglishSource = DefaultVocabularySource
	}
	if strings.TrimSpace(task.Status) == "" {
		task.Status = VocabularyTaskStatusPending
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session InterpretSession
		if err := tx.Where("id = ?", task.SessionID).Take(&session).Error; err != nil {
			return fmt.Errorf("load interpret session: %w", err)
		}

		now := nowText()
		task.TenantID = strings.TrimSpace(session.TenantID)
		task.UserID = strings.TrimSpace(session.UserID)
		task.PartitionKey = VocabularyPartitionKey(task.TenantID, task.UserID, task.SessionID)
		task.CreatedAt = firstText(task.CreatedAt, now)
		task.UpdatedAt = now

		if err := tx.Model(&VocabularyTask{}).
			Where("session_id = ? AND status = ?", task.SessionID, VocabularyTaskStatusPending).
			Updates(map[string]any{
				"status":        VocabularyTaskStatusCancelled,
				"finished_at":   now,
				"error_message": "superseded by " + task.ID,
				"updated_at":    now,
			}).Error; err != nil {
			return fmt.Errorf("cancel superseded vocabulary tasks: %w", err)
		}
		if err := tx.Create(&task).Error; err != nil {
			return fmt.Errorf("create vocabulary task: %w", err)
		}
		return nil
	})
	if err != nil {
		return VocabularyTask{}, err
	}
	return task, nil
}

// ListVocabularyTasks 按会话和状态分页查询单词本任务。
func (s *Store) ListVocabularyTasks(ctx context.Context, query VocabularyTaskListQuery) (VocabularyTaskListResult, error) {
	if s == nil || s.db == nil {
		return VocabularyTaskListResult{}, fmt.Errorf("list vocabulary tasks: %w", ErrInvalidRecord)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	db := s.db.WithContext(ctx).Model(&VocabularyTask{})
	if sessionID := strings.TrimSpace(query.SessionID); sessionID != "" {
		db = db.Where("session_id = ?", sessionID)
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return VocabularyTaskListResult{}, fmt.Errorf("count vocabulary tasks: %w", err)
	}
	var tasks []VocabularyTask
	if err := db.Order("created_at DESC").Order("id DESC").Limit(limit).Offset(offset).Find(&tasks).Error; err != nil {
		return VocabularyTaskListResult{}, fmt.Errorf("list vocabulary tasks: %w", err)
	}
	return VocabularyTaskListResult{Tasks: tasks, Total: total, Limit: limit, Offset: offset}, nil
}

// VocabularyTaskDetail 查询单词本任务和结果词条。
func (s *Store) VocabularyTaskDetail(ctx context.Context, id string) (VocabularyTaskDetail, error) {
	if s == nil || s.db == nil {
		return VocabularyTaskDetail{}, fmt.Errorf("load vocabulary task detail: %w", ErrInvalidRecord)
	}
	id = strings.TrimSpace(id)
	var task VocabularyTask
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&task).Error; err != nil {
		return VocabularyTaskDetail{}, err
	}
	var entries []VocabularyEntry
	if err := s.db.WithContext(ctx).
		Where("task_id = ?", id).
		Order("ordinal ASC").
		Order("id ASC").
		Find(&entries).Error; err != nil {
		return VocabularyTaskDetail{}, fmt.Errorf("list vocabulary entries: %w", err)
	}
	return VocabularyTaskDetail{Task: task, Entries: entries}, nil
}

// ClaimNextVocabularyTask 原子领取一条可消费任务。同一 partition_key 内保持顺序。
func (s *Store) ClaimNextVocabularyTask(ctx context.Context, workerID string, maxAttempts int) (VocabularyTask, bool, error) {
	if s == nil || s.db == nil {
		return VocabularyTask{}, false, fmt.Errorf("claim vocabulary task: %w", ErrInvalidRecord)
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return VocabularyTask{}, false, fmt.Errorf("%w: worker id is required", ErrInvalidRecord)
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	var candidate VocabularyTask
	if err := s.db.WithContext(ctx).Raw(`
SELECT *
FROM vocabulary_tasks AS t
WHERE t.status = ?
  AND t.attempt_count < ?
  AND NOT EXISTS (
    SELECT 1
    FROM vocabulary_tasks AS earlier
    WHERE earlier.partition_key = t.partition_key
      AND earlier.status IN (?, ?)
      AND (
        earlier.created_at < t.created_at
        OR (earlier.created_at = t.created_at AND earlier.id < t.id)
      )
  )
ORDER BY t.created_at ASC, t.id ASC
LIMIT 1
`, VocabularyTaskStatusPending, maxAttempts, VocabularyTaskStatusPending, VocabularyTaskStatusRunning).Scan(&candidate).Error; err != nil {
		return VocabularyTask{}, false, fmt.Errorf("select claimable vocabulary task: %w", err)
	}
	if candidate.ID == "" {
		return VocabularyTask{}, false, nil
	}

	now := nowText()
	updates := map[string]any{
		"status":        VocabularyTaskStatusRunning,
		"locked_by":     workerID,
		"locked_at":     now,
		"attempt_count": gorm.Expr("attempt_count + 1"),
		"error_message": "",
		"updated_at":    now,
	}
	if strings.TrimSpace(candidate.StartedAt) == "" {
		updates["started_at"] = now
	}
	result := s.db.WithContext(ctx).Model(&VocabularyTask{}).
		Where("id = ? AND status = ?", candidate.ID, VocabularyTaskStatusPending).
		Updates(updates)
	if result.Error != nil {
		return VocabularyTask{}, false, fmt.Errorf("claim vocabulary task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return VocabularyTask{}, false, nil
	}

	var claimed VocabularyTask
	if err := s.db.WithContext(ctx).Where("id = ?", candidate.ID).Take(&claimed).Error; err != nil {
		return VocabularyTask{}, false, fmt.Errorf("load claimed vocabulary task: %w", err)
	}
	return claimed, claimed.ID != "", nil
}

// CompleteVocabularyTask 写入词条并提交任务完成状态。
func (s *Store) CompleteVocabularyTask(ctx context.Context, taskID string, entries []VocabularyEntry, inputJSON, rawRequestJSON, rawResponseJSON string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("complete vocabulary task: %w", ErrInvalidRecord)
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("%w: task id is required", ErrInvalidRecord)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).Delete(&VocabularyEntry{}).Error; err != nil {
			return fmt.Errorf("clear vocabulary entries: %w", err)
		}
		now := nowText()
		for i := range entries {
			entries[i].TaskID = taskID
			if entries[i].Ordinal <= 0 {
				entries[i].Ordinal = i + 1
			}
			entries[i].Word = strings.TrimSpace(entries[i].Word)
			if entries[i].Word == "" {
				continue
			}
			if entries[i].CreatedAt == "" {
				entries[i].CreatedAt = now
			}
			if err := tx.Create(&entries[i]).Error; err != nil {
				return fmt.Errorf("insert vocabulary entry: %w", err)
			}
		}
		result := tx.Model(&VocabularyTask{}).
			Where("id = ? AND status = ?", taskID, VocabularyTaskStatusRunning).
			Updates(map[string]any{
				"status":            VocabularyTaskStatusSucceeded,
				"finished_at":       now,
				"locked_by":         "",
				"locked_at":         "",
				"error_message":     "",
				"input_json":        inputJSON,
				"raw_request_json":  rawRequestJSON,
				"raw_response_json": rawResponseJSON,
				"updated_at":        now,
			})
		if result.Error != nil {
			return fmt.Errorf("complete vocabulary task: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: vocabulary task is not running", ErrInvalidRecord)
		}
		return nil
	})
}

// FailVocabularyTask 标记任务失败；未达到最大重试次数时重新入队。
func (s *Store) FailVocabularyTask(ctx context.Context, taskID, message string, maxAttempts int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("fail vocabulary task: %w", ErrInvalidRecord)
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("%w: task id is required", ErrInvalidRecord)
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task VocabularyTask
		if err := tx.Where("id = ?", taskID).Take(&task).Error; err != nil {
			return fmt.Errorf("load vocabulary task: %w", err)
		}
		now := nowText()
		status := VocabularyTaskStatusPending
		finishedAt := ""
		if task.AttemptCount >= maxAttempts {
			status = VocabularyTaskStatusFailed
			finishedAt = now
		}
		if err := tx.Model(&VocabularyTask{}).
			Where("id = ?", taskID).
			Updates(map[string]any{
				"status":        status,
				"finished_at":   finishedAt,
				"locked_by":     "",
				"locked_at":     "",
				"error_message": strings.TrimSpace(message),
				"updated_at":    now,
			}).Error; err != nil {
			return fmt.Errorf("fail vocabulary task: %w", err)
		}
		return nil
	})
}

// ResetStaleVocabularyTasks 恢复锁超时的 running 任务；超过最大尝试次数的任务置为 failed。
func (s *Store) ResetStaleVocabularyTasks(ctx context.Context, staleBefore time.Time, maxAttempts int) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("reset stale vocabulary tasks: %w", ErrInvalidRecord)
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	cutoff := staleBefore.UTC().Format(time.RFC3339Nano)
	now := nowText()
	var total int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		failed := tx.Model(&VocabularyTask{}).
			Where("status = ? AND locked_at != '' AND locked_at < ? AND attempt_count >= ?", VocabularyTaskStatusRunning, cutoff, maxAttempts).
			Updates(map[string]any{
				"status":        VocabularyTaskStatusFailed,
				"finished_at":   now,
				"locked_by":     "",
				"locked_at":     "",
				"error_message": "task lock expired",
				"updated_at":    now,
			})
		if failed.Error != nil {
			return fmt.Errorf("fail stale vocabulary tasks: %w", failed.Error)
		}
		total += failed.RowsAffected
		pending := tx.Model(&VocabularyTask{}).
			Where("status = ? AND locked_at != '' AND locked_at < ? AND attempt_count < ?", VocabularyTaskStatusRunning, cutoff, maxAttempts).
			Updates(map[string]any{
				"status":        VocabularyTaskStatusPending,
				"locked_by":     "",
				"locked_at":     "",
				"error_message": "task lock expired; retry pending",
				"updated_at":    now,
			})
		if pending.Error != nil {
			return fmt.Errorf("reset stale vocabulary tasks: %w", pending.Error)
		}
		total += pending.RowsAffected
		return nil
	}); err != nil {
		return 0, err
	}
	return total, nil
}

func VocabularyPartitionKey(tenantID, userID, sessionID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "default"
	}
	userID = strings.TrimSpace(userID)
	if userID != "" {
		return tenantID + ":" + userID
	}
	return tenantID + ":session:" + strings.TrimSpace(sessionID)
}

// ASRCallback 按 ID 查询单条 ASR 回调记录。
func (s *Store) ASRCallback(ctx context.Context, id int64) (ASRCallback, error) {
	var callback ASRCallback
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&callback).Error
	return callback, err
}

// executeSQL 逐条执行以分号分隔的 SQL 语句（用于 Schema 迁移）。
func executeSQL(ctx context.Context, db *gorm.DB, sqlText string) error {
	statements := strings.Split(sqlText, ";")
	for i, statement := range statements {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := db.WithContext(ctx).Exec(statement).Error; err != nil {
			return fmt.Errorf("execute sqlite migration statement %d: %w", i+1, err)
		}
	}
	return nil
}

// missingSQLiteObjects 检查 sqlite_master 中缺失的指定类型对象（表/索引）。
func (s *Store) missingSQLiteObjects(ctx context.Context, objectType string, required []string) ([]string, error) {
	missing := make([]string, 0)
	for _, name := range required {
		var count int64
		if err := s.db.WithContext(ctx).Raw(
			"SELECT COUNT(1) FROM sqlite_master WHERE type = ? AND name = ?",
			objectType,
			name,
		).Scan(&count).Error; err != nil {
			return nil, fmt.Errorf("inspect sqlite %s %s: %w", objectType, name, err)
		}
		if count == 0 {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

// decodeProviderIDs 解析 JSON 编码的 provider 映射（capability → providerID[]）。
func decodeProviderIDs(raw string) (map[string][]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string][]int64{}, nil
	}
	var providerIDs map[string][]int64
	if err := json.Unmarshal([]byte(raw), &providerIDs); err != nil {
		return nil, fmt.Errorf("decode session provider ids: %w", err)
	}
	if providerIDs == nil {
		return map[string][]int64{}, nil
	}
	return providerIDs, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ensureSQLiteParentDir 根据 DSN 提取文件路径，确保其父目录存在。
func ensureSQLiteParentDir(dsn string) error {
	path := sqliteFilePath(dsn)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite data directory %s: %w", dir, err)
	}
	return nil
}

// sqliteFilePath 从 DSN 字符串中提取文件路径（去掉 file: 前缀和查询参数）。
func sqliteFilePath(dsn string) string {
	raw := strings.TrimSpace(dsn)
	if raw == "" || raw == ":memory:" || strings.Contains(raw, "mode=memory") {
		return ""
	}
	raw = strings.TrimPrefix(raw, "file:")
	if idx := strings.Index(raw, "?"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, ":") {
		return ""
	}
	return filepath.Clean(raw)
}

// nowText 返回当前 UTC 时间的 RFC3339 纳秒级字符串表示。
func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
