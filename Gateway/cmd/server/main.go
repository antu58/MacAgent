package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	routeDirect    = "direct"
	routeAskUser   = "ask_user"
	routeDeepModel = "deepmodel"
)

type Config struct {
	Port               string
	DefaultIntentsFile string

	FeishuSocketEnabled bool
	FeishuAppID         string
	FeishuAppSecret     string
	FeishuOpenBaseURL   string

	IntentModelBaseURL  string
	IntentModelName     string
	IntentModelAPIKey   string
	IntentTimeoutSec    int
	ExecutionTimeoutSec int

	LowPrecisionModelBaseURL            string
	LowPrecisionModelName               string
	LowPrecisionModelAPIKey             string
	LowPrecisionMultimodalModelBaseURL  string
	LowPrecisionMultimodalModelName     string
	LowPrecisionMultimodalModelAPIKey   string
	HighPrecisionMultimodalModelBaseURL string
	HighPrecisionMultimodalModelName    string
	HighPrecisionMultimodalModelAPIKey  string

	DeepModelBaseURL            string
	DeepModelName               string
	DeepModelAPIKey             string
	DeepModelInsecureSkipVerify bool
	DeepTimeoutSec              int

	MemoryTTL time.Duration
}

type ChatRequest struct {
	SessionID string       `json:"session_id"`
	Message   string       `json:"message"`
	Intents   []IntentSpec `json:"intents"`
}

type RouteDecision struct {
	Route        string         `json:"route"`
	Skill        *string        `json:"skill"`
	Params       map[string]any `json:"params"`
	MissingParms []string       `json:"missing_params"`
	Confidence   float64        `json:"confidence"`
	Question     *string        `json:"question"`
}

type ClassificationDecision struct {
	Hit        bool    `json:"hit"`
	Intent     *string `json:"intent"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
}

type IntentExecutionResult struct {
	Skill        *string        `json:"skill"`
	Params       map[string]any `json:"params"`
	MissingParms []string       `json:"missing_params"`
	Confidence   float64        `json:"confidence"`
	Question     *string        `json:"question"`
}

type FrontendResult struct {
	Type         string         `json:"type"`
	Skill        *string        `json:"skill,omitempty"`
	Params       map[string]any `json:"params,omitempty"`
	MissingParms []string       `json:"missing_params,omitempty"`
	Question     *string        `json:"question,omitempty"`
	Response     any            `json:"response,omitempty"`
}

type SSEMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Done    bool   `json:"done"`
}

type SessionMemory struct {
	PendingSkill   string         `json:"pending_skill"`
	CollectedParms map[string]any `json:"collected_params"`
	MissingParms   []string       `json:"missing_params"`
	Timestamp      int64          `json:"timestamp"`
}

type IntentSpec struct {
	Name                 string      `json:"name"`
	ModelType            string      `json:"model_type,omitempty"`
	RouteDescription     string      `json:"route_description,omitempty"`
	ExecutionDescription string      `json:"execution_description,omitempty"`
	Description          string      `json:"description,omitempty"`
	Params               []ParamSpec `json:"params"`
}

type ParamSpec struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ValueType    string   `json:"value_type"`
	Required     bool     `json:"required"`
	IsEnum       bool     `json:"is_enum"`
	EnumValues   []string `json:"enum_values,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
}

type MemoryStore struct {
	mu   sync.Mutex
	data map[string]SessionMemory
	ttl  time.Duration
}

type SkillSpec struct {
	ModelType            string      `json:"model_type,omitempty"`
	RouteDescription     string      `json:"route_description,omitempty"`
	ExecutionDescription string      `json:"execution_description,omitempty"`
	Description          string      `json:"description,omitempty"`
	Params               []ParamSpec `json:"params"`
}

