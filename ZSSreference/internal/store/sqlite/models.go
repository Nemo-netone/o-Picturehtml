// SQLite GORM模型定义：同传会话+ASR回调+翻译+纠错记录表结构
package sqlite

type Provider struct {
	ID           int64  `gorm:"column:id;primaryKey"`
	Name         string `gorm:"column:name"`
	Capability   string `gorm:"column:capability"`
	Vendor       string `gorm:"column:vendor"`
	EndpointURL  string `gorm:"column:endpoint_url"`
	Model        string `gorm:"column:model"`
	APIKeyRef    string `gorm:"column:api_key_ref"`
	Enabled      int    `gorm:"column:enabled"`
	IsDefault    int    `gorm:"column:is_default"`
	ConfigJSON   string `gorm:"column:config_json"`
	MetadataJSON string `gorm:"column:metadata_json"`
	CreatedAt    string `gorm:"column:created_at"`
	UpdatedAt    string `gorm:"column:updated_at"`
}

func (Provider) TableName() string {
	return "providers"
}

type InterpretSession struct {
	ID                string `gorm:"column:id;primaryKey"`
	TenantID          string `gorm:"column:tenant_id"`
	ConnectionID      string `gorm:"column:connection_id"`
	UserID            string `gorm:"column:user_id"`
	Caller            string `gorm:"column:caller"`
	Callee            string `gorm:"column:callee"`
	State             string `gorm:"column:state"`
	MediaState        string `gorm:"column:media_state"`
	ProviderIDsJSON   string `gorm:"column:provider_ids_json"`
	TranslateStrategy string `gorm:"column:translate_strategy"`
	DubbingEnabled    int    `gorm:"column:dubbing_enabled"`
	StartedAt         string `gorm:"column:started_at"`
	EndedAt           string `gorm:"column:ended_at"`
	CreatedAt         string `gorm:"column:created_at"`
	UpdatedAt         string `gorm:"column:updated_at"`
	MetadataJSON      string `gorm:"column:metadata_json"`
}

func (InterpretSession) TableName() string {
	return "interpret_sessions"
}

type ASRCallback struct {
	ID           int64   `gorm:"column:id;primaryKey"`
	SessionID    string  `gorm:"column:session_id"`
	ProviderID   int64   `gorm:"column:provider_id"`
	CallID       string  `gorm:"column:call_id"`
	UtteranceID  string  `gorm:"column:utterance_id"`
	SequenceNo   int64   `gorm:"column:sequence_no"`
	Language     string  `gorm:"column:language"`
	Text         string  `gorm:"column:text"`
	IsFinal      int     `gorm:"column:is_final"`
	Confidence   float64 `gorm:"column:confidence"`
	StartMS      int64   `gorm:"column:start_ms"`
	EndMS        int64   `gorm:"column:end_ms"`
	ReceivedAt   string  `gorm:"column:received_at"`
	MetadataJSON string  `gorm:"column:metadata_json"`
	RawJSON      string  `gorm:"column:raw_json"`
}

func (ASRCallback) TableName() string {
	return "asr_callbacks"
}

type MTTranslationRecord struct {
	ID              int64  `gorm:"column:id;primaryKey"`
	ASRCallbackID   int64  `gorm:"column:asr_callback_id"`
	ProviderID      int64  `gorm:"column:provider_id"`
	ASRPhase        string `gorm:"column:asr_phase"`
	SourceLang      string `gorm:"column:source_lang"`
	TargetLang      string `gorm:"column:target_lang"`
	SourceText      string `gorm:"column:source_text"`
	TargetText      string `gorm:"column:target_text"`
	IsFinal         int    `gorm:"column:is_final"`
	Status          string `gorm:"column:status"`
	ErrorCode       string `gorm:"column:error_code"`
	ErrorMessage    string `gorm:"column:error_message"`
	LatencyMS       int64  `gorm:"column:latency_ms"`
	RequestedAt     string `gorm:"column:requested_at"`
	RespondedAt     string `gorm:"column:responded_at"`
	MetadataJSON    string `gorm:"column:metadata_json"`
	RawRequestJSON  string `gorm:"column:raw_request_json"`
	RawResponseJSON string `gorm:"column:raw_response_json"`
}

