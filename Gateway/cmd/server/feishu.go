package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type FeishuBridge struct {
	gw        *Gateway
	apiClient *lark.Client
	wsClient  *larkws.Client
	deduper   *ttlDeduper
}

type ttlDeduper struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]time.Time
}

type feishuIncomingMessage struct {
	MessageID string
	SessionID string
	Text      string
	Type      string
	ChatID    string
}

type feishuTextContent struct {
	Text string `json:"text"`
}

func newFeishuBridge(gw *Gateway) (*FeishuBridge, error) {
	if gw == nil {
		return nil, errors.New("gateway is nil")
	}
	if !gw.cfg.FeishuSocketEnabled {
		return nil, errors.New("feishu socket is disabled")
	}
	if strings.TrimSpace(gw.cfg.FeishuAppID) == "" {
		return nil, errors.New("FEISHU_APP_ID is required when FEISHU_SOCKET_ENABLED=1")
	}
	if strings.TrimSpace(gw.cfg.FeishuAppSecret) == "" {
		return nil, errors.New("FEISHU_APP_SECRET is required when FEISHU_SOCKET_ENABLED=1")
	}

	dispatch := dispatcher.NewEventDispatcher("", "")
	bridge := &FeishuBridge{
		gw:      gw,
		deduper: newTTLDeduper(6 * time.Hour),
	}
	dispatch.OnP2MessageReceiveV1(bridge.handleMessageReceive)

	clientOptions := []lark.ClientOptionFunc{
		lark.WithAppType(larkcore.AppTypeSelfBuilt),
		lark.WithReqTimeout(30 * time.Second),
	}
	wsOptions := []larkws.ClientOption{
		larkws.WithEventHandler(dispatch),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	}

	if baseURL := strings.TrimRight(strings.TrimSpace(gw.cfg.FeishuOpenBaseURL), "/"); baseURL != "" {
		clientOptions = append(clientOptions, lark.WithOpenBaseUrl(baseURL))
		wsOptions = append(wsOptions, larkws.WithDomain(baseURL))
	}

	bridge.apiClient = lark.NewClient(gw.cfg.FeishuAppID, gw.cfg.FeishuAppSecret, clientOptions...)
	bridge.wsClient = larkws.NewClient(gw.cfg.FeishuAppID, gw.cfg.FeishuAppSecret, wsOptions...)
	return bridge, nil
}

func (b *FeishuBridge) Start(ctx context.Context) error {
	log.Printf("[feishu] socket client starting")
	return b.wsClient.Start(ctx)
}

func (b *FeishuBridge) handleMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	incoming, ok := extractFeishuIncomingMessage(event)
	if !ok {
		return nil
	}
	if !b.deduper.Mark(incoming.MessageID) {
		log.Printf("[feishu] skip duplicate message_id=%s", incoming.MessageID)
		return nil
	}

	go b.processMessage(context.Background(), incoming)
	return nil
}

func (b *FeishuBridge) processMessage(ctx context.Context, incoming feishuIncomingMessage) {
	timeoutSec := b.gw.cfg.DeepTimeoutSec
	if b.gw.cfg.ExecutionTimeoutSec > timeoutSec {
		timeoutSec = b.gw.cfg.ExecutionTimeoutSec
	}
	if timeoutSec < b.gw.cfg.IntentTimeoutSec {
		timeoutSec = b.gw.cfg.IntentTimeoutSec
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec+15)*time.Second)
	defer cancel()

	reply := ""
	switch incoming.Type {
	case "text":
		reply = b.handleTextMessage(ctx, incoming)
	default:
		reply = buildUnsupportedMessageReply(incoming.Text)
	}
	if strings.TrimSpace(reply) == "" {
		reply = buildGatewayErrorReply(incoming.Text)
	}

	if err := b.sendTextToChat(ctx, incoming.ChatID, reply); err != nil {
		log.Printf("[feishu] reply failed message_id=%s err=%v", incoming.MessageID, err)
	}
}

func (b *FeishuBridge) handleTextMessage(ctx context.Context, incoming feishuIncomingMessage) string {
	if strings.TrimSpace(incoming.Text) == "" {
		return buildUnsupportedMessageReply(incoming.Text)
	}

	req := ChatRequest{
		SessionID: incoming.SessionID,
		Message:   incoming.Text,
		Intents:   nil,
	}

	b.gw.memStore.Cleanup()
	mem, ok := b.gw.memStore.Get(req.SessionID)
	var memPtr *SessionMemory
	if ok {
		memPtr = &mem
	}

	skillCatalog := buildSkillCatalog(b.gw.defaultIntents)
	result, err := b.gw.resolveFrontendResult(ctx, req, memPtr, skillCatalog, nil)
	if err != nil {
		log.Printf("[feishu] resolve failed session=%s err=%v", req.SessionID, err)
		return buildGatewayErrorReply(req.Message)
	}
	return renderFeishuReply(result, req.Message)
}

func (b *FeishuBridge) sendTextToChat(ctx context.Context, chatID, text string) error {
	chatID = strings.TrimSpace(chatID)
	text = strings.TrimSpace(text)
	if chatID == "" {
		return errors.New("chat_id is empty")
	}
	if text == "" {
		return errors.New("text is empty")
	}

	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(
			larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(chatID).
				MsgType("text").
				Content(string(content)).
				Uuid(fmt.Sprintf("gateway-feishu-send-%d", time.Now().UnixNano())).
				Build(),
		).
		Build()

	resp, err := b.apiClient.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("code=%d msg=%s", resp.Code, resp.Msg)
	}
	log.Printf("[feishu] message sent chat_id=%s", chatID)
	return nil
}

