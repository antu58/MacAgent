package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type config struct {
	webPort        string
	gatewayBaseURL string
}

type chatRequest struct {
	SessionID string       `json:"session_id"`
	Message   string       `json:"message"`
	Intents   []intentSpec `json:"intents"`
}

type intentSpec struct {
	Name                 string      `json:"name"`
	ModelType            string      `json:"model_type,omitempty"`
	RouteDescription     string      `json:"route_description,omitempty"`
	ExecutionDescription string      `json:"execution_description,omitempty"`
	Description          string      `json:"description,omitempty"`
	Params               []paramSpec `json:"params"`
}

type paramSpec struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ValueType    string   `json:"value_type"`
	Required     bool     `json:"required"`
	IsEnum       bool     `json:"is_enum"`
	EnumValues   []string `json:"enum_values,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
}

func main() {
	cfg := config{
		webPort:        getenv("WEB_PORT", "19091"),
		gatewayBaseURL: strings.TrimRight(getenv("GATEWAY_BASE_URL", "http://127.0.0.1:19090"), "/"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/config", handleConfig(cfg))
	mux.HandleFunc("/api/route", handleRoute(cfg))
	mux.HandleFunc("/api/chat", handleChat(cfg))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	server := &http.Server{
		Addr:              ":" + cfg.webPort,
		Handler:           withLog(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("gateway-web running at :%s", cfg.webPort)
	log.Printf("proxy target: %s", cfg.gatewayBaseURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "static/index.html")
}

func handleConfig(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"gateway_base_url": cfg.gatewayBaseURL,
			"default_intents":  []intentSpec{},
		})
	}
}

func handleRoute(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		reqBody, err := decodeRequest(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[web][route][session=%s] %s", reqBody.SessionID, summarizeIntents(reqBody.Intents))

		target := cfg.gatewayBaseURL + "/route"
		raw, status, err := forwardJSON(r.Context(), target, reqBody)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(raw)
	}
}

func handleChat(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		reqBody, err := decodeRequest(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[web][chat][session=%s] %s", reqBody.SessionID, summarizeIntents(reqBody.Intents))

		body, err := json.Marshal(reqBody)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		target := cfg.gatewayBaseURL + "/chat"
		upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		upReq.Header.Set("Content-Type", "application/json")

		// Keep chat proxy timeout open-ended for long-form model outputs.
		client := &http.Client{}
		resp, err := client.Do(upReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(resp.StatusCode)

		if _, err := io.Copy(w, resp.Body); err != nil {
			log.Printf("stream copy error: %v", err)
		}
	}
}

func decodeRequest(r io.Reader) (chatRequest, error) {
	var req chatRequest
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return chatRequest{}, fmt.Errorf("invalid request json: %w", err)
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return chatRequest{}, fmt.Errorf("message is required")
	}
	if req.SessionID == "" {
		req.SessionID = "web-user"
	}
	for i := range req.Intents {
		req.Intents[i].Name = strings.TrimSpace(req.Intents[i].Name)
		req.Intents[i].ModelType = strings.TrimSpace(req.Intents[i].ModelType)
		req.Intents[i].RouteDescription = strings.TrimSpace(req.Intents[i].RouteDescription)
		req.Intents[i].ExecutionDescription = strings.TrimSpace(req.Intents[i].ExecutionDescription)
		req.Intents[i].Description = strings.TrimSpace(req.Intents[i].Description)
		for j := range req.Intents[i].Params {
			req.Intents[i].Params[j].Name = strings.TrimSpace(req.Intents[i].Params[j].Name)
			req.Intents[i].Params[j].Description = strings.TrimSpace(req.Intents[i].Params[j].Description)
			req.Intents[i].Params[j].ValueType = strings.TrimSpace(req.Intents[i].Params[j].ValueType)
			req.Intents[i].Params[j].DefaultValue = strings.TrimSpace(req.Intents[i].Params[j].DefaultValue)
		}
	}
	return req, nil
}

func forwardJSON(ctx context.Context, url string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return raw, resp.StatusCode, nil
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func summarizeIntents(intents []intentSpec) string {
	if len(intents) == 0 {
		return "intents=0"
	}
	parts := make([]string, 0, len(intents))
	for _, intent := range intents {
		paramParts := make([]string, 0, len(intent.Params))
		for _, param := range intent.Params {
			paramParts = append(paramParts, fmt.Sprintf("%s:%s", param.Name, param.ValueType))
		}
		desc := strings.TrimSpace(intent.RouteDescription)
		if desc == "" {
			desc = strings.TrimSpace(intent.Description)
		}
		if desc == "" {
			desc = strings.TrimSpace(intent.ExecutionDescription)
		}
		parts = append(parts, fmt.Sprintf("intent=%q route_desc=%q params=[%s]", intent.Name, desc, strings.Join(paramParts, ", ")))
	}
	return fmt.Sprintf("intents=%d %s", len(intents), strings.Join(parts, " | "))
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