type Gateway struct {
	cfg            Config
	defaultIntents []IntentSpec
	intentClient   *http.Client
	executorClient *http.Client
	deepClient     *http.Client
	memStore       *MemoryStore
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	Temperature    float64         `json:"temperature"`
	Stream         bool            `json:"stream"`
	EnableThinking *bool           `json:"enable_thinking,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

var builtInSkills = map[string]SkillSpec{
	"greeting.reply": {
		ModelType:            "low_precision",
		RouteDescription:     "Use only for pure greetings such as hello, hi, 你好, 早上好. Do not use when the user is asking to perform a task.",
		ExecutionDescription: "Reply briefly to a greeting in the user's language.",
		Description:          "处理寒暄问候，直接简单回复",
	},
}

func main() {
	cfg := loadConfig()
	defaultIntents, err := loadIntentSpecsFile(cfg.DefaultIntentsFile)
	if err != nil {
		log.Fatalf("load default intents failed: %v", err)
	}
	gw := &Gateway{
		cfg:            cfg,
		defaultIntents: defaultIntents,
		intentClient:   newHTTPClient(false, time.Duration(cfg.IntentTimeoutSec)*time.Second),
		executorClient: newHTTPClient(false, time.Duration(cfg.ExecutionTimeoutSec)*time.Second),
		deepClient:     newHTTPClient(cfg.DeepModelInsecureSkipVerify, time.Duration(cfg.DeepTimeoutSec)*time.Second),
		memStore:       NewMemoryStore(cfg.MemoryTTL),
	}

	if cfg.FeishuSocketEnabled {
		bridge, err := newFeishuBridge(gw)
		if err != nil {
			log.Fatalf("init feishu bridge failed: %v", err)
		}
		go func() {
			if err := bridge.Start(context.Background()); err != nil {
				log.Fatal(err)
			}
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", gw.handleHealthz)
	mux.HandleFunc("/route", gw.handleRoute)
	mux.HandleFunc("/chat", gw.handleChat)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           withLog(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("gateway started at :%s", cfg.Port)
	log.Printf("default intents: %d loaded from %s", len(defaultIntents), cfg.DefaultIntentsFile)
	log.Printf("feishu socket enabled: %t", cfg.FeishuSocketEnabled)
	log.Printf("classifier model: %s (%s)", cfg.IntentModelName, cfg.IntentModelBaseURL)
	log.Printf("low_precision model: %s (%s)", cfg.LowPrecisionModelName, cfg.LowPrecisionModelBaseURL)
	log.Printf("low_precision_multimodal model: %s (%s)", cfg.LowPrecisionMultimodalModelName, cfg.LowPrecisionMultimodalModelBaseURL)
	log.Printf("deep model: %s (%s)", cfg.DeepModelName, cfg.DeepModelBaseURL)
	log.Printf("timeouts: classifier=%ds executor=%ds deep=%ds", cfg.IntentTimeoutSec, cfg.ExecutionTimeoutSec, cfg.DeepTimeoutSec)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadConfig() Config {
	return Config{
		Port:               getenv("PORT", "8080"),
		DefaultIntentsFile: getenv("DEFAULT_INTENTS_FILE", "/app/default_intents.json"),

		FeishuSocketEnabled: getenvBool("FEISHU_SOCKET_ENABLED", false),
		FeishuAppID:         getenv("FEISHU_APP_ID", ""),
		FeishuAppSecret:     getenv("FEISHU_APP_SECRET", ""),
		FeishuOpenBaseURL:   getenv("FEISHU_OPEN_BASE_URL", ""),

		IntentModelBaseURL:  getenv("INTENT_MODEL_BASE_URL", "http://127.0.0.1:18081/v1"),
		IntentModelName:     getenv("INTENT_MODEL_NAME", "mlx-community/Qwen3.5-0.8B-8bit"),
		IntentModelAPIKey:   getenv("INTENT_MODEL_API_KEY", "sk-local"),
		IntentTimeoutSec:    getenvInt("INTENT_TIMEOUT_SECONDS", 30),
		ExecutionTimeoutSec: getenvInt("EXECUTION_TIMEOUT_SECONDS", 60),

		LowPrecisionModelBaseURL:            getenv("LOW_PRECISION_MODEL_BASE_URL", "http://127.0.0.1:18081/v1"),
		LowPrecisionModelName:               getenv("LOW_PRECISION_MODEL_NAME", "mlx-community/Qwen3.5-0.8B-8bit"),
		LowPrecisionModelAPIKey:             getenv("LOW_PRECISION_MODEL_API_KEY", "sk-local"),
		LowPrecisionMultimodalModelBaseURL:  getenv("LOW_PRECISION_MULTIMODAL_MODEL_BASE_URL", "http://127.0.0.1:18082/v1"),
		LowPrecisionMultimodalModelName:     getenv("LOW_PRECISION_MULTIMODAL_MODEL_NAME", "mlx-community/Qwen3.5-4B-MLX-8bit"),
		LowPrecisionMultimodalModelAPIKey:   getenv("LOW_PRECISION_MULTIMODAL_MODEL_API_KEY", "sk-local"),
		HighPrecisionMultimodalModelBaseURL: getenv("HIGH_PRECISION_MULTIMODAL_MODEL_BASE_URL", ""),
		HighPrecisionMultimodalModelName:    getenv("HIGH_PRECISION_MULTIMODAL_MODEL_NAME", ""),
		HighPrecisionMultimodalModelAPIKey:  getenv("HIGH_PRECISION_MULTIMODAL_MODEL_API_KEY", ""),

		DeepModelBaseURL:            getenv("DEEP_MODEL_BASE_URL", "https://api.newcoin.tech/v1"),
		DeepModelName:               getenv("DEEP_MODEL_NAME", "doubao-seed-1-6-251015"),
		DeepModelAPIKey:             getenv("DEEP_MODEL_API_KEY", ""),
		DeepModelInsecureSkipVerify: getenvBool("DEEP_MODEL_INSECURE_SKIP_VERIFY", false),
		DeepTimeoutSec:              getenvInt("DEEP_TIMEOUT_SECONDS", 120),

		MemoryTTL: time.Duration(getenvInt("MEMORY_TTL_SECONDS", 600)) * time.Second,
	}
}

func newHTTPClient(insecureSkipVerify bool, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
}

func NewMemoryStore(ttl time.Duration) *MemoryStore {
	return &MemoryStore{
		data: make(map[string]SessionMemory),
		ttl:  ttl,
	}
}

func loadIntentSpecsFile(path string) ([]IntentSpec, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("default intents file is empty")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var intents []IntentSpec
	if err := json.Unmarshal(raw, &intents); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range intents {
		intents[i].Name = strings.TrimSpace(intents[i].Name)
		intents[i].ModelType = normalizeModelType(intents[i].ModelType)
		intents[i].RouteDescription = strings.TrimSpace(intents[i].RouteDescription)
		intents[i].ExecutionDescription = strings.TrimSpace(intents[i].ExecutionDescription)
		intents[i].Description = strings.TrimSpace(intents[i].Description)
		intents[i].Params = sanitizeParamSpecs(intents[i].Params)
	}
	return intents, nil
}

func (m *MemoryStore) Set(sessionID string, mem SessionMemory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[sessionID] = mem
}

func (m *MemoryStore) Get(sessionID string) (SessionMemory, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.data[sessionID]
	if !ok {
		return SessionMemory{}, false
	}
	if time.Since(time.Unix(item.Timestamp, 0)) > m.ttl {
		delete(m.data, sessionID)
		return SessionMemory{}, false
	}
	return item, true
}

func (m *MemoryStore) Clear(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, sessionID)
}

func (m *MemoryStore) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for sessionID, item := range m.data {
		if now.Sub(time.Unix(item.Timestamp, 0)) > m.ttl {
			delete(m.data, sessionID)
		}
	}
}

func (gw *Gateway) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (gw *Gateway) handleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := decodeChatRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("[gateway][route][session=%s] %s", req.SessionID, summarizeIntentSpecs(req.Intents))
	mergedIntents := mergeIntentSpecs(gw.defaultIntents, req.Intents)
	log.Printf("[gateway][route][session=%s] merged=%s", req.SessionID, summarizeIntentSpecs(mergedIntents))
	skillCatalog := buildSkillCatalog(mergedIntents)

	mem, ok := gw.memStore.Get(req.SessionID)
	var memPtr *SessionMemory
	if ok {
		memPtr = &mem
	}

	result, err := gw.resolveFrontendResult(r.Context(), req, memPtr, skillCatalog, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	log.Printf("[route][session=%s] type=%s skill=%s", req.SessionID, result.Type, stringValue(result.Skill))
	_ = json.NewEncoder(w).Encode(result)
}

func (gw *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := decodeChatRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("[gateway][chat][session=%s] %s", req.SessionID, summarizeIntentSpecs(req.Intents))
	mergedIntents := mergeIntentSpecs(gw.defaultIntents, req.Intents)
	log.Printf("[gateway][chat][session=%s] merged=%s", req.SessionID, summarizeIntentSpecs(mergedIntents))
	skillCatalog := buildSkillCatalog(mergedIntents)

	gw.memStore.Cleanup()
	mem, ok := gw.memStore.Get(req.SessionID)
	var memPtr *SessionMemory
	if ok {
		memPtr = &mem
	}

	setSSEHeaders(w)
	if err := writeSSE(w, SSEMessage{Type: "status", Content: "routing...", Done: false}); err != nil {
		return
	}

	result, err := gw.resolveFrontendResult(r.Context(), req, memPtr, skillCatalog, func(status string) {
		_ = writeSSE(w, SSEMessage{Type: "status", Content: status, Done: false})
	})
	if err != nil {
		log.Printf("resolve frontend result error: %v", err)
		_ = writeSSE(w, SSEMessage{Type: "message", Content: fmt.Sprintf(`{"type":"result","response":{"type":"assistant_message","content":%q}}`, "gateway 执行失败"), Done: true})
		return
	}
	_ = writeSSE(w, SSEMessage{Type: "message", Content: mustJSON(result), Done: true})
}

func (gw *Gateway) resolveFrontendResult(ctx context.Context, req ChatRequest, mem *SessionMemory, skillCatalog map[string]SkillSpec, emitStatus func(string)) (FrontendResult, error) {
	if mem != nil && strings.TrimSpace(mem.PendingSkill) != "" {
		pendingSkill := strings.TrimSpace(mem.PendingSkill)
		if isCancelMessage(req.Message) {
			log.Printf("[gateway][pending][session=%s] cancel skill=%s", req.SessionID, pendingSkill)
			gw.memStore.Clear(req.SessionID)
			return FrontendResult{
				Type: "result",
				Response: map[string]any{
					"type":    "assistant_message",
					"content": buildCancelReply(req.Message),
				},
			}, nil
		}
		if _, ok := skillCatalog[pendingSkill]; ok {
			log.Printf("[gateway][pending][session=%s] continue skill=%s missing=%v", req.SessionID, pendingSkill, mem.MissingParms)
			return gw.executeIntentFlow(ctx, req, mem, skillCatalog, pendingSkill, emitStatus)
		}
		log.Printf("[gateway][pending][session=%s] clear stale skill=%s (not found in current catalog)", req.SessionID, pendingSkill)
		gw.memStore.Clear(req.SessionID)
	}

	classification, err := gw.classifyIntent(ctx, req.Message, mem, skillCatalog)
	if err != nil {
		log.Printf("[classifier][session=%s] failed: %v", req.SessionID, err)
		return gw.buildDeepModelResult(ctx, req, emitStatus)
	}
	log.Printf("[classifier][session=%s] hit=%t skill=%s confidence=%.3f reason=%s", req.SessionID, classification.Hit, stringValue(classification.Intent), classification.Confidence, classification.Reason)

	skill := stringValue(classification.Intent)
	if !classification.Hit || skill == "" {
		return gw.buildDeepModelResult(ctx, req, emitStatus)
	}
	return gw.executeIntentFlow(ctx, req, mem, skillCatalog, skill, emitStatus)
}

func (gw *Gateway) buildDeepModelResult(ctx context.Context, req ChatRequest, emitStatus func(string)) (FrontendResult, error) {
	if emitStatus != nil {
		emitStatus("calling deep model...")
	}
	gw.memStore.Clear(req.SessionID)

	content, err := gw.callOpenAI(ctx, "deep", gw.cfg.DeepModelBaseURL, gw.cfg.DeepModelName, gw.cfg.DeepModelAPIKey, []openAIMessage{
		{
			Role:    "system",
			Content: "You are a reliable multilingual assistant. Answer directly and use the user's language whenever possible.",
		},
		{
			Role:    "user",
			Content: req.Message,
		},
	}, 0.4, gw.deepClient)
	if err != nil {
		return FrontendResult{}, err
	}

	return FrontendResult{
		Type: "result",
		Response: map[string]any{
			"type":    "assistant_message",
			"content": content,
		},
	}, nil
}

func (gw *Gateway) executeIntentFlow(ctx context.Context, req ChatRequest, mem *SessionMemory, skillCatalog map[string]SkillSpec, skill string, emitStatus func(string)) (FrontendResult, error) {
	if skill == "greeting.reply" {
		gw.memStore.Clear(req.SessionID)
		return FrontendResult{
			Type:  "result",
			Skill: stringPtr(skill),
			Response: map[string]any{
				"type":    "assistant_message",
				"content": buildGreetingReply(req.Message),
			},
		}, nil
	}

	if emitStatus != nil {
		emitStatus("executing intent...")
	}

	execResult, err := gw.executeIntent(ctx, req.Message, mem, skill, skillCatalog)
	if err != nil {
		log.Printf("[executor][session=%s][skill=%s] failed: %v", req.SessionID, skill, err)
		return gw.buildDeepModelResult(ctx, req, emitStatus)
	}

	existingParams := map[string]any{}
	if mem != nil && mem.PendingSkill == skill && mem.CollectedParms != nil {
		existingParams = cloneParams(mem.CollectedParms)
	}
	mergedParams := mergeParamMaps(existingParams, execResult.Params)
	mergedParams = normalizeSkillParamsWithContext(skill, mergedParams, req.Message)

	normalized, missing, issues, err := inspectSkillParams(skill, mergedParams, skillCatalog)
	if err != nil {
		log.Printf("[executor][session=%s][skill=%s] validate failed: %v", req.SessionID, skill, err)
		return gw.buildDeepModelResult(ctx, req, emitStatus)
	}
	if len(issues) > 0 {
		log.Printf("[executor][session=%s][skill=%s] validate issues=%v", req.SessionID, skill, issues)
		return gw.buildDeepModelResult(ctx, req, emitStatus)
	}

	if len(missing) > 0 {
		question := strings.TrimSpace(stringValue(execResult.Question))
		if question == "" {
			question = buildMissingQuestion(req.Message, missing)
		}
		gw.memStore.Set(req.SessionID, SessionMemory{
			PendingSkill:   skill,
			CollectedParms: normalized,
			MissingParms:   missing,
			Timestamp:      time.Now().Unix(),
		})
		return FrontendResult{
			Type:         "ask_user",
			Skill:        stringPtr(skill),
			Params:       normalized,
			MissingParms: missing,
			Question:     stringPtr(question),
		}, nil
	}

	gw.memStore.Clear(req.SessionID)
	return FrontendResult{
		Type:  "result",
		Skill: stringPtr(skill),
		Response: map[string]any{
			"type":        "intent_result",
			"intent":      skill,
			"description": executionDescriptionText(skillCatalog[skill]),
			"params":      normalized,
			"executed":    false,
		},
	}, nil
}

func (gw *Gateway) classifyIntent(ctx context.Context, userMessage string, mem *SessionMemory, skillCatalog map[string]SkillSpec) (ClassificationDecision, error) {
	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return ClassificationDecision{}, errors.New("empty user message")
	}

	if isGreetingMessage(userMessage) {
		skill := "greeting.reply"
		return ClassificationDecision{
			Hit:        true,
			Intent:     &skill,
			Confidence: 0.98,
			Reason:     "greeting shortcut",
		}, nil
	}

	if len(skillCatalog) == 0 {
		return ClassificationDecision{Hit: false, Confidence: 0.2, Reason: "empty skill catalog"}, nil
	}

	prompt := buildClassificationPrompt(userMessage, mem, skillCatalog)
	rawContent, err := gw.callOpenAI(ctx, "classifier", gw.cfg.IntentModelBaseURL, gw.cfg.IntentModelName, gw.cfg.IntentModelAPIKey, []openAIMessage{
		{
			Role:    "system",
			Content: "You only classify whether the user matches one intent from the provided catalog. Output exactly one JSON object.",
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}, 0.1, gw.intentClient)
	if err != nil {
		return ClassificationDecision{}, err
	}
	log.Printf("[llm:classifier] raw=%s", preview(rawContent, 220))

	decision, err := parseClassificationDecision(rawContent)
	if err != nil {
		return ClassificationDecision{}, err
	}
	return normalizeClassificationDecision(decision, userMessage, skillCatalog), nil
}

func (gw *Gateway) executeIntent(ctx context.Context, userMessage string, mem *SessionMemory, skill string, skillCatalog map[string]SkillSpec) (IntentExecutionResult, error) {
	first, err := gw.executeIntentOnce(ctx, userMessage, mem, skill, skillCatalog, "")
	if err != nil {
		return IntentExecutionResult{}, err
	}

	feedback := buildExecutionRetryFeedback(skill, userMessage, first, mem, skillCatalog)
	if feedback == "" {
		return first, nil
	}
	log.Printf("[executor-retry][skill=%s] first_attempt_feedback=%s", skill, feedback)

	second, err := gw.executeIntentOnce(ctx, userMessage, mem, skill, skillCatalog, feedback)
	if err != nil {
		return IntentExecutionResult{}, err
	}
	secondFeedback := buildExecutionRetryFeedback(skill, userMessage, second, mem, skillCatalog)
	if secondFeedback != "" {
		log.Printf("[executor-retry][skill=%s] second_attempt_feedback=%s", skill, secondFeedback)
		return IntentExecutionResult{}, errors.New(secondFeedback)
	}
	return second, nil
}

func (gw *Gateway) executeIntentOnce(ctx context.Context, userMessage string, mem *SessionMemory, skill string, skillCatalog map[string]SkillSpec, retryFeedback string) (IntentExecutionResult, error) {
	spec, ok := skillCatalog[skill]
	if !ok {
		return IntentExecutionResult{}, fmt.Errorf("unsupported skill: %s", skill)
	}

	modelBaseURL, modelName, modelAPIKey := gw.resolveModelTarget(spec.ModelType)
	prompt := buildExecutionPrompt(userMessage, mem, skill, spec, retryFeedback)

	rawContent, err := gw.callOpenAI(ctx, "executor", modelBaseURL, modelName, modelAPIKey, []openAIMessage{
		{
			Role:    "system",
			Content: "You only execute the locked intent and return exactly one JSON object.",
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}, 0.1, gw.executorClient)
	if err != nil {
		return IntentExecutionResult{}, err
	}
	log.Printf("[llm:executor][skill=%s] raw=%s", skill, preview(rawContent, 220))

	result, err := parseExecutionResult(rawContent)
	if err != nil {
		return IntentExecutionResult{}, err
	}
	return result, nil
}

func (gw *Gateway) resolveModelTarget(modelType string) (string, string, string) {
	switch normalizeModelType(modelType) {
	case "low_precision":
		return gw.cfg.LowPrecisionModelBaseURL, gw.cfg.LowPrecisionModelName, gw.cfg.LowPrecisionModelAPIKey
	case "high_precision_multimodal":
		if strings.TrimSpace(gw.cfg.HighPrecisionMultimodalModelBaseURL) != "" && strings.TrimSpace(gw.cfg.HighPrecisionMultimodalModelName) != "" {
			return gw.cfg.HighPrecisionMultimodalModelBaseURL, gw.cfg.HighPrecisionMultimodalModelName, gw.cfg.HighPrecisionMultimodalModelAPIKey
		}
		return gw.cfg.LowPrecisionMultimodalModelBaseURL, gw.cfg.LowPrecisionMultimodalModelName, gw.cfg.LowPrecisionMultimodalModelAPIKey
	case "high_precision":
		return gw.cfg.DeepModelBaseURL, gw.cfg.DeepModelName, gw.cfg.DeepModelAPIKey
	default:
		return gw.cfg.LowPrecisionMultimodalModelBaseURL, gw.cfg.LowPrecisionMultimodalModelName, gw.cfg.LowPrecisionMultimodalModelAPIKey
	}
}

func (gw *Gateway) handleDirectPath(ctx context.Context, w http.ResponseWriter, req ChatRequest, route RouteDecision, skillCatalog map[string]SkillSpec) {
	skill := stringValue(route.Skill)
	if skill == "" {
		gw.handleDeepModelPath(ctx, w, req)
		return
	}

	normalized, missing, err := validateSkill(skill, route.Params, skillCatalog)
	if err != nil {
		gw.memStore.Clear(req.SessionID)
		_ = writeSSE(w, SSEMessage{Type: "status", Content: "invalid direct route, fallback to deepmodel", Done: false})
		gw.handleDeepModelPath(ctx, w, req)
		return
	}

	if len(missing) > 0 {
		question := buildMissingQuestion(req.Message, missing)
		gw.memStore.Set(req.SessionID, SessionMemory{
			PendingSkill:   skill,
			CollectedParms: normalized,
			MissingParms:   missing,
			Timestamp:      time.Now().Unix(),
		})
		_ = writeSSE(w, SSEMessage{Type: "message", Content: question, Done: true})
		return
	}

	result := executeSkill(skill, normalized, skillCatalog, req.Message)
	gw.memStore.Clear(req.SessionID)
	_ = writeSSE(w, SSEMessage{Type: "status", Content: "skill executed", Done: false})
	_ = writeSSE(w, SSEMessage{Type: "message", Content: result, Done: true})
}

func (gw *Gateway) handleAskUserPath(w http.ResponseWriter, sessionID, userMessage string, route RouteDecision) {
	skill := stringValue(route.Skill)
	if skill == "" {
		skill = "unknown"
	}
	missing := route.MissingParms
	if len(missing) == 0 {
		missing = []string{defaultMissingParamLabel(userMessage)}
	}

	gw.memStore.Set(sessionID, SessionMemory{
		PendingSkill:   skill,
		CollectedParms: route.Params,
		MissingParms:   missing,
		Timestamp:      time.Now().Unix(),
	})

	question := strings.TrimSpace(stringValue(route.Question))
	if question == "" {
		question = buildMissingQuestion(userMessage, missing)
	}

	_ = writeSSE(w, SSEMessage{Type: "message", Content: question, Done: true})
}

func (gw *Gateway) handleDeepModelPath(ctx context.Context, w http.ResponseWriter, req ChatRequest) {
	gw.memStore.Clear(req.SessionID)
	_ = writeSSE(w, SSEMessage{Type: "status", Content: "calling deep model...", Done: false})

	content, err := gw.callOpenAI(ctx, "deep", gw.cfg.DeepModelBaseURL, gw.cfg.DeepModelName, gw.cfg.DeepModelAPIKey, []openAIMessage{
		{
			Role:    "system",
			Content: "You are a reliable multilingual assistant. Answer directly and use the user's language whenever possible.",
		},
		{
			Role:    "user",
			Content: req.Message,
		},
	}, 0.4, gw.deepClient)
	if err != nil {
		_ = writeSSE(w, SSEMessage{Type: "message", Content: "deep model 调用失败: " + err.Error(), Done: true})
		return
	}

	_ = writeSSE(w, SSEMessage{Type: "message", Content: content, Done: true})
}

func buildClassificationPrompt(userMessage string, mem *SessionMemory, skillCatalog map[string]SkillSpec) string {
	return fmt.Sprintf(
		`You are a multilingual intent classifier.

Your job:
1. Decide whether the current user message matches one intent from the catalog.
2. If it matches, choose exactly one intent name from the catalog.
3. Do not extract detailed parameters in this stage.

Output exactly one JSON object:
{"hit":true,"intent":"string|null","confidence":0.0,"reason":"string"}

Rules:
- If the message does not clearly match any intent, return {"hit":false,"intent":null,...}.
- Only choose an intent name that exists in the catalog.
- Use the intent description to understand when to use and when not to use an intent.
- Use parameter names and basic parameter structure only as hints.
- If session memory shows a pending skill, first consider whether the current message is supplying missing parameters for that pending skill.
- If the user is asking for open-ended knowledge, explanation, writing, brainstorming, analysis, coding help, or general conversation, return hit=false unless one intent explicitly covers that kind of request.
- If the message is fundamentally a knowledge question such as explaining a concept, describing how something works, comparing ideas, or answering “how/why/what is” style questions, default to hit=false.
- Do not force a match just because an intent description contains broad nouns such as review, sync, event, task, or query.
- Do not reinterpret unrelated nouns as event titles, locations, reminder content, weather locations, or task bodies when the user is clearly asking for an explanation instead of asking you to perform a task.
- If the user message cannot be naturally restated as “perform this exact intent-defined task now”, return hit=false.
- Do not explain, do not output markdown.

Intent catalog:
%s

Session memory:
%s

Current user message:
%s`,
		buildClassificationSkillText(filterIntentPromptSkills(skillCatalog)),
		buildIntentPromptMemoryText(mem),
		userMessage,
	)
}

func buildExecutionPrompt(userMessage string, mem *SessionMemory, skill string, spec SkillSpec, retryFeedback string) string {
	retryBlock := ""
	if strings.TrimSpace(retryFeedback) != "" {
		retryBlock = "\nPrevious output was invalid. Fix it strictly according to the error feedback below:\n" + strings.TrimSpace(retryFeedback) + "\n"
	}
	memoryGuidance := buildExecutionMemoryGuidance(mem, skill)

	return fmt.Sprintf(
		`You are the intent executor for exactly one locked intent.

Locked intent name:
%s

Your job:
1. Stay inside this locked intent only.
2. Extract parameters for this intent from the user message.
3. If required parameters are missing, list them in missing_params and ask a short question.

Output exactly one JSON object:
{"skill":"string","params":{},"missing_params":[],"confidence":0.0,"question":"string|null"}

Base parameter rules:
- skill must stay exactly equal to the locked intent name.
- Only use parameter names defined below.
- required=true parameters may appear in missing_params when missing.
- Optional parameters must never be the only reason to ask the user.
- If is_enum=true, only choose from enum_values.
- If value_type=number, output numbers only.
- If value_type=text, output text only.
- Keep parameter values in the user's original language whenever possible.
- Do not invent values when you cannot infer them reliably.
- Do not output default values on your own; Gateway handles defaults mechanically.
%s

Current intent execution description:
%s

Current intent params:
%s

Session memory:
%s%s

Current user message:
%s`,
		skill,
		memoryGuidance,
		executionDescriptionText(spec),
		buildExecutionParamText(spec),
		buildIntentPromptMemoryText(mem),
		retryBlock,
		userMessage,
	)
}

func buildClassificationSkillText(skillCatalog map[string]SkillSpec) string {
	if len(skillCatalog) == 0 {
		return "[]"
	}

	names := make([]string, 0, len(skillCatalog))
	for name := range skillCatalog {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		spec := skillCatalog[name]
		b.WriteString("- intent: ")
		b.WriteString(name)
		if routeDescriptionText(spec) != "" {
			b.WriteString("\n  route_description: ")
			b.WriteString(routeDescriptionText(spec))
		}
		if len(spec.Params) > 0 {
			b.WriteString("\n  params: ")
			paramSummaries := make([]string, 0, len(spec.Params))
			for _, param := range spec.Params {
				paramSummaries = append(paramSummaries, fmt.Sprintf("%s(%s,required=%t,enum=%t)", param.Name, param.ValueType, param.Required, param.IsEnum))
			}
			b.WriteString(strings.Join(paramSummaries, ", "))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func buildExecutionParamText(spec SkillSpec) string {
	if len(spec.Params) == 0 {
		return "[]"
	}

	var b strings.Builder
	for _, param := range spec.Params {
		b.WriteString("- name: ")
		b.WriteString(param.Name)
		b.WriteString("\n  value_type: ")
		b.WriteString(param.ValueType)
		b.WriteString("\n  required: ")
		b.WriteString(strconv.FormatBool(param.Required))
		b.WriteString("\n  is_enum: ")
		b.WriteString(strconv.FormatBool(param.IsEnum))
		if param.Description != "" {
			b.WriteString("\n  description: ")
			b.WriteString(param.Description)
		}
		if param.IsEnum && len(param.EnumValues) > 0 {
			b.WriteString("\n  enum_values: [")
			b.WriteString(strings.Join(param.EnumValues, ", "))
			b.WriteString("]")
		}
		if param.DefaultValue != "" {
			b.WriteString("\n  default_value: ")
			b.WriteString(param.DefaultValue)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func buildExecutionMemoryGuidance(mem *SessionMemory, skill string) string {
	if mem == nil || strings.TrimSpace(mem.PendingSkill) == "" || mem.PendingSkill != skill {
		return ""
	}

	return fmt.Sprintf(`
- Session memory shows this exact skill is still pending.
- Treat the current user message primarily as a follow-up answer to fill the previously missing required params.
- Previously collected params: %s
- Previously missing required params: %v
- First try to extract values for those missing params from the current user message.
- After that, conceptually merge the new values with the previously collected params before deciding what is still missing.
- Do not ask again for parameters that are already present in session memory.`, mustJSON(mem.CollectedParms), mem.MissingParms)
}

func parseClassificationDecision(modelContent string) (ClassificationDecision, error) {
	content := strings.TrimSpace(modelContent)
	if content == "" {
		return ClassificationDecision{}, errors.New("empty classification content")
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return ClassificationDecision{}, errors.New("classification content does not include valid json object")
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil {
		return ClassificationDecision{}, err
	}

	decision := ClassificationDecision{
		Hit:        toBool(raw["hit"]),
		Confidence: toFloat(raw["confidence"]),
		Reason:     strings.TrimSpace(fmt.Sprintf("%v", raw["reason"])),
	}
	if intent := strings.TrimSpace(fmt.Sprintf("%v", raw["intent"])); intent != "" && intent != "null" && intent != "<nil>" {
		decision.Intent = &intent
	}
	return decision, nil
}

func normalizeClassificationDecision(decision ClassificationDecision, userMessage string, skillCatalog map[string]SkillSpec) ClassificationDecision {
	if isGreetingMessage(userMessage) {
		skill := "greeting.reply"
		return ClassificationDecision{Hit: true, Intent: &skill, Confidence: maxConfidence(decision.Confidence, 0.98), Reason: "greeting shortcut"}
	}

	skill := stringValue(decision.Intent)
	if skill == "" {
		return ClassificationDecision{Hit: false, Confidence: decision.Confidence, Reason: decision.Reason}
	}
	resolved := resolveSkillName(skill, skillCatalog)
	if resolved == "" {
		return ClassificationDecision{Hit: false, Confidence: decision.Confidence, Reason: decision.Reason}
	}
	return ClassificationDecision{
		Hit:        true,
		Intent:     &resolved,
		Confidence: decision.Confidence,
		Reason:     decision.Reason,
	}
}

func parseExecutionResult(modelContent string) (IntentExecutionResult, error) {
	content := strings.TrimSpace(modelContent)
	if content == "" {
		return IntentExecutionResult{}, errors.New("empty execution content")
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return IntentExecutionResult{}, errors.New("execution content does not include valid json object")
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil {
		return IntentExecutionResult{}, err
	}

	result := IntentExecutionResult{
		Confidence: toFloat(raw["confidence"]),
	}
	if skill := strings.TrimSpace(fmt.Sprintf("%v", raw["skill"])); skill != "" && skill != "null" && skill != "<nil>" {
		result.Skill = &skill
	}
	if params, ok := raw["params"].(map[string]any); ok {
		result.Params = params
	} else {
		result.Params = map[string]any{}
	}
	result.MissingParms = normalizeMissingParams(raw["missing_params"])
	if question := strings.TrimSpace(fmt.Sprintf("%v", raw["question"])); question != "" && question != "null" && question != "<nil>" {
		result.Question = &question
	}
	return result, nil
}

func buildExecutionRetryFeedback(skill string, userMessage string, result IntentExecutionResult, mem *SessionMemory, skillCatalog map[string]SkillSpec) string {
	if stringValue(result.Skill) == "" {
		return fmt.Sprintf("skill is empty. It must stay exactly equal to %s.", skill)
	}
	if resolved := resolveSkillName(stringValue(result.Skill), skillCatalog); resolved != skill {
		return fmt.Sprintf("skill must stay locked to %s, but you returned %s.", skill, stringValue(result.Skill))
	}

	existingParams := map[string]any{}
	if mem != nil && mem.PendingSkill == skill && mem.CollectedParms != nil {
		existingParams = cloneParams(mem.CollectedParms)
	}
	mergedParams := mergeParamMaps(existingParams, result.Params)
	mergedParams = normalizeSkillParamsWithContext(skill, mergedParams, userMessage)

	_, _, issues, err := inspectSkillParams(skill, mergedParams, skillCatalog)
	if err != nil {
		return fmt.Sprintf("mechanical validation failed: %v", err)
	}
	if len(issues) > 0 {
		return fmt.Sprintf("mechanical validation failed: %s", strings.Join(issues, "; "))
	}

	if len(result.MissingParms) > 0 {
		filtered := filterMissingToRequired(result.MissingParms, skillCatalog[skill])
		if len(filtered) != len(result.MissingParms) {
			return "missing_params may only contain required parameter names from the locked intent."
		}
	}

	if mem != nil && mem.PendingSkill == skill {
		existingParams := cloneParams(mem.CollectedParms)
		mergedParams := mergeParamMaps(existingParams, result.Params)
		_, mergedMissing, _, err := inspectSkillParams(skill, mergedParams, skillCatalog)
		if err == nil && len(mem.MissingParms) > 0 && len(result.Params) == 0 && len(mergedMissing) == len(mem.MissingParms) {
			return fmt.Sprintf(
				"Session memory indicates this is a follow-up answer for pending skill %s. Already collected params: %s. Previously missing params: %v. Re-read the current user message and extract values for those missing params if present. Do not ask again for already collected params. Current user message: %s",
				skill,
				mustJSON(existingParams),
				mem.MissingParms,
				userMessage,
			)
		}
	}
	return ""
}

func (gw *Gateway) routeByIntentModel(ctx context.Context, userMessage string, mem *SessionMemory, skillCatalog map[string]SkillSpec) (RouteDecision, error) {
	first, err := gw.routeByIntentModelOnce(ctx, userMessage, mem, skillCatalog, "")
	if err != nil {
		return RouteDecision{}, err
	}

	retryFeedback := buildIntentRetryFeedback(first, userMessage, skillCatalog)
	if retryFeedback == "" {
		return first, nil
	}

	log.Printf("[intent-retry] first_attempt_feedback=%s", retryFeedback)
	retryCatalog := skillCatalog
	if focused := buildFocusedRetrySkillCatalog(first, skillCatalog); len(focused) > 0 {
		retryCatalog = focused
	}
	second, err := gw.routeByIntentModelOnce(ctx, userMessage, mem, retryCatalog, retryFeedback)
	if err != nil {
		log.Printf("[intent-retry] second attempt failed: %v", err)
		return RouteDecision{Route: routeDeepModel, Confidence: first.Confidence}, nil
	}

	secondFeedback := buildIntentRetryFeedback(second, userMessage, skillCatalog)
	if secondFeedback != "" {
		log.Printf("[intent-retry] second_attempt_feedback=%s", secondFeedback)
		return RouteDecision{Route: routeDeepModel, Confidence: maxConfidence(first.Confidence, second.Confidence)}, nil
	}
	return second, nil
}

func (gw *Gateway) routeByIntentModelOnce(ctx context.Context, userMessage string, mem *SessionMemory, skillCatalog map[string]SkillSpec, retryFeedback string) (RouteDecision, error) {
	intentPrompt := buildIntentPrompt(userMessage, mem, skillCatalog, retryFeedback)

	rawContent, err := gw.callOpenAI(ctx, "intent", gw.cfg.IntentModelBaseURL, gw.cfg.IntentModelName, gw.cfg.IntentModelAPIKey, []openAIMessage{
		{
			Role:    "system",
			Content: "You only handle skill matching, parameter extraction, and routing decisions. Output must be a single valid JSON object. Work with multilingual input.",
		},
		{
			Role:    "user",
			Content: intentPrompt,
		},
	}, 0.1, gw.intentClient)
	if err != nil {
		return RouteDecision{}, err
	}
	log.Printf("[llm:intent] raw=%s", preview(rawContent, 220))

	decision, err := parseRouteDecision(rawContent)
	if err != nil {
		return RouteDecision{}, fmt.Errorf("parse route decision failed: %w", err)
	}
	return normalizeRouteDecision(decision, userMessage, skillCatalog), nil
}

func buildIntentPrompt(userMessage string, mem *SessionMemory, skillCatalog map[string]SkillSpec, retryFeedback string) string {
	retryBlock := ""
	retryFeedback = strings.TrimSpace(retryFeedback)
	if retryFeedback != "" {
		retryBlock = fmt.Sprintf("\n上一次输出存在问题，请修正后重新输出：\n%s\n", retryFeedback)
	}
	localeBlock := buildIntentPromptLocaleBlock(userMessage)

	return fmt.Sprintf(
		`你是一个严格的多语言意图路由模型，只负责三件事：
1. 选择 route
2. 从可用技能中选择一个 skill
3. 按参数规则抽取 params

你必须只输出一个 JSON 对象，不要 markdown，不要解释，不要多余文字。

唯一允许的输出格式：
{"route":"direct|ask_user|deepmodel","skill":"string|null","params":{},"missing_params":[],"confidence":0.0,"question":"string|null"}

你的决策步骤：
步骤1：判断是不是纯寒暄。
- “你好”“嗨”“hello”“hi”“hey”“good morning”“hola”“bonjour” 这类纯问候，返回 direct，skill=greeting.reply，params={}
- 但只要用户话里带了明确任务，就不要按寒暄处理

步骤2：如果短期记忆里有 pending_skill，优先判断用户这句话是不是在补之前缺的参数。

步骤3：只在“可用技能”中选 skill，禁止编造 skill 名。
补充要求：skill 必须逐字符复制“可用技能定义”里的原始 name，不能改写点号、下划线、大小写，也不能用描述代替 name。

步骤4：参数描述非常重要。参数描述代表这个参数真正想要的语义。

核心路由规则：
1. 命中某个技能，且必填参数齐全：返回 direct。
2. 命中某个技能，但缺少必填参数：返回 ask_user。
3. 只有确实不属于任何技能，或属于复杂开放式聊天/推理/创作时，才返回 deepmodel。
4. 可选参数缺失时不要 ask_user；它们可以省略，Go 会负责默认值。
5. missing_params 里只能放“必填参数名”，不要放可选参数名。
6. question 只有在 route=ask_user 时才填写；否则填 null。

参数抽取硬规则：
1. value_type=number：只能提取数字。
2. value_type=text：只能提取文字。
3. is_enum=true：只能从 enum_values 中选择现有值。
4. 如果用户话语无法可靠映射到某个枚举值，就不要填这个参数。
5. 禁止编造 enum_values 之外的值。
6. 一个值必须放到最匹配的参数里，不能因为语义接近就挂错参数。
7. 如果多个参数都是枚举参数，要优先按“值属于哪个参数的 enum_values”来决定归属。
8. 如果某个值不属于某个参数的 enum_values，就不要填给那个参数。
9. 对于 required=true 且 value_type=text 且 is_enum=false 的参数，只要用户原话里已经给出了可直接提取的内容，就必须直接提取，不要 ask_user。
10. 如果参数描述包含“内容”“正文”“记录”“备注”“备忘”等语义，应该提取真正要记录的正文，而不是整句命令壳。
11. 像“帮我新建一个备忘录，提醒我明天发布软件”这类句子，真正的内容应优先理解为“明天发布软件”。
12. 参数值必须尽量保留用户原话的语言和写法，不要把英文翻译成中文，也不要把中文翻译成英文。
13. 如果用户已经说了时间、地点、日期、标题或正文，就优先直接抽取，不要遗漏，不要把它们错误塞进别的参数。
14. 对于 content/title/time/location/date/due_time 这类文本参数，优先直接截取当前用户原话中的对应片段，不要自行翻译、改写或总结。
15. 如果可选参数在当前用户原话中已经明确出现，例如 due_time、date、location，就应该提取出来，不要因为它是可选参数而省略。

如何理解意图文件：
1. 每个 skill 的 description 是这个意图的主要语义定义，里面可以说明“适用于什么场景”“不适用于什么场景”“和相近意图如何区分”“命中后应该抽什么”。
2. 每个参数的 description 是这个参数的主要语义定义，里面可以说明“参数想要的内容”“不应包含什么”“是否要去掉命令壳”“是否要保留原话语言”。
3. 当 skill description 和 param description 已经给出明确约束时，你必须优先遵守这些约束，而不是按自己的习惯概括。
4. 如果某个参数 description 明确要求“只保留正文/主题/时间/地点”，就只提取那一部分，不要把整句命令塞进去。
5. 如果某个参数 description 明确要求“保留原话语言或原始表达”，就不要翻译、不要总结、不要替换成别的语言。
6. 如果 skill description 明确写了与其他意图的区分规则，例如 reminder vs todo、memo vs reminder、calendar vs reminder、weather vs deepmodel，你要按 description 的区分规则选择 skill。
7. 如果 skill description 或 param description 里使用了结构化提示字段，例如 when_to_use、not_for、extract、exclude、examples、preserve_language、copy_from_user，这些字段都应视为强约束。

关于 ask_user：
1. 只有必填参数缺失时才能 ask_user。
2. missing_params 只列参数名，例如 ["time"]。
3. 如果用户原话里已经出现了必填文本内容，就不要 ask_user。
4. question 要简短直接，并尽量使用用户当前语言。

当前输入语言约束：
%s

置信度建议：
- 明显命中技能且参数清晰：0.85 到 0.98
- 命中技能但还有少量不确定：0.65 到 0.84
- 不属于技能，走 deepmodel：0.20 到 0.60

可用技能定义：
%s

短期记忆：
%s%s

当前用户输入：
%s`, localeBlock, buildIntentPromptSkillText(filterIntentPromptSkills(skillCatalog)), buildIntentPromptMemoryText(mem), retryBlock, userMessage,
	)
}

func (gw *Gateway) callOpenAI(ctx context.Context, tag, baseURL, model, apiKey string, messages []openAIMessage, temperature float64, client *http.Client) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", errors.New("model is empty")
	}
	if strings.TrimSpace(baseURL) == "" {
		return "", errors.New("base_url is empty")
	}

	var enableThinking *bool
	if tag != "deep" {
		disabled := false
		enableThinking = &disabled
	}

	reqBody := openAIChatRequest{
		Model:          model,
		Messages:       messages,
		Temperature:    temperature,
		Stream:         false,
		EnableThinking: enableThinking,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := chatCompletionsURL(baseURL)
	start := time.Now()
	log.Printf("[llm:%s] start model=%s url=%s msg_count=%d", tag, model, url, len(messages))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	if client == nil {
		client = newHTTPClient(false, 60*time.Second)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[llm:%s] request failed after %s: %v", tag, time.Since(start), err)
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[llm:%s] read failed after %s: %v", tag, time.Since(start), err)
		return "", err
	}
	log.Printf("[llm:%s] status=%d elapsed=%s bytes=%d", tag, resp.StatusCode, time.Since(start), len(raw))

	if resp.StatusCode >= http.StatusBadRequest {
		log.Printf("[llm:%s] error body=%s", tag, preview(string(raw), 280))
		return "", fmt.Errorf("status=%d body=%s", resp.StatusCode, string(raw))
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		log.Printf("[llm:%s] invalid json body=%s", tag, preview(string(raw), 280))
		return "", fmt.Errorf("invalid openai response: %w", err)
	}
	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return "", errors.New(chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", errors.New("no choices in openai response")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	filtered := stripThinkingContent(content)
	if filtered != content {
		log.Printf("[llm:%s] content_filtered=%s", tag, preview(filtered, 180))
	} else {
		log.Printf("[llm:%s] content=%s", tag, preview(content, 180))
	}
	return filtered, nil
}

func parseRouteDecision(modelContent string) (RouteDecision, error) {
	content := strings.TrimSpace(modelContent)
	if content == "" {
		return RouteDecision{}, errors.New("empty route content")
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return RouteDecision{}, errors.New("route content does not include valid json object")
	}

	jsonPart := content[start : end+1]
	var raw map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &raw); err != nil {
		return RouteDecision{}, err
	}
	route := RouteDecision{
		Route:      strings.TrimSpace(fmt.Sprintf("%v", raw["route"])),
		Confidence: toFloat(raw["confidence"]),
	}
	if skill := strings.TrimSpace(fmt.Sprintf("%v", raw["skill"])); skill != "" && skill != "<nil>" && skill != "null" {
		route.Skill = &skill
	}
	if params, ok := raw["params"].(map[string]any); ok {
		route.Params = params
	}
	if question := strings.TrimSpace(fmt.Sprintf("%v", raw["question"])); question != "" && question != "<nil>" && question != "null" {
		route.Question = &question
	}
	route.MissingParms = normalizeMissingParams(raw["missing_params"])

	switch route.Route {
	case routeDirect, routeAskUser, routeDeepModel:
	default:
		if route.Skill != nil && route.Route == strings.TrimSpace(*route.Skill) {
			route.Route = routeDirect
			break
		}
		return RouteDecision{}, fmt.Errorf("invalid route: %s", route.Route)
	}
	if route.Params == nil {
		route.Params = map[string]any{}
	}
	return route, nil
}

func stripThinkingContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	for {
		start := strings.Index(strings.ToLower(content), "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(strings.ToLower(content[start:]), "</think>")
		if end == -1 {
			content = strings.TrimSpace(content[:start])
			break
		}
		end += start + len("</think>")
		content = strings.TrimSpace(content[:start] + content[end:])
	}

	if start := strings.Index(content, "{"); start > 0 {
		prefix := strings.TrimSpace(content[:start])
		if prefix != "" {
			return strings.TrimSpace(content[start:])
		}
	}
	return content
}

func normalizeMissingParams(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			switch typed := item.(type) {
			case map[string]any:
				name := strings.TrimSpace(fmt.Sprintf("%v", typed["name"]))
				if name != "" && name != "null" {
					items = append(items, name)
				}
			default:
				s := strings.TrimSpace(fmt.Sprintf("%v", item))
				if s != "" && s != "null" {
					items = append(items, s)
				}
			}
		}
		return items
	case []string:
		items := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				items = append(items, item)
			}
		}
		return items
	case string:
		s := strings.TrimSpace(v)
		if s == "" || s == "null" {
			return nil
		}
		return []string{s}
	case map[string]any:
		items := make([]string, 0, len(v))
		for key := range v {
			key = strings.TrimSpace(key)
			if key != "" {
				items = append(items, key)
			}
		}
		return items
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", value))
		if s == "" || s == "null" {
			return nil
		}
		return []string{s}
	}
}

func normalizeRouteDecision(route RouteDecision, userMessage string, skillCatalog map[string]SkillSpec) RouteDecision {
	userMessage = strings.TrimSpace(userMessage)
	if route.Params == nil {
		route.Params = map[string]any{}
	}

	if isGreetingMessage(userMessage) {
		skill := "greeting.reply"
		return RouteDecision{
			Route:      routeDirect,
			Skill:      &skill,
			Params:     map[string]any{},
			Confidence: maxConfidence(route.Confidence, 0.95),
		}
	}

	skill := stringValue(route.Skill)
	question := stringValue(route.Question)

	if isPlaceholderValue(skill) {
		route.Skill = nil
		skill = ""
	}
	if skill != "" {
		resolved := resolveSkillName(skill, skillCatalog)
		if resolved == "" {
			route.Skill = nil
			skill = ""
		} else if resolved != skill {
			log.Printf("[route-skill-alias] from=%q to=%q", skill, resolved)
			route.Skill = &resolved
			skill = resolved
		}
	}
	if skill == "greeting.reply" && !isGreetingMessage(userMessage) {
		route.Skill = nil
		skill = ""
		return RouteDecision{Route: routeDeepModel, Confidence: route.Confidence}
	}
	if isPlaceholderValue(question) {
		route.Question = nil
		question = ""
	}

	switch route.Route {
	case routeDirect:
		if !isSupportedSkill(skill, skillCatalog) {
			return RouteDecision{Route: routeDeepModel, Confidence: route.Confidence}
		}
		_, missing, err := validateSkill(skill, route.Params, skillCatalog)
		if err != nil {
			return RouteDecision{Route: routeDeepModel, Confidence: route.Confidence}
		}
		if len(missing) > 0 {
			q := fmt.Sprintf("请补充参数：%s", strings.Join(missing, ", "))
			return RouteDecision{
				Route:        routeAskUser,
				Skill:        route.Skill,
				Params:       route.Params,
				MissingParms: missing,
				Confidence:   route.Confidence,
				Question:     &q,
			}
		}
		return route
	case routeAskUser:
		if !isSupportedSkill(skill, skillCatalog) {
			return RouteDecision{Route: routeDeepModel, Confidence: route.Confidence}
		}

		_, missing, err := validateSkill(skill, route.Params, skillCatalog)
		if err != nil {
			return RouteDecision{Route: routeDeepModel, Confidence: route.Confidence}
		}
		route.MissingParms = filterMissingToRequired(route.MissingParms, skillCatalog[skill])
		if len(route.MissingParms) == 0 {
			route.MissingParms = missing
		}
		if len(route.MissingParms) == 0 {
			return RouteDecision{
				Route:      routeDirect,
				Skill:      route.Skill,
				Params:     route.Params,
				Confidence: route.Confidence,
			}
		}
		if question == "" || question == userMessage {
			q := fmt.Sprintf("请补充参数：%s", strings.Join(route.MissingParms, ", "))
			route.Question = &q
		}
		return route
	default:
		return RouteDecision{Route: routeDeepModel, Confidence: route.Confidence}
	}
}

func isSupportedSkill(skill string, skillCatalog map[string]SkillSpec) bool {
	if strings.TrimSpace(skill) == "" {
		return false
	}
	_, ok := skillCatalog[skill]
	return ok
}

func resolveSkillName(skill string, skillCatalog map[string]SkillSpec) string {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return ""
	}
	if _, ok := skillCatalog[skill]; ok {
		return skill
	}

	normalized := normalizeSkillKey(skill)
	match := ""
	for candidate := range skillCatalog {
		if normalizeSkillKey(candidate) != normalized {
			continue
		}
		if match != "" {
			match = ""
			break
		}
		match = candidate
	}
	if match != "" {
		return match
	}

	customSkills := make([]string, 0, len(skillCatalog))
	for candidate := range skillCatalog {
		if _, builtIn := builtInSkills[candidate]; builtIn {
			continue
		}
		customSkills = append(customSkills, candidate)
	}
	if len(customSkills) == 1 {
		return customSkills[0]
	}
	return ""
}

func normalizeSkillKey(skill string) string {
	skill = strings.TrimSpace(strings.ToLower(skill))
	if skill == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range skill {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= '\u4e00' && r <= '\u9fa5') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isPlaceholderValue(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return false
	}
	switch v {
	case "string|null", "null|string", "null", "string":
		return true
	}
	return strings.Contains(v, "string|null")
}

func buildIntentRetryFeedback(route RouteDecision, userMessage string, skillCatalog map[string]SkillSpec) string {
	skill := stringValue(route.Skill)
	switch route.Route {
	case routeDirect, routeAskUser:
		if skill == "" {
			return "你返回了 direct 或 ask_user，但 skill 为空。请从可用技能中重新选择 skill。"
		}
		if !isSupportedSkill(skill, skillCatalog) {
			return fmt.Sprintf("你返回的 skill=%q 不在当前可用技能中。请只从可用技能定义里的 name 中选择。", skill)
		}

		_, missing, issues, err := inspectSkillParams(skill, route.Params, skillCatalog)
		if err != nil {
			return fmt.Sprintf("技能校验失败：%v。请重新按可用技能定义输出。", err)
		}
		if len(issues) > 0 {
			return fmt.Sprintf("参数校验失败：%s。请严格按参数类型、枚举值和参数名重新输出，并尽量保留用户原话的语言，不要翻译参数值。", strings.Join(issues, "；"))
		}
		if route.Route == routeAskUser {
			retriable := retriableMissingTextParams(missing, skillCatalog[skill])
			if len(retriable) > 0 {
				return fmt.Sprintf(
					"当前 skill 已锁定为 %s。你返回了 ask_user，并认为缺少参数 %s。请重新检查用户原话是否已经包含这些必填文本参数；如果原话已包含，请直接提取并返回 direct。参数描述要作为语义依据，并尽量保留用户原话的语言，不要翻译参数值。\n参数定义：\n%s\n用户原话：%s",
					skill,
					strings.Join(retriable, ", "),
					buildRetryParamHint(retriable, skillCatalog[skill]),
					userMessage,
				)
			}
		}
	}
	return ""
}

func buildFocusedRetrySkillCatalog(route RouteDecision, skillCatalog map[string]SkillSpec) map[string]SkillSpec {
	skill := stringValue(route.Skill)
	if skill == "" {
		return nil
	}
	spec, ok := skillCatalog[skill]
	if !ok {
		return nil
	}
	return map[string]SkillSpec{
		skill: spec,
	}
}

func buildRetryParamHint(missing []string, spec SkillSpec) string {
	if len(missing) == 0 {
		return "- none"
	}
	set := make(map[string]struct{}, len(missing))
	for _, name := range missing {
		set[name] = struct{}{}
	}

	lines := make([]string, 0, len(missing))
	for _, param := range spec.Params {
		if _, ok := set[param.Name]; !ok {
			continue
		}
		line := fmt.Sprintf("- %s: %s", param.Name, strings.TrimSpace(param.Description))
		if param.ValueType != "" {
			line += fmt.Sprintf(" | value_type=%s", param.ValueType)
		}
		if param.IsEnum && len(param.EnumValues) > 0 {
			line += fmt.Sprintf(" | enum_values=%v", param.EnumValues)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "- none"
	}
	return strings.Join(lines, "\n")
}

func validateSkill(skill string, params map[string]any, skillCatalog map[string]SkillSpec) (map[string]any, []string, error) {
	normalized, missing, _, err := inspectSkillParams(skill, params, skillCatalog)
	return normalized, missing, err
}

func inspectSkillParams(skill string, params map[string]any, skillCatalog map[string]SkillSpec) (map[string]any, []string, []string, error) {
	spec, ok := skillCatalog[skill]
	if !ok {
		return nil, nil, nil, fmt.Errorf("unsupported skill: %s", skill)
	}
	if params == nil {
		params = map[string]any{}
	}

	normalized := make(map[string]any, len(params))
	for k, v := range params {
		normalized[k] = v
	}

	allowed := make(map[string]ParamSpec, len(spec.Params))
	for _, param := range spec.Params {
		allowed[param.Name] = param
	}

	normalized = stripUnsupportedSharedParams(normalized, allowed)

	missing := make([]string, 0, len(spec.Params))
	issues := make([]string, 0)
	for name := range normalized {
		if _, ok := allowed[name]; ok {
			continue
		}
		issues = append(issues, fmt.Sprintf("参数 %s 未在意图定义中声明", name))
		delete(normalized, name)
	}

	for _, param := range spec.Params {
		val, exists := normalized[param.Name]
		if !exists || isEmptyParamValue(val) {
			if param.Required {
				missing = append(missing, param.Name)
				continue
			}
			if param.DefaultValue != "" {
				normalized[param.Name] = param.DefaultValue
			}
			continue
		}
		converted, valid := normalizeParamValue(val, param)
		if valid {
			normalized[param.Name] = converted
			continue
		}
		issues = append(issues, describeParamValidationIssue(param, val))
		if param.Required {
			missing = append(missing, param.Name)
		} else {
			delete(normalized, param.Name)
			if param.DefaultValue != "" {
				normalized[param.Name] = param.DefaultValue
			}
		}
	}
	normalized = normalizeSkillParams(skill, normalized)
	return normalized, missing, issues, nil
}

func normalizeSkillParams(skill string, params map[string]any) map[string]any {
	_ = skill
	if len(params) == 0 {
		return params
	}
	if isEmptyParamValue(params["time"]) && isEmptyParamValue(params["time_period"]) {
		return params
	}
	return normalizeTimeLikeParams(params)
}

func normalizeSkillParamsWithContext(skill string, params map[string]any, userMessage string) map[string]any {
	_ = skill
	if len(params) == 0 {
		return params
	}
	if isEmptyParamValue(params["time"]) && isEmptyParamValue(params["time_period"]) {
		return params
	}
	return normalizeTimeLikeParamsWithContext(params, userMessage)
}

func stripUnsupportedSharedParams(params map[string]any, allowed map[string]ParamSpec) map[string]any {
	if len(params) == 0 {
		return params
	}

	_, allowTime := allowed["time"]
	_, allowTimePeriod := allowed["time_period"]
	if allowTime || allowTimePeriod {
		return params
	}

	_, hasTime := params["time"]
	_, hasTimePeriod := params["time_period"]
	if !hasTime && !hasTimePeriod {
		return params
	}

	cleaned := cloneParams(params)
	delete(cleaned, "time")
	delete(cleaned, "time_period")
	return cleaned
}

func normalizeTimeLikeParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}

	timeRaw := strings.TrimSpace(fmt.Sprintf("%v", params["time"]))
	if timeRaw == "" {
		return params
	}
	currentPeriod := strings.TrimSpace(fmt.Sprintf("%v", params["time_period"]))

	clock, period, ok := normalizeAlarmClockAndPeriod(timeRaw)
	if !ok {
		return params
	}

	normalized := cloneParams(params)
	normalized["time"] = clock
	if currentPeriod != "" && currentPeriod != "24小时制" && !shouldUse24HourStyle(timeRaw) {
		normalized["time_period"] = currentPeriod
		return normalized
	}
	if period != "" {
		normalized["time_period"] = period
	}
	return normalized
}

func normalizeTimeLikeParamsWithContext(params map[string]any, userMessage string) map[string]any {
	normalized := normalizeTimeLikeParams(params)
	if len(normalized) == 0 {
		return normalized
	}

	timeRaw := strings.TrimSpace(fmt.Sprintf("%v", normalized["time"]))
	if timeRaw == "" {
		return normalized
	}

	currentPeriod := strings.TrimSpace(fmt.Sprintf("%v", normalized["time_period"]))
	if shouldUse24HourStyle(timeRaw) || shouldUse24HourStyle(userMessage) {
		normalized["time_period"] = "24小时制"
		return normalized
	}

	if messagePeriod := detectTimePeriod(userMessage); messagePeriod != "" {
		normalized["time_period"] = messagePeriod
		return normalized
	}
	if currentPeriod != "" && currentPeriod != "24小时制" {
		normalized["time_period"] = currentPeriod
	}
	return normalized
}

func normalizeAlarmClockAndPeriod(raw string) (string, string, bool) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "：", ":"))
	if raw == "" {
		return "", "", false
	}

	if hour, minute, ampm, ok := extractAMPMClock(raw); ok {
		period := "上午"
		if ampm == "pm" {
			period = "晚上"
		} else if ampm == "am" && hour <= 5 {
			period = "凌晨"
		}
		return fmt.Sprintf("%02d:%02d", normalize12Hour(hour), minute), period, true
	}

	hour, minute, ok := extractColonClock(raw)
	if ok {
		if isExplicit24HourClock(hour, raw) {
			return fmt.Sprintf("%02d:%02d", hour, minute), "24小时制", true
		}
		if period := detectTimePeriod(raw); period != "" {
			return fmt.Sprintf("%02d:%02d", normalize12Hour(hour), minute), period, true
		}
		return fmt.Sprintf("%02d:%02d", hour, minute), "24小时制", true
	}

	hour, minute, ok = extractChineseClock(raw)
	if !ok {
		return "", "", false
	}

	period := detectTimePeriod(raw)
	if period == "" {
		return fmt.Sprintf("%02d:%02d", hour, minute), "24小时制", true
	}
	return fmt.Sprintf("%02d:%02d", normalize12Hour(hour), minute), period, true
}

func extractAMPMClock(raw string) (int, int, string, bool) {
	re := regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{1,2}))?\s*(am|pm)\b`)
	match := re.FindStringSubmatch(raw)
	if len(match) == 0 {
		return 0, 0, "", false
	}

	hour, err := strconv.Atoi(match[1])
	if err != nil || hour < 1 || hour > 12 {
		return 0, 0, "", false
	}

	minute := 0
	if strings.TrimSpace(match[2]) != "" {
		minute, err = strconv.Atoi(match[2])
		if err != nil || minute < 0 || minute > 59 {
			return 0, 0, "", false
		}
	}
	return hour, minute, strings.ToLower(match[3]), true
}

