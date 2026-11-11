// Package llm talks to a local Ollama server (for example running Qwen).
// Nothing leaves the computer: the default address is 127.0.0.1.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultURL   = "http://127.0.0.1:11434"
	DefaultModel = "qwen3:4b"
)

// Client is safe for concurrent use. A nil *Client means the feature is off.
type Client struct {
	URL   string
	Model string
	http  *http.Client

	// CacheTTL is how long a status check is reused (default 10s).
	CacheTTL time.Duration

	mu      sync.Mutex
	cached  Status
	checked time.Time
}

// Status describes whether the model can be used right now.
type Status struct {
	Enabled   bool     `json:"enabled"`
	Available bool     `json:"available"`
	Model     string   `json:"model"`
	URL       string   `json:"url"`
	Models    []string `json:"models"`
	Reason    string   `json:"reason,omitempty"`
}

func New(url, model string) *Client {
	if url == "" {
		url = DefaultURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{URL: strings.TrimRight(url, "/"), Model: model, http: &http.Client{Timeout: 10 * time.Minute}}
}

func sameModel(a, b string) bool {
	trim := func(s string) string { return strings.TrimSuffix(strings.ToLower(s), ":latest") }
	return trim(a) == trim(b)
}

// Status checks the Ollama server and the model, caching the answer for a few seconds.
func (c *Client) Status(ctx context.Context) Status {
	if c == nil {
		return Status{Reason: "Нейросеть отключена настройкой ATLAS_AI=off."}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := c.CacheTTL
	if ttl == 0 {
		ttl = 10 * time.Second
	}
	if time.Since(c.checked) < ttl {
		return c.cached
	}
	st := Status{Enabled: true, Model: c.Model, URL: c.URL, Models: []string{}}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/tags", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		st.Reason = "Ollama не запущена. Установите Ollama и запустите её, затем скачайте модель: ollama pull " + c.Model + "."
	} else {
		defer resp.Body.Close()
		var tags struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&tags) != nil {
			st.Reason = "Ollama ответила с ошибкой."
		} else {
			for _, m := range tags.Models {
				st.Models = append(st.Models, m.Name)
				if sameModel(m.Name, c.Model) {
					st.Available = true
				}
			}
			if !st.Available {
				st.Reason = "Модель " + c.Model + " не скачана. Выполните: ollama pull " + c.Model + "."
			}
		}
	}
	c.cached, c.checked = st, time.Now()
	return st
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) chat(ctx context.Context, system, prompt string, jsonMode bool) (string, error) {
	body := map[string]any{
		"model":    c.Model,
		"messages": []message{{"system", system}, {"user", prompt}},
		"stream":   false,
		"think":    false, // Qwen3: answer directly, without a long reasoning block.
		"options":  map[string]any{"temperature": 0.1, "num_ctx": 8192},
	}
	if jsonMode {
		body["format"] = "json"
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("нет связи с Ollama: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama вернула %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		Message message `json:"message"`
		Error   string  `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("непонятный ответ Ollama: %w", err)
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	return stripThinking(out.Message.Content), nil
}

// stripThinking removes a <think>…</think> block that some Qwen versions still emit.
func stripThinking(s string) string {
	if i := strings.Index(s, "</think>"); i >= 0 {
		s = s[i+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

// JSON asks for a JSON object and decodes it into out.
func (c *Client) JSON(ctx context.Context, system, prompt string, out any) error {
	text, err := c.chat(ctx, system, prompt, true)
	if err != nil {
		return err
	}
	text = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(text), "```"), "```json")
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start >= 0 && end > start {
		text = text[start : end+1]
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("модель вернула некорректный JSON: %w", err)
	}
	return nil
}

// Text asks for a plain-text answer.
func (c *Client) Text(ctx context.Context, system, prompt string) (string, error) {
	return c.chat(ctx, system, prompt, false)
}