func extractFeishuIncomingMessage(event *larkim.P2MessageReceiveV1) (feishuIncomingMessage, bool) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return feishuIncomingMessage{}, false
	}

	message := event.Event.Message
	messageID := stringValue(message.MessageId)
	messageType := strings.TrimSpace(stringValue(message.MessageType))
	if messageID == "" || messageType == "" {
		return feishuIncomingMessage{}, false
	}

	text := ""
	if messageType == "text" {
		text = extractFeishuText(message.Content)
		if strings.TrimSpace(text) == "" {
			return feishuIncomingMessage{}, false
		}
	}

	return feishuIncomingMessage{
		MessageID: messageID,
		SessionID: buildFeishuSessionID(event),
		Text:      text,
		Type:      messageType,
		ChatID:    stringValue(message.ChatId),
	}, true
}

func buildFeishuSessionID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil {
		return "feishu:default"
	}

	scope := "unknown_scope"
	if event.Event.Message != nil {
		scope = firstNonEmpty(
			stringValue(event.Event.Message.ThreadId),
			stringValue(event.Event.Message.ChatId),
			scope,
		)
	}

	sender := "unknown_sender"
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		sender = firstNonEmpty(
			stringValue(event.Event.Sender.SenderId.OpenId),
			stringValue(event.Event.Sender.SenderId.UserId),
			stringValue(event.Event.Sender.SenderId.UnionId),
			sender,
		)
	}

	tenant := ""
	if event.Event.Sender != nil {
		tenant = stringValue(event.Event.Sender.TenantKey)
	}
	parts := []string{"feishu"}
	if tenant != "" {
		parts = append(parts, tenant)
	}
	parts = append(parts, scope, sender)
	return strings.Join(parts, ":")
}

func extractFeishuText(content *string) string {
	raw := stringValue(content)
	if raw == "" {
		return ""
	}

	var payload feishuTextContent
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return strings.TrimSpace(raw)
	}
	text := strings.TrimSpace(payload.Text)
	if text == "" {
		return ""
	}
	text = stripFeishuAtTags(text)
	return strings.TrimSpace(text)
}

func stripFeishuAtTags(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	for {
		start := strings.Index(text, "<at ")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "</at>")
		if end == -1 {
			break
		}
		end += start + len("</at>")
		text = strings.TrimSpace(text[:start] + " " + text[end:])
	}

	replacer := strings.NewReplacer("\u00a0", " ", "\n", " ", "\r", " ", "\t", " ")
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func renderFeishuReply(result FrontendResult, userMessage string) string {
	if strings.TrimSpace(result.Type) == "ask_user" {
		question := stringValue(result.Question)
		if question != "" {
			return question
		}
		if len(result.MissingParms) > 0 {
			return buildMissingQuestion(userMessage, result.MissingParms)
		}
	}

	response, ok := result.Response.(map[string]any)
	if !ok {
		return mustJSON(result)
	}

	switch strings.TrimSpace(fmt.Sprintf("%v", response["type"])) {
	case "assistant_message":
		content := strings.TrimSpace(fmt.Sprintf("%v", response["content"]))
		if content != "" {
			return content
		}
	case "intent_result":
		return renderIntentResultReply(response, userMessage)
	}

	if question := stringValue(result.Question); question != "" {
		return question
	}
	return mustJSON(result)
}

func renderIntentResultReply(response map[string]any, userMessage string) string {
	intent := strings.TrimSpace(fmt.Sprintf("%v", response["intent"]))
	description := strings.TrimSpace(fmt.Sprintf("%v", response["description"]))
	paramsJSON := prettyJSON(response["params"])

	if prefersChinese(userMessage) {
		lines := make([]string, 0, 4)
		if intent != "" {
			lines = append(lines, "已进入当前网关流程。")
			lines = append(lines, "intent: "+intent)
		}
		if description != "" {
			lines = append(lines, description)
		}
		if paramsJSON != "" && paramsJSON != "null" {
			lines = append(lines, "params:\n"+paramsJSON)
		}
		if len(lines) == 0 {
			return "已进入当前网关流程。"
		}
		return strings.Join(lines, "\n")
	}

	lines := make([]string, 0, 4)
	if intent != "" {
		lines = append(lines, "The message has entered the current gateway flow.")
		lines = append(lines, "intent: "+intent)
	}
	if description != "" {
		lines = append(lines, description)
	}
	if paramsJSON != "" && paramsJSON != "null" {
		lines = append(lines, "params:\n"+paramsJSON)
	}
	if len(lines) == 0 {
		return "The message has entered the current gateway flow."
	}
	return strings.Join(lines, "\n")
}

func prettyJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func buildUnsupportedMessageReply(userMessage string) string {
	if prefersChinese(userMessage) {
		return "当前飞书通道先只支持文本消息。"
	}
	return "This Feishu channel currently supports text messages only."
}

func buildGatewayErrorReply(userMessage string) string {
	if prefersChinese(userMessage) {
		return "网关处理失败，请稍后重试。"
	}
	return "Gateway processing failed. Please try again later."
}

func newTTLDeduper(ttl time.Duration) *ttlDeduper {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &ttlDeduper{
		ttl:  ttl,
		data: make(map[string]time.Time),
	}
}

func (d *ttlDeduper) Mark(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()

	for existingKey, ts := range d.data {
		if now.Sub(ts) > d.ttl {
			delete(d.data, existingKey)
		}
	}

	if ts, ok := d.data[key]; ok && now.Sub(ts) <= d.ttl {
		return false
	}
	d.data[key] = now
	return true
}