func (MTTranslationRecord) TableName() string {
	return "mt_translation_records"
}

type LLMRevisionRecord struct {
	ID               int64  `gorm:"column:id;primaryKey"`
	ASRCallbackID    int64  `gorm:"column:asr_callback_id"`
	ProviderID       int64  `gorm:"column:provider_id"`
	SourceText       string `gorm:"column:source_text"`
	DraftTranslation string `gorm:"column:draft_translation"`
	RevisedText      string `gorm:"column:revised_text"`
	Revised          int    `gorm:"column:revised"`
	Status           string `gorm:"column:status"`
	ErrorMessage     string `gorm:"column:error_message"`
	LatencyMS        int64  `gorm:"column:latency_ms"`
	RequestedAt      string `gorm:"column:requested_at"`
	RespondedAt      string `gorm:"column:responded_at"`
	ContextJSON      string `gorm:"column:context_json"`
	TermsJSON        string `gorm:"column:terms_json"`
	MetadataJSON     string `gorm:"column:metadata_json"`
	RawRequestJSON   string `gorm:"column:raw_request_json"`
	RawResponseJSON  string `gorm:"column:raw_response_json"`
}

func (LLMRevisionRecord) TableName() string {
	return "llm_revision_records"
}

type VocabularyTask struct {
	ID              string `gorm:"column:id;primaryKey"`
	SessionID       string `gorm:"column:session_id"`
	TenantID        string `gorm:"column:tenant_id"`
	UserID          string `gorm:"column:user_id"`
	PartitionKey    string `gorm:"column:partition_key"`
	Status          string `gorm:"column:status"`
	MaxWords        int    `gorm:"column:max_words"`
	EnglishSource   string `gorm:"column:english_source"`
	AttemptCount    int    `gorm:"column:attempt_count"`
	LockedBy        string `gorm:"column:locked_by"`
	LockedAt        string `gorm:"column:locked_at"`
	StartedAt       string `gorm:"column:started_at"`
	FinishedAt      string `gorm:"column:finished_at"`
	ErrorMessage    string `gorm:"column:error_message"`
	InputJSON       string `gorm:"column:input_json"`
	RawRequestJSON  string `gorm:"column:raw_request_json"`
	RawResponseJSON string `gorm:"column:raw_response_json"`
	CreatedAt       string `gorm:"column:created_at"`
	UpdatedAt       string `gorm:"column:updated_at"`
}

func (VocabularyTask) TableName() string {
	return "vocabulary_tasks"
}

type VocabularyEntry struct {
	ID                     int64  `gorm:"column:id;primaryKey"`
	TaskID                 string `gorm:"column:task_id"`
	Ordinal                int    `gorm:"column:ordinal"`
	Word                   string `gorm:"column:word"`
	Lemma                  string `gorm:"column:lemma"`
	Phonetic               string `gorm:"column:phonetic"`
	PartOfSpeech           string `gorm:"column:part_of_speech"`
	MeaningZH              string `gorm:"column:meaning_zh"`
	ExampleEN              string `gorm:"column:example_en"`
	ExampleZH              string `gorm:"column:example_zh"`
	Occurrences            int    `gorm:"column:occurrences"`
	Difficulty             string `gorm:"column:difficulty"`
	SourceUtteranceIDsJSON string `gorm:"column:source_utterance_ids_json"`
	MetadataJSON           string `gorm:"column:metadata_json"`
	CreatedAt              string `gorm:"column:created_at"`
}

func (VocabularyEntry) TableName() string {
	return "vocabulary_entries"
}
