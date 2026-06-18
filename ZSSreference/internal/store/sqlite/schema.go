// SQLite数据库Schema：建表语句+索引+迁移
package sqlite

// SchemaSQL is the canonical SQLite schema for interpreter session storage.
// Keep these statements explicit so comments, defaults, and the absence of
// database-level relationship constraints remain visible during review.
const SchemaSQL = `
-- Provider 表。所有云厂商、本地模型、mock provider 都放这里。
CREATE TABLE IF NOT EXISTS providers (
    -- provider ID。
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- provider 名称，例如 tencent-asr、tencent-tmt、openai-compatible、local-whisper。
    name TEXT NOT NULL,

    -- provider 能力：asr / mt / llm / tts / vad。
    capability TEXT NOT NULL,

    -- 厂商或来源：tencent / aliyun / volcengine / openai / deepseek / local / mock。
    vendor TEXT,

    -- endpoint URL、base URL、本地推理服务 URL 或模型路径。
    endpoint_url TEXT,

    -- 模型名；云服务无模型时可为空。
    model TEXT,

    -- API key 引用，不保存明文；例如 env:SIMULSPEAK_LLM_API_KEY。
    api_key_ref TEXT,

    -- 是否启用，0=false，1=true。
    enabled INTEGER NOT NULL DEFAULT 1,

    -- 是否默认 provider，0=false，1=true。
    is_default INTEGER NOT NULL DEFAULT 0,

    -- 通用配置 JSON，例如 language、sampleRate、codec。
    config_json TEXT,

    -- provider 特有配置 JSON，例如 region、projectId、appId、voiceType、promptVersion。
    metadata_json TEXT,

    -- 创建时间。
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 更新时间。
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 同一种能力下 provider 名称唯一。
    UNIQUE(name, capability)
);

-- 一次同传会话。id 直接使用 callId。
CREATE TABLE IF NOT EXISTS interpret_sessions (
    -- callId。
    id TEXT PRIMARY KEY,

    -- 租户 ID。
    tenant_id TEXT NOT NULL,

    -- WebSocket connectionId。
    connection_id TEXT,

    -- 前端/业务用户 ID。
    user_id TEXT,

    -- 主叫。
    caller TEXT,

    -- 被叫。
    callee TEXT,

    -- 会话状态：active / ended / failed。
    state TEXT NOT NULL DEFAULT 'active',

    -- 媒体状态，例如 connected / ended。
    media_state TEXT,

    -- 本次会话使用过的 provider id 集合 JSON。
    -- 推荐格式：{"asr":[1],"mt":[2],"llm":[3],"tts":[4]}
    provider_ids_json TEXT NOT NULL DEFAULT '{}',

    -- 翻译策略：tmt / hybrid / llm。
    translate_strategy TEXT DEFAULT 'tmt',

    -- 是否开启自动配音，0=false，1=true。
    dubbing_enabled INTEGER NOT NULL DEFAULT 0,

    -- 开始时间。
    started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 结束时间。
    ended_at TEXT,

    -- 创建时间。
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 更新时间。
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 会话扩展 metadata，例如客户端版本、切换 provider 历史、调试标记。
    metadata_json TEXT
);

-- ASR 回调记录。partial/final 都存，是字幕链路的句子主轴。
CREATE TABLE IF NOT EXISTS asr_callbacks (
    -- 自增主键；MT 和 LLM 都通过这个 ID 关联。
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- 所属 session id，即 interpret_sessions.id；业务侧维护关联。
    session_id TEXT NOT NULL,

    -- 本次 ASR 使用的 provider id；业务侧维护关联到 providers.id。
    provider_id INTEGER,

    -- 冗余 callId，便于查日志。
    call_id TEXT NOT NULL,

    -- 句子 ID，同一句 partial/final 共用。
    utterance_id TEXT NOT NULL,

    -- 会话内 ASR 回调顺序号。
    sequence_no INTEGER NOT NULL,

    -- 识别语言。
    language TEXT,

    -- ASR 文本。
    text TEXT NOT NULL,

    -- 是否 ASR final，0=partial，1=final。
    is_final INTEGER NOT NULL DEFAULT 0,

    -- ASR 置信度。
    confidence REAL,

    -- 句子开始毫秒。
    start_ms INTEGER,

    -- 句子结束毫秒。
    end_ms INTEGER,

    -- 收到 ASR 回调时间。
    received_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- ASR provider 特有字段。
    metadata_json TEXT,

    -- 原始 ASR 回调 JSON，不含密钥。
    raw_json TEXT
);

-- 机器翻译记录。直接业务侧关联到 asr_callbacks.id。
CREATE TABLE IF NOT EXISTS mt_translation_records (
    -- 自增主键。
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- 触发本次翻译的 asr_callbacks.id；业务侧必须保证存在。
    asr_callback_id INTEGER NOT NULL,

    -- 本次 MT 使用的 provider id；业务侧维护关联到 providers.id。
    provider_id INTEGER,

    -- 触发翻译的 ASR 阶段：partial / final。
    asr_phase TEXT NOT NULL DEFAULT 'partial',

    -- 源语言。
    source_lang TEXT NOT NULL DEFAULT 'en',

    -- 目标语言。
    target_lang TEXT NOT NULL DEFAULT 'zh',

    -- 源文本，通常等于关联 ASR callback 的 text。
    source_text TEXT NOT NULL,

    -- 翻译结果。失败时为空。
    target_text TEXT,

    -- 翻译结果是否用于前端锁定字幕，0=临时字幕，1=锁定字幕。
    is_final INTEGER NOT NULL DEFAULT 0,

    -- 调用状态：ok / error / timeout。
    status TEXT NOT NULL DEFAULT 'ok',

    -- 通用错误码。
    error_code TEXT,

    -- 错误信息。
    error_message TEXT,

    -- 调用耗时，毫秒。
    latency_ms INTEGER,

    -- 请求发起时间。
    requested_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 响应返回时间。
    responded_at TEXT,

    -- MT provider 特有字段，例如 requestId、region、projectId。
    metadata_json TEXT,

    -- 原始请求 JSON，不含密钥、Authorization、签名。
    raw_request_json TEXT,

    -- 原始响应 JSON。
    raw_response_json TEXT
);

-- LLM 校准记录。直接业务侧关联到 asr_callbacks.id。
CREATE TABLE IF NOT EXISTS llm_revision_records (
    -- 自增主键。
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- 触发本次校准的 asr_callbacks.id；业务侧必须保证存在。
    asr_callback_id INTEGER NOT NULL,

    -- 本次 LLM 使用的 provider id；业务侧维护关联到 providers.id。
    provider_id INTEGER,

    -- 当前 ASR final 原文，通常等于关联 ASR callback 的 text。
    source_text TEXT NOT NULL,

    -- 机器翻译草稿，例如 TMT final。
    draft_translation TEXT,

    -- LLM 校准后的译文。
    revised_text TEXT,

    -- 是否相对草稿发生修订，0=false，1=true。
    revised INTEGER NOT NULL DEFAULT 0,

    -- 调用状态：ok / error / timeout。
    status TEXT NOT NULL DEFAULT 'ok',

    -- 错误信息。
    error_message TEXT,

    -- 调用耗时，毫秒。
    latency_ms INTEGER,

    -- 请求发起时间。
    requested_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 响应返回时间。
    responded_at TEXT,

    -- 最近上下文 JSON。
    context_json TEXT,

    -- 术语表 JSON。
    terms_json TEXT,

    -- LLM provider 特有字段，例如 temperature、promptVersion、requestId。
    metadata_json TEXT,

    -- 原始请求 JSON，不含 API key。
    raw_request_json TEXT,

    -- 原始响应 JSON。
    raw_response_json TEXT
);

-- 单词本异步任务。使用 SQLite 模拟 Kafka-like durable queue。
CREATE TABLE IF NOT EXISTS vocabulary_tasks (
    -- 任务 ID。
    id TEXT PRIMARY KEY,

    -- 同传会话 ID，即 interpret_sessions.id；业务侧维护关联。
    session_id TEXT NOT NULL,

    -- 租户 ID。
    tenant_id TEXT NOT NULL,

    -- 用户 ID；历史会话可能为空。
    user_id TEXT,

    -- 分区 key：tenant_id:user_id；无 user_id 时退化为 tenant_id:session:session_id。
    partition_key TEXT NOT NULL,

    -- 任务状态：pending / running / succeeded / failed / cancelled。
    status TEXT NOT NULL DEFAULT 'pending',

    -- 最大词条数。
    max_words INTEGER NOT NULL DEFAULT 30,

    -- 英文侧来源策略：auto。
    english_source TEXT NOT NULL DEFAULT 'auto',

    -- 已尝试次数。
    attempt_count INTEGER NOT NULL DEFAULT 0,

    -- 当前锁定 worker ID。
    locked_by TEXT,

    -- 锁定时间。
    locked_at TEXT,

    -- 任务开始处理时间。
    started_at TEXT,

    -- 任务完成时间。
    finished_at TEXT,

    -- 错误信息。
    error_message TEXT,

    -- 本次任务输入快照 JSON。
    input_json TEXT,

    -- 原始请求 JSON，不含 API key。
    raw_request_json TEXT,

    -- 原始响应 JSON。
    raw_response_json TEXT,

    -- 创建时间。
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 更新时间。
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- 单词本任务结果词条。
CREATE TABLE IF NOT EXISTS vocabulary_entries (
    -- 自增主键。
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- vocabulary_tasks.id；业务侧维护关联。
    task_id TEXT NOT NULL,

    -- 词条顺序。
    ordinal INTEGER NOT NULL,

    -- 单词或短语。
    word TEXT NOT NULL,

    -- 原形。
    lemma TEXT,

    -- 音标。
    phonetic TEXT,

    -- 词性。
    part_of_speech TEXT,

    -- 中文释义。
    meaning_zh TEXT,

    -- 英文例句。
    example_en TEXT,

    -- 例句中文翻译。
    example_zh TEXT,

    -- 出现次数。
    occurrences INTEGER NOT NULL DEFAULT 0,

    -- 难度，例如 A2 / B1 / B2。
    difficulty TEXT,

    -- 来源 utterance id 集合 JSON。
    source_utterance_ids_json TEXT,

    -- 扩展 metadata。
    metadata_json TEXT,

    -- 创建时间。
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
`

