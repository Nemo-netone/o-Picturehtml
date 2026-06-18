// OpenAPI/Swagger类型定义
package httpapi

import (
	"time"

	"github.com/SATA260/SimulSpeak1/internal/api-server/router"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

type HealthResult struct {
	Status  string    `json:"status" example:"ok"`
	Service string    `json:"service" example:"api-server"`
	Time    time.Time `json:"time"`
}

type HealthResponse struct {
	Code    int          `json:"code" example:"200"`
	Message string       `json:"message" example:"ok"`
	Data    HealthResult `json:"data"`
}

type MediaNodeSummary struct {
	Total        int `json:"total" example:"3"`
	Available    int `json:"available" example:"2"`
	Unavailable  int `json:"unavailable" example:"1"`
	Up           int `json:"up" example:"2"`
	Down         int `json:"down" example:"0"`
	Draining     int `json:"draining" example:"1"`
	Suspect      int `json:"suspect" example:"0"`
	Capacity     int `json:"capacity" example:"300"`
	CurrentCalls int `json:"currentCalls" example:"42"`
}

type MediaNodesResult struct {
	Summary MediaNodeSummary `json:"summary"`
	Nodes   []*model.Node    `json:"nodes"`
}

type MediaNodesResponse struct {
	Code    int              `json:"code" example:"200"`
	Message string           `json:"message" example:"ok"`
	Data    MediaNodesResult `json:"data"`
}

type WorkerNodeSummary struct {
	Total       int `json:"total" example:"2"`
	Available   int `json:"available" example:"2"`
	Unavailable int `json:"unavailable" example:"0"`
	Up          int `json:"up" example:"2"`
	Down        int `json:"down" example:"0"`
	Draining    int `json:"draining" example:"0"`
	Suspect     int `json:"suspect" example:"0"`
	Capacity    int `json:"capacity" example:"8"`
	ActiveTasks int `json:"activeTasks" example:"3"`
}

type WorkerNodesResult struct {
	Summary WorkerNodeSummary `json:"summary"`
	Nodes   []*model.Node     `json:"nodes"`
}

type SystemNodesResult struct {
	RefreshedAt time.Time         `json:"refreshedAt"`
	Media       MediaNodesResult  `json:"media"`
	Workers     WorkerNodesResult `json:"workers"`
}

type SystemNodesResponse struct {
	Code    int               `json:"code" example:"200"`
	Message string            `json:"message" example:"ok"`
	Data    SystemNodesResult `json:"data"`
}

type InterpreterSessionSummary struct {
	ID                string             `json:"id"`
	TenantID          string             `json:"tenantId"`
	ConnectionID      string             `json:"connectionId,omitempty"`
	UserID            string             `json:"userId,omitempty"`
	Caller            string             `json:"caller,omitempty"`
	Callee            string             `json:"callee,omitempty"`
	State             string             `json:"state"`
	MediaState        string             `json:"mediaState,omitempty"`
	ProviderIDs       map[string][]int64 `json:"providerIds,omitempty"`
	TranslateStrategy string             `json:"translateStrategy,omitempty"`
	DubbingEnabled    bool               `json:"dubbingEnabled"`
	StartedAt         string             `json:"startedAt,omitempty"`
	EndedAt           string             `json:"endedAt,omitempty"`
	CreatedAt         string             `json:"createdAt,omitempty"`
	UpdatedAt         string             `json:"updatedAt,omitempty"`
	Metadata          map[string]any     `json:"metadata,omitempty"`
}

type InterpreterSessionListResult struct {
	Items  []InterpreterSessionSummary `json:"items"`
	Total  int64                       `json:"total"`
	Limit  int                         `json:"limit"`
	Offset int                         `json:"offset"`
}

type InterpreterSessionListResponse struct {
	Code    int                          `json:"code" example:"200"`
	Message string                       `json:"message" example:"ok"`
	Data    InterpreterSessionListResult `json:"data"`
}

type InterpreterSessionDetailResult struct {
	Session    InterpreterSessionSummary    `json:"session"`
	Utterances []InterpreterUtteranceDetail `json:"utterances"`
}

type InterpreterSessionDetailResponse struct {
	Code    int                            `json:"code" example:"200"`
	Message string                         `json:"message" example:"ok"`
	Data    InterpreterSessionDetailResult `json:"data"`
}

type InterpreterUtteranceDetail struct {
	UtteranceID  string                         `json:"utteranceId"`
	ASRCallbacks []InterpreterASRCallbackResult `json:"asrCallbacks"`
}

type InterpreterASRCallbackResult struct {
	ID             int64                        `json:"id"`
	SessionID      string                       `json:"sessionId"`
	ProviderID     int64                        `json:"providerId,omitempty"`
	CallID         string                       `json:"callId"`
	UtteranceID    string                       `json:"utteranceId"`
	SequenceNo     int64                        `json:"sequenceNo"`
	Language       string                       `json:"language,omitempty"`
	Text           string                       `json:"text"`
	IsFinal        bool                         `json:"isFinal"`
	Confidence     float64                      `json:"confidence,omitempty"`
	StartMS        int64                        `json:"startMs,omitempty"`
	EndMS          int64                        `json:"endMs,omitempty"`
	ReceivedAt     string                       `json:"receivedAt,omitempty"`
	Metadata       map[string]any               `json:"metadata,omitempty"`
	RawJSON        string                       `json:"rawJson,omitempty"`
	MTTranslations []InterpreterMTRecordResult  `json:"mtTranslations,omitempty"`
	LLMRevisions   []InterpreterLLMRecordResult `json:"llmRevisions,omitempty"`
}

type InterpreterMTRecordResult struct {
	ID              int64          `json:"id"`
	ASRCallbackID   int64          `json:"asrCallbackId"`
	ProviderID      int64          `json:"providerId,omitempty"`
	ASRPhase        string         `json:"asrPhase,omitempty"`
	SourceLang      string         `json:"sourceLang,omitempty"`
	TargetLang      string         `json:"targetLang,omitempty"`
	SourceText      string         `json:"sourceText,omitempty"`
	TargetText      string         `json:"targetText,omitempty"`
	IsFinal         bool           `json:"isFinal"`
	Status          string         `json:"status,omitempty"`
	ErrorCode       string         `json:"errorCode,omitempty"`
	ErrorMessage    string         `json:"errorMessage,omitempty"`
	LatencyMS       int64          `json:"latencyMs,omitempty"`
	RequestedAt     string         `json:"requestedAt,omitempty"`
	RespondedAt     string         `json:"respondedAt,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	RawRequestJSON  string         `json:"rawRequestJson,omitempty"`
	RawResponseJSON string         `json:"rawResponseJson,omitempty"`
}

type InterpreterLLMRecordResult struct {
	ID               int64          `json:"id"`
	ASRCallbackID    int64          `json:"asrCallbackId"`
	ProviderID       int64          `json:"providerId,omitempty"`
	SourceText       string         `json:"sourceText,omitempty"`
	DraftTranslation string         `json:"draftTranslation,omitempty"`
	RevisedText      string         `json:"revisedText,omitempty"`
	Revised          bool           `json:"revised"`
	Status           string         `json:"status,omitempty"`
	ErrorMessage     string         `json:"errorMessage,omitempty"`
	LatencyMS        int64          `json:"latencyMs,omitempty"`
	RequestedAt      string         `json:"requestedAt,omitempty"`
	RespondedAt      string         `json:"respondedAt,omitempty"`
	Context          any            `json:"context,omitempty"`
	Terms            any            `json:"terms,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	RawRequestJSON   string         `json:"rawRequestJson,omitempty"`
	RawResponseJSON  string         `json:"rawResponseJson,omitempty"`
}

type CreateVocabularyTaskRequest struct {
	MaxWords int `json:"maxWords,omitempty" example:"30"`
}

type VocabularyTaskResult struct {
	ID            string         `json:"id"`
	SessionID     string         `json:"sessionId"`
	TenantID      string         `json:"tenantId"`
	UserID        string         `json:"userId,omitempty"`
	PartitionKey  string         `json:"partitionKey"`
	Status        string         `json:"status"`
	MaxWords      int            `json:"maxWords"`
	EnglishSource string         `json:"englishSource,omitempty"`
	AttemptCount  int            `json:"attemptCount"`
	LockedBy      string         `json:"lockedBy,omitempty"`
	LockedAt      string         `json:"lockedAt,omitempty"`
	StartedAt     string         `json:"startedAt,omitempty"`
	FinishedAt    string         `json:"finishedAt,omitempty"`
	ErrorMessage  string         `json:"errorMessage,omitempty"`
	Input         any            `json:"input,omitempty"`
	CreatedAt     string         `json:"createdAt,omitempty"`
	UpdatedAt     string         `json:"updatedAt,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type VocabularyEntryResult struct {
	ID                 int64          `json:"id,omitempty"`
	TaskID             string         `json:"taskId,omitempty"`
	Ordinal            int            `json:"ordinal"`
	Word               string         `json:"word"`
	Lemma              string         `json:"lemma,omitempty"`
	Phonetic           string         `json:"phonetic,omitempty"`
	PartOfSpeech       string         `json:"partOfSpeech,omitempty"`
	MeaningZH          string         `json:"meaningZh,omitempty"`
	ExampleEN          string         `json:"exampleEn,omitempty"`
	ExampleZH          string         `json:"exampleZh,omitempty"`
	Occurrences        int            `json:"occurrences,omitempty"`
	Difficulty         string         `json:"difficulty,omitempty"`
	SourceUtteranceIDs any            `json:"sourceUtteranceIds,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type VocabularyTaskListResult struct {
	Items  []VocabularyTaskResult `json:"items"`
	Total  int64                  `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

type VocabularyTaskDetailResult struct {
	Task    VocabularyTaskResult    `json:"task"`
	Entries []VocabularyEntryResult `json:"entries,omitempty"`
}

type VocabularyTaskResponse struct {
	Code    int                  `json:"code" example:"200"`
	Message string               `json:"message" example:"ok"`
	Data    VocabularyTaskResult `json:"data"`
}

type VocabularyTaskListResponse struct {
	Code    int                      `json:"code" example:"200"`
	Message string                   `json:"message" example:"ok"`
	Data    VocabularyTaskListResult `json:"data"`
}

type VocabularyTaskDetailResponse struct {
	Code    int                        `json:"code" example:"200"`
	Message string                     `json:"message" example:"ok"`
	Data    VocabularyTaskDetailResult `json:"data"`
}

type ErrorResponse struct {
	Code    int    `json:"code" example:"404"`
	Message string `json:"message" example:"error"`
	Error   string `json:"error" example:"not found"`
}

type RouteCallRequest struct {
	TenantID string               `json:"tenantId" example:"tenant-a"`
	Caller   string               `json:"caller" example:"1001"`
	Callee   string               `json:"callee" example:"1002"`
	Media    string               `json:"media" example:"audio"`
	NeedAI   bool                 `json:"needAI" example:"true"`
	Strategy router.RouteStrategy `json:"strategy" example:"round_robin"`
	Zone     string               `json:"zone" example:"az-a"`
	Language string               `json:"language" example:"zh-CN"`
	ASRModel string               `json:"asrModel" example:"general"`
}

type RouteCallResponse struct {
	Code    int                `json:"code" example:"200"`
	Message string             `json:"message" example:"ok"`
	Data    router.RouteResult `json:"data"`
}

type CallSessionResponse struct {
	Code    int               `json:"code" example:"200"`
	Message string            `json:"message" example:"ok"`
	Data    model.CallSession `json:"data"`
}

type CallEndResult struct {
	CallID string `json:"callId" example:"call-01"`
}

type CallEndResponse struct {
	Code    int           `json:"code" example:"200"`
	Message string        `json:"message" example:"ok"`
	Data    CallEndResult `json:"data"`
}

type ExtensionResponse struct {
	Code    int             `json:"code" example:"200"`
	Message string          `json:"message" example:"ok"`
	Data    model.Extension `json:"data"`
}

type PresenceResponse struct {
	Code    int            `json:"code" example:"200"`
	Message string         `json:"message" example:"ok"`
	Data    model.Presence `json:"data"`
}