func extractColonClock(raw string) (int, int, bool) {
	re := regexp.MustCompile(`(?i)\b(\d{1,2}):(\d{1,2})\b`)
	match := re.FindStringSubmatch(raw)
	if len(match) == 0 {
		return 0, 0, false
	}

	hour, err := strconv.Atoi(match[1])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, false
	}
	minute, err := strconv.Atoi(match[2])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func extractChineseClock(raw string) (int, int, bool) {
	re := regexp.MustCompile(`(\d{1,2})\s*(?:点|點|时|時)(半|一刻|三刻|[0-5]?\d分?)?`)
	match := re.FindStringSubmatch(raw)
	if len(match) == 0 {
		return 0, 0, false
	}

	hour, err := strconv.Atoi(match[1])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, false
	}

	minute := 0
	switch strings.TrimSpace(match[2]) {
	case "", "0分":
		minute = 0
	case "半":
		minute = 30
	case "一刻":
		minute = 15
	case "三刻":
		minute = 45
	default:
		value := strings.TrimSuffix(strings.TrimSpace(match[2]), "分")
		minute, err = strconv.Atoi(value)
		if err != nil || minute < 0 || minute > 59 {
			return 0, 0, false
		}
	}
	return hour, minute, true
}

func detectTimePeriod(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(raw, "凌晨"):
		return "凌晨"
	case strings.Contains(raw, "早上"), strings.Contains(raw, "上午"), strings.Contains(lowered, " am"), strings.HasSuffix(lowered, "am"):
		return "上午"
	case strings.Contains(raw, "下午"), strings.Contains(raw, "中午"), strings.Contains(raw, "午后"):
		return "下午"
	case strings.Contains(raw, "晚上"), strings.Contains(raw, "今晚"), strings.Contains(raw, "傍晚"), strings.Contains(lowered, " pm"), strings.HasSuffix(lowered, "pm"), strings.Contains(lowered, "evening"), strings.Contains(lowered, "night"):
		return "晚上"
	default:
		return ""
	}
}