var RequiredTables = []string{
	"providers",
	"interpret_sessions",
	"asr_callbacks",
	"mt_translation_records",
	"llm_revision_records",
	"vocabulary_tasks",
	"vocabulary_entries",
}

var RequiredIndexes = []string{
	"idx_providers_capability_enabled",
	"idx_sessions_tenant_started",
	"idx_asr_session_utterance",
	"idx_asr_session_final",
	"idx_asr_provider",
	"idx_mt_asr",
	"idx_mt_provider_status",
	"idx_llm_asr",
	"idx_llm_provider_status",
	"idx_vocab_tasks_status_created",
	"idx_vocab_tasks_partition_status_created",
	"idx_vocab_tasks_session_created",
	"idx_vocab_entries_task_ordinal",
}

// IndexesSQL contains query indexes for the session storage schema.
const IndexesSQL = `
CREATE INDEX IF NOT EXISTS idx_providers_capability_enabled
ON providers(capability, enabled, is_default);

CREATE INDEX IF NOT EXISTS idx_sessions_tenant_started
ON interpret_sessions(tenant_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_asr_session_utterance
ON asr_callbacks(session_id, utterance_id, sequence_no);

CREATE INDEX IF NOT EXISTS idx_asr_session_final
ON asr_callbacks(session_id, is_final, received_at);

CREATE INDEX IF NOT EXISTS idx_asr_provider
ON asr_callbacks(provider_id, received_at);

CREATE INDEX IF NOT EXISTS idx_mt_asr
ON mt_translation_records(asr_callback_id);

CREATE INDEX IF NOT EXISTS idx_mt_provider_status
ON mt_translation_records(provider_id, status, requested_at);

CREATE INDEX IF NOT EXISTS idx_llm_asr
ON llm_revision_records(asr_callback_id);

CREATE INDEX IF NOT EXISTS idx_llm_provider_status
ON llm_revision_records(provider_id, status, requested_at);

CREATE INDEX IF NOT EXISTS idx_vocab_tasks_status_created
ON vocabulary_tasks(status, created_at, id);

CREATE INDEX IF NOT EXISTS idx_vocab_tasks_partition_status_created
ON vocabulary_tasks(partition_key, status, created_at, id);

CREATE INDEX IF NOT EXISTS idx_vocab_tasks_session_created
ON vocabulary_tasks(session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_vocab_entries_task_ordinal
ON vocabulary_entries(task_id, ordinal);
`