func isExplicit24HourClock(hour int, raw string) bool {
	if hour > 12 {
		return true
	}
	if hour == 0 {
		return true
	}
	lowered := strings.ToLower(raw)
	return strings.Contains(lowered, "am") || strings.Contains(lowered, "pm")
}

func shouldUse24HourStyle(raw string) bool {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "：", ":"))
	if raw == "" {
		return false
	}

	if hour, _, ok := extractColonClock(raw); ok {
		return isExplicit24HourClock(hour, raw)
	}
	if hour, _, ok := extractChineseClock(raw); ok {
		return hour > 12 || hour == 0
	}
	return false
}

func normalize12Hour(hour int) int {
	if hour == 0 {
		return 0
	}
	if hour > 12 {
		return hour - 12
	}
	return hour
}

func retriableMissingTextParams(missing []string, spec SkillSpec) []string {
	if len(missing) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(missing))
	for _, name := range missing {
		set[name] = struct{}{}
	}
	result := make([]string, 0, len(missing))
	for _, param := range spec.Params {
		if _, ok := set[param.Name]; !ok {
			continue
		}
		if !param.Required || param.IsEnum || param.ValueType != "text" {
			continue
		}
		if looksLikeContentParam(param) {
			result = append(result, param.Name)
		}
	}
	return result
}

func describeParamValidationIssue(param ParamSpec, value any) string {
	raw := strings.TrimSpace(fmt.Sprintf("%v", value))
	switch {
	case param.IsEnum && param.ValueType == "number":
		return fmt.Sprintf("参数 %s 只能从数字枚举值中选择，当前值=%q", param.Name, raw)
	case param.IsEnum:
		return fmt.Sprintf("参数 %s 只能从 enum_values 中选择，当前值=%q", param.Name, raw)
	case param.ValueType == "number":
		return fmt.Sprintf("参数 %s 需要数字，当前值=%q", param.Name, raw)
	default:
		return fmt.Sprintf("参数 %s 校验失败，当前值=%q", param.Name, raw)
	}
}

func remapMisplacedEnumParams(skill string, params map[string]any, skillCatalog map[string]SkillSpec) map[string]any {
	spec, ok := skillCatalog[skill]
	if !ok || len(spec.Params) == 0 || len(params) == 0 {
		return params
	}

	remapped := make(map[string]any, len(params))
	for key, value := range params {
		remapped[key] = value
	}

	for _, source := range spec.Params {
		if !source.IsEnum {
			continue
		}
		rawValue, exists := remapped[source.Name]
		if !exists || isEmptyParamValue(rawValue) {
			continue
		}
		if _, valid := normalizeEnumValue(rawValue, source); valid {
			continue
		}

		for _, target := range spec.Params {
			if target.Name == source.Name || !target.IsEnum {
				continue
			}
			if current, ok := remapped[target.Name]; ok && !isEmptyParamValue(current) {
				continue
			}
			converted, valid := normalizeEnumValue(rawValue, target)
			if !valid {
				continue
			}
			delete(remapped, source.Name)
			remapped[target.Name] = converted
			log.Printf("[route-remap] skill=%s move=%s->%s value=%q", skill, source.Name, target.Name, strings.TrimSpace(fmt.Sprintf("%v", rawValue)))
			break
		}
	}

	return remapped
}

func fillEnumParamsFromMessage(skill string, params map[string]any, userMessage string, skillCatalog map[string]SkillSpec) map[string]any {
	spec, ok := skillCatalog[skill]
	if !ok || len(spec.Params) == 0 {
		return params
	}

	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return params
	}

	filled := make(map[string]any, len(params))
	for key, value := range params {
		filled[key] = value
	}

	for _, param := range spec.Params {
		if !param.IsEnum || len(param.EnumValues) == 0 {
			continue
		}
		if current, exists := filled[param.Name]; exists && !isEmptyParamValue(current) {
			if _, valid := normalizeEnumValue(current, param); valid {
				continue
			}
		}

		matches := matchedEnumCandidates(userMessage, param)
		if len(matches) != 1 {
			continue
		}
		if converted, ok := normalizeEnumValue(matches[0], param); ok {
			filled[param.Name] = converted
			log.Printf("[route-enum-fill] skill=%s param=%s value=%q", skill, param.Name, matches[0])
		}
	}

	return filled
}

func matchedEnumCandidates(userMessage string, spec ParamSpec) []string {
	matches := make([]string, 0, len(spec.EnumValues))
	seen := make(map[string]struct{}, len(spec.EnumValues))
	for _, candidate := range spec.EnumValues {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !strings.Contains(userMessage, candidate) {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		matches = append(matches, candidate)
	}
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i]) > len(matches[j])
	})
	if len(matches) <= 1 {
		return matches
	}
	if strings.Contains(matches[0], matches[1]) {
		return matches[:1]
	}
	return nil
}

func fillRequiredTextParamsFromMessage(skill string, params map[string]any, userMessage string, skillCatalog map[string]SkillSpec) map[string]any {
	spec, ok := skillCatalog[skill]
	if !ok || len(spec.Params) == 0 {
		return params
	}

	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return params
	}

	filled := make(map[string]any, len(params))
	for key, value := range params {
		filled[key] = value
	}

	requiredTextParams := make([]ParamSpec, 0, len(spec.Params))
	for _, param := range spec.Params {
		if !param.Required || param.IsEnum || param.ValueType != "text" {
			continue
		}
		requiredTextParams = append(requiredTextParams, param)
	}

	for _, param := range requiredTextParams {
		if current, exists := filled[param.Name]; exists && !isEmptyParamValue(current) {
			continue
		}
		candidate := extractRequiredTextCandidate(userMessage, param, len(requiredTextParams))
		if candidate == "" {
			continue
		}
		filled[param.Name] = candidate
		log.Printf("[route-text-fill] skill=%s param=%s value=%q", skill, param.Name, candidate)
	}

	return filled
}

func extractRequiredTextCandidate(userMessage string, param ParamSpec, requiredTextParamCount int) string {
	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return ""
	}

	if candidate := extractContentTail(userMessage); candidate != "" && (looksLikeContentParam(param) || requiredTextParamCount == 1) {
		return candidate
	}

	if looksLikeContentParam(param) {
		for _, marker := range []string{"提醒我", "记得", "记下", "写下", "记录", "备注", "内容是", "内容"} {
			if idx := strings.Index(userMessage, marker); idx >= 0 {
				candidate := strings.TrimSpace(userMessage[idx:])
				candidate = trimContentLead(candidate)
				if candidate != "" && candidate != userMessage {
					return candidate
				}
				if candidate != "" {
					return candidate
				}
			}
		}
	}

	return ""
}

func extractContentTail(userMessage string) string {
	replacer := strings.NewReplacer("，", "\n", ",", "\n", "。", "\n", "；", "\n", ";", "\n", "：", "\n", ":", "\n")
	segments := strings.Split(replacer.Replace(userMessage), "\n")
	for i := 1; i < len(segments); i++ {
		candidate := trimContentLead(strings.TrimSpace(segments[i]))
		if candidate == "" {
			continue
		}
		return candidate
	}
	return ""
}

func trimContentLead(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	prefixes := []string{
		"内容是", "内容", "备注是", "备注", "记得", "提醒我", "请记得", "请提醒我",
		"帮我记下", "帮我记录", "帮我写下", "记录一下", "写一下",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
			break
		}
	}
	return strings.TrimSpace(value)
}

func looksLikeContentParam(param ParamSpec) bool {
	text := strings.ToLower(strings.TrimSpace(param.Name + " " + param.Description))
	keywords := []string{
		"content", "text", "note", "memo", "body", "query", "title", "message", "label",
		"内容", "正文", "文本", "备注", "备忘", "记录", "标题", "消息", "说明",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func executeSkill(skill string, params map[string]any, skillCatalog map[string]SkillSpec, userMessage string) string {
	if skill == "greeting.reply" {
		return buildGreetingReply(userMessage)
	}
	spec := skillCatalog[skill]
	payload := map[string]any{
		"type":        "intent_result",
		"intent":      skill,
		"description": spec.Description,
		"params":      params,
		"executed":    false,
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}

func mergeIntentSpecs(defaults, overrides []IntentSpec) []IntentSpec {
	if len(defaults) == 0 && len(overrides) == 0 {
		return nil
	}

	merged := make([]IntentSpec, 0, len(defaults)+len(overrides))
	indexByName := make(map[string]int, len(defaults)+len(overrides))

	appendOrReplace := func(intent IntentSpec) {
		intent.Name = strings.TrimSpace(intent.Name)
		if intent.Name == "" {
			return
		}
		intent.ModelType = normalizeModelType(intent.ModelType)
		intent.RouteDescription = strings.TrimSpace(intent.RouteDescription)
		intent.ExecutionDescription = strings.TrimSpace(intent.ExecutionDescription)
		intent.Description = strings.TrimSpace(intent.Description)
		intent.Params = sanitizeParamSpecs(intent.Params)
		if idx, ok := indexByName[intent.Name]; ok {
			merged[idx] = intent
			return
		}
		indexByName[intent.Name] = len(merged)
		merged = append(merged, intent)
	}

	for _, intent := range defaults {
		appendOrReplace(intent)
	}
	for _, intent := range overrides {
		appendOrReplace(intent)
	}
	return merged
}

func buildSkillCatalog(intents []IntentSpec) map[string]SkillSpec {
	catalog := make(map[string]SkillSpec, len(builtInSkills)+len(intents))
	for name, spec := range builtInSkills {
		catalog[name] = spec
	}
	for _, intent := range intents {
		name := strings.TrimSpace(intent.Name)
		if name == "" {
			continue
		}
		catalog[name] = SkillSpec{
			ModelType:            normalizeModelType(intent.ModelType),
			RouteDescription:     strings.TrimSpace(intent.RouteDescription),
			ExecutionDescription: strings.TrimSpace(intent.ExecutionDescription),
			Description:          strings.TrimSpace(intent.Description),
			Params:               sanitizeParamSpecs(intent.Params),
		}
	}
	return catalog
}

func filterIntentPromptSkills(skillCatalog map[string]SkillSpec) map[string]SkillSpec {
	filtered := make(map[string]SkillSpec, len(skillCatalog))
	for name, spec := range skillCatalog {
		if _, builtIn := builtInSkills[name]; builtIn {
			continue
		}
		filtered[name] = spec
	}
	return filtered
}

func buildIntentPromptSkillText(skillCatalog map[string]SkillSpec) string {
	if len(skillCatalog) == 0 {
		return "[]"
	}

	names := make([]string, 0, len(skillCatalog))
	for name := range skillCatalog {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		spec := skillCatalog[name]
		b.WriteString("- skill: ")
		b.WriteString(name)
		if spec.Description != "" {
			b.WriteString("\n  description: ")
			b.WriteString(spec.Description)
		}
		if len(spec.Params) == 0 {
			b.WriteString("\n  params: []\n")
			continue
		}
		b.WriteString("\n  params:\n")
		for _, param := range spec.Params {
			b.WriteString("  - name: ")
			b.WriteString(param.Name)
			if param.Description != "" {
				b.WriteString("\n    description: ")
				b.WriteString(param.Description)
			}
			b.WriteString("\n    value_type: ")
			b.WriteString(param.ValueType)
			b.WriteString("\n    required: ")
			b.WriteString(strconv.FormatBool(param.Required))
			b.WriteString("\n    is_enum: ")
			b.WriteString(strconv.FormatBool(param.IsEnum))
			if param.IsEnum {
				b.WriteString("\n    enum_values: [")
				b.WriteString(strings.Join(param.EnumValues, ", "))
				b.WriteString("]")
			}
			if param.DefaultValue != "" {
				b.WriteString("\n    default_value: ")
				b.WriteString(param.DefaultValue)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func buildIntentPromptMemoryText(mem *SessionMemory) string {
	if mem == nil {
		return "{}"
	}
	raw, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func buildIntentPromptLocaleBlock(userMessage string) string {
	if detectPromptLocale(userMessage) == "zh" {
		return "当前用户主要使用中文。参数值与 question 应尽量保留用户原话中的中文表达，不要改写成英文，也不要复用英文示例中的值。"
	}
	return "The current user is primarily using English or another non-Chinese language. Parameter values and question should stay in the user's original language whenever possible. Do not output Chinese values unless the current user message actually contains Chinese."
}

func detectPromptLocale(text string) string {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return "zh"
		}
	}
	return "en"
}

func sanitizeParamSpecs(params []ParamSpec) []ParamSpec {
	result := make([]ParamSpec, 0, len(params))
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			continue
		}
		param.Name = name
		param.Description = strings.TrimSpace(param.Description)
		param.ValueType = normalizeValueType(param.ValueType)
		param.EnumValues = sanitizeEnumValues(param.EnumValues, param.ValueType, param.IsEnum)
		param.DefaultValue = sanitizeDefaultValue(param.DefaultValue, param)
		if param.Required {
			param.DefaultValue = ""
		}
		result = append(result, param)
	}
	return result
}

func normalizeModelType(modelType string) string {
	switch strings.TrimSpace(modelType) {
	case "low_precision", "low_precision_multimodal", "high_precision_multimodal", "high_precision":
		return strings.TrimSpace(modelType)
	default:
		return "low_precision_multimodal"
	}
}

func routeDescriptionText(spec SkillSpec) string {
	return firstNonEmpty(spec.RouteDescription, spec.Description, spec.ExecutionDescription)
}

func executionDescriptionText(spec SkillSpec) string {
	return firstNonEmpty(spec.ExecutionDescription, spec.Description, spec.RouteDescription)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func filterMissingToRequired(missing []string, spec SkillSpec) []string {
	if len(missing) == 0 {
		return nil
	}
	requiredSet := make(map[string]struct{}, len(spec.Params))
	for _, param := range spec.Params {
		if param.Required {
			requiredSet[param.Name] = struct{}{}
		}
	}
	filtered := make([]string, 0, len(missing))
	for _, name := range missing {
		if _, ok := requiredSet[name]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func normalizeValueType(t string) string {
	switch strings.TrimSpace(t) {
	case "number":
		return strings.TrimSpace(t)
	default:
		return "text"
	}
}

func normalizeParamValue(value any, spec ParamSpec) (any, bool) {
	if spec.IsEnum {
		return normalizeEnumValue(value, spec)
	}
	switch spec.ValueType {
	case "number":
		return normalizeNumberValue(value)
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", value))
		return s, s != ""
	}
}

func normalizeNumberValue(value any) (any, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, err == nil
	default:
		return nil, false
	}
}

func normalizeEnumValue(value any, spec ParamSpec) (any, bool) {
	raw := strings.TrimSpace(fmt.Sprintf("%v", value))
	if raw == "" {
		return nil, false
	}
	for _, candidate := range spec.EnumValues {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if raw == candidate {
			if spec.ValueType == "number" {
				return normalizeNumberValue(candidate)
			}
			return candidate, true
		}
	}
	return nil, false
}

func sanitizeEnumValues(values []string, valueType string, isEnum bool) []string {
	if !isEnum {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch valueType {
		case "number":
			if _, ok := normalizeNumberValue(value); ok {
				result = append(result, value)
			}
		default:
			if _, ok := normalizeNumberValue(value); ok {
				continue
			}
			result = append(result, value)
		}
	}
	return result
}

func sanitizeDefaultValue(value string, spec ParamSpec) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if spec.Required {
		return ""
	}
	normalized, ok := normalizeParamValue(value, spec)
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", normalized))
}

func isEmptyParamValue(value any) bool {
	if value == nil {
		return true
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value)) == ""
}

func decodeChatRequest(r *http.Request) (ChatRequest, error) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return ChatRequest{}, fmt.Errorf("invalid json body: %w", err)
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return ChatRequest{}, errors.New("message is required")
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.SessionID == "" {
		req.SessionID = "default"
	}
	for i := range req.Intents {
		req.Intents[i].Name = strings.TrimSpace(req.Intents[i].Name)
		req.Intents[i].ModelType = normalizeModelType(req.Intents[i].ModelType)
		req.Intents[i].RouteDescription = strings.TrimSpace(req.Intents[i].RouteDescription)
		req.Intents[i].ExecutionDescription = strings.TrimSpace(req.Intents[i].ExecutionDescription)
		req.Intents[i].Description = strings.TrimSpace(req.Intents[i].Description)
		req.Intents[i].Params = sanitizeParamSpecs(req.Intents[i].Params)
	}
	return req, nil
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func writeSSE(w http.ResponseWriter, msg SSEMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func chatCompletionsURL(base string) string {
	u := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(u, "/chat/completions") {
		return u
	}
	return u + "/chat/completions"
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func summarizeIntentSpecs(intents []IntentSpec) string {
	if len(intents) == 0 {
		return "intents=0"
	}
	parts := make([]string, 0, len(intents))
	for _, intent := range intents {
		paramParts := make([]string, 0, len(intent.Params))
		for _, param := range intent.Params {
			paramParts = append(paramParts, fmt.Sprintf("%s:%s", param.Name, param.ValueType))
		}
		desc := firstNonEmpty(intent.RouteDescription, intent.Description, intent.ExecutionDescription)
		parts = append(parts, fmt.Sprintf("intent=%q route_desc=%q params=[%s]", intent.Name, desc, strings.Join(paramParts, ", ")))
	}
	return fmt.Sprintf("intents=%d %s", len(intents), strings.Join(parts, " | "))
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return strings.TrimSpace(*ptr)
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func cloneParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(params))
	for k, v := range params {
		cloned[k] = v
	}
	return cloned
}

func mergeParamMaps(base, overlay map[string]any) map[string]any {
	merged := cloneParams(base)
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func preview(s string, limit int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "))
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "...(truncated)"
}

func isGreetingMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return false
	}

	replacer := strings.NewReplacer(
		"!", "",
		"！", "",
		".", "",
		"。", "",
		",", "",
		"，", "",
		"?", "",
		"？", "",
		"~", "",
		"～", "",
		" ", "",
	)
	normalized = replacer.Replace(normalized)

	greetings := map[string]struct{}{
		"你好":            {},
		"您好":            {},
		"嗨":             {},
		"哈喽":            {},
		"hello":         {},
		"hi":            {},
		"hey":           {},
		"heythere":      {},
		"goodmorning":   {},
		"goodafternoon": {},
		"goodevening":   {},
		"hola":          {},
		"bonjour":       {},
		"salut":         {},
		"hallo":         {},
		"ciao":          {},
		"嘿":             {},
		"在吗":            {},
		"早上好":           {},
		"上午好":           {},
		"中午好":           {},
		"下午好":           {},
		"晚上好":           {},
	}
	_, ok := greetings[normalized]
	return ok
}

func buildGreetingReply(message string) string {
	if prefersChinese(message) {
		return "你好，我在。有什么可以帮你？"
	}
	return "Hello, I'm here. How can I help?"
}

func isCancelMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return false
	}

	replacer := strings.NewReplacer(
		"!", "",
		"！", "",
		".", "",
		"。", "",
		",", "",
		"，", "",
		"?", "",
		"？", "",
		"~", "",
		"～", "",
		" ", "",
	)
	normalized = replacer.Replace(normalized)

	cancelPhrases := map[string]struct{}{
		"取消":        {},
		"取消吧":       {},
		"算了":        {},
		"不用了":       {},
		"停止":        {},
		"结束":        {},
		"终止":        {},
		"放弃":        {},
		"cancel":    {},
		"cancelit":  {},
		"nevermind": {},
		"forgetit":  {},
		"stop":      {},
		"abort":     {},
	}
	_, ok := cancelPhrases[normalized]
	return ok
}

func buildCancelReply(message string) string {
	if prefersChinese(message) {
		return "已取消当前未完成任务。"
	}
	return "The current pending task has been canceled."
}

func buildMissingQuestion(message string, missing []string) string {
	if prefersChinese(message) {
		return fmt.Sprintf("请补充参数：%s", strings.Join(missing, ", "))
	}
	return fmt.Sprintf("Please provide: %s", strings.Join(missing, ", "))
}

func defaultMissingParamLabel(message string) string {
	if prefersChinese(message) {
		return "未识别参数"
	}
	return "missing_parameter"
}

func prefersChinese(message string) bool {
	for _, r := range message {
		if r >= '\u4e00' && r <= '\u9fa5' {
			return true
		}
	}
	return false
}

func maxConfidence(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func toFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, err := v.Float64()
		if err == nil {
			return n
		}
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return n
		}
	}
	return 0
}

func toBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
