package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"semanticatlas/backend/internal/llm"
)

// fakeOllama imitates the Ollama API with deterministic answers.
func fakeOllama(t *testing.T, broken bool) (*httptest.Server, *int32) {
	return fakeOllamaSwitch(t, broken, nil)
}

func fakeOllamaSwitch(t *testing.T, broken bool, down *atomic.Bool) (*httptest.Server, *int32) {
	var chats int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down != nil && down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3:4b"},{"name":"llama3:8b"}]}`))
		case "/api/chat":
			atomic.AddInt32(&chats, 1)
			if broken {
				http.Error(w, `{"error":"model crashed"}`, http.StatusInternalServerError)
				return
			}
			var req struct {
				Model    string                     `json:"model"`
				Format   string                     `json:"format"`
				Think    *bool                      `json:"think"`
				Messages []struct{ Content string } `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Model != "qwen3:4b" || req.Think == nil || *req.Think {
				t.Errorf("unexpected request: %+v", req)
			}
			prompt := req.Messages[len(req.Messages)-1].Content
			reply := func(content string) {
				json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": content}})
			}
			if req.Format != "json" {
				reply("<think>ok</think>Иван передал Анне письмо о научной экспедиции [1].")
				return
			}
			var items []map[string]any
			json.Unmarshal([]byte(prompt[strings.LastIndex(prompt, "\n\n[")+2:]), &items)
			var out []map[string]string
			if strings.Contains(prompt, `"entities"`) {
				for _, it := range items {
					name, typ := it["name"].(string), it["type"].(string)
					switch {
					case typ == "concept":
						typ = "noise" // every concept is treated as noise by this fake
					case name == "Мария":
						name = "Иван" // forces a merge
					}
					out = append(out, map[string]string{"id": it["id"].(string), "type": typ, "name": name, "description": "Роль по цитатам."})
				}
				raw, _ := json.Marshal(map[string]any{"entities": out})
				reply("```json\n" + string(raw) + "\n```")
				return
			}
			for _, it := range items {
				out = append(out, map[string]string{"id": it["id"].(string), "type": "family", "label": "брат и сестра", "description": "В цитатах они действуют вместе."})
			}
			raw, _ := json.Marshal(map[string]any{"edges": out})
			reply(string(raw))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &chats
}

func aiServer(t *testing.T, ollamaURL string) *client {
	model := llm.New(ollamaURL, "qwen3:4b")
	model.CacheTTL = time.Millisecond
	s, err := New(Config{DataDir: t.TempDir(), SecretKey: "test-only-secret-key-which-is-long-enough", AI: model, AIPoll: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(func() { ts.Close(); s.Close() })
	c := newClient(t, ts)
	c.register()
	return c
}

type aiDoc struct {
	Status   string `json:"status"`
	Note     string `json:"note"`
	AIModel  string `json:"ai_model"`
	Analysis struct {
		Entities []struct {
			ID, Label, Type, Description string
			Count                        int
			ByAI                         bool `json:"by_ai"`
		}
		Edges []struct {
			Source, Target, Type, Label, Description string
			ByAI                                     bool `json:"by_ai"`
		}
		AI *struct {
			Model                            string
			Entities, Merged, Removed, Edges int
			Error                            string
		}
	}
}

func TestQwenRefinesAnalysisAndAnswers(t *testing.T) {
	ollama, chats := fakeOllama(t, false)
	c := aiServer(t, ollama.URL)
	var st map[string]any
	c.do("GET", "/api/ai", nil).json(t, &st)
	if st["available"] != true || st["model"] != "qwen3:4b" {
		t.Fatalf("status: %v", st)
	}
	r := c.do("POST", "/api/documents", map[string]any{"title": "С Qwen", "content": sampleText})
	var created doc
	r.json(t, &created)
	c.waitReady(created.ID)
	var d aiDoc
	c.do("GET", "/api/documents/"+created.ID, nil).json(t, &d)
	a := d.Analysis
	if d.AIModel != "qwen3:4b" || a.AI == nil || a.AI.Error != "" || a.AI.Merged != 1 || a.AI.Removed == 0 || a.AI.Edges == 0 {
		t.Fatalf("ai info: %+v note=%s", a.AI, d.Note)
	}
	if !strings.Contains(d.Note, "проверено qwen3:4b") {
		t.Fatalf("note: %s", d.Note)
	}
	ids := map[string]bool{}
	for _, e := range a.Entities {
		ids[e.ID] = true
		if e.Type == "concept" || e.Label == "Мария" {
			t.Fatalf("noise or merged entity left: %+v", e)
		}
		if !e.ByAI || !strings.Contains(e.Description, "Qwen") {
			t.Fatalf("entity not refined: %+v", e)
		}
	}
	labelled := false
	for _, e := range a.Edges {
		if !ids[e.Source] || !ids[e.Target] || e.Source == e.Target {
			t.Fatalf("dangling edge after merge: %+v", e)
		}
		if e.ByAI && e.Label == "брат и сестра" && e.Description != "" {
			labelled = true
		}
	}
	if !labelled {
		t.Fatal("no edge labelled by the model")
	}
	r = c.do("POST", "/api/documents/"+created.ID+"/questions", map[string]string{"question": "Что передал Иван?"})
	var q struct {
		Answer, Mode string
		Sources      []struct{ Text string }
	}
	r.json(t, &q)
	if q.Mode != "ai:qwen3:4b" || !strings.HasPrefix(q.Answer, "Иван передал") || len(q.Sources) == 0 {
		t.Fatalf("question: %+v", q)
	}
	if atomic.LoadInt32(chats) < 3 {
		t.Fatalf("expected several model calls, got %d", *chats)
	}
}

func TestQwenFailureKeepsLocalAnalysis(t *testing.T) {
	ollama, _ := fakeOllama(t, true)
	c := aiServer(t, ollama.URL)
	r := c.do("POST", "/api/documents", map[string]any{"title": "Сбой", "content": sampleText})
	var created doc
	r.json(t, &created)
	ready := c.waitReady(created.ID)
	if len(ready.Analysis.Entities) == 0 {
		t.Fatal("local analysis lost")
	}
	var d aiDoc
	c.do("GET", "/api/documents/"+created.ID, nil).json(t, &d)
	if d.Analysis.AI == nil || d.Analysis.AI.Error == "" || !strings.Contains(d.Note, "не ответила") {
		t.Fatalf("failure not reported: %+v %s", d.Analysis.AI, d.Note)
	}
	r = c.do("POST", "/api/documents/"+created.ID+"/questions", map[string]string{"question": "Что передал Иван?"})
	if !strings.Contains(string(r.body), `"mode":"extractive"`) || !strings.Contains(string(r.body), "Qwen не ответила") {
		t.Fatalf("fallback answer: %s", r.body)
	}
}

func TestEveryAnalysisGoesThroughQwenOnceItIsUp(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	ollama, _ := fakeOllamaSwitch(t, false, &down)
	c := aiServer(t, ollama.URL)
	var st map[string]any
	c.do("GET", "/api/ai", nil).json(t, &st)
	if st["available"] != false {
		t.Fatalf("status while down: %v", st)
	}
	// Qwen is down: the text is still analysed locally and marked as waiting for Qwen.
	r := c.do("POST", "/api/documents", map[string]any{"title": "Ждёт Qwen", "content": sampleText})
	if r.status != 201 {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	var created doc
	r.json(t, &created)
	c.waitReady(created.ID)
	var d aiDoc
	c.do("GET", "/api/documents/"+created.ID, nil).json(t, &d)
	if d.AIModel != "pending" || !strings.Contains(d.Note, "перепроверится") {
		t.Fatalf("pending: %s %s", d.AIModel, d.Note)
	}
	// Qwen comes online: the watcher repeats the analysis with it.
	down.Store(false)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c.do("GET", "/api/documents/"+created.ID, nil).json(t, &d)
		if d.Status == "ready" && d.AIModel == "qwen3:4b" && d.Analysis.AI != nil {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("not re-analysed with Qwen: %s %s %+v", d.Status, d.AIModel, d.Analysis.AI)
}

func TestQwenOff(t *testing.T) {
	c := newClient(t, newTestServer(t)) // AI nil = ATLAS_AI=off
	c.register()
	var st map[string]any
	c.do("GET", "/api/ai", nil).json(t, &st)
	if st["enabled"] != false {
		t.Fatalf("status: %v", st)
	}
	d := c.document(sampleText)
	var full aiDoc
	c.do("GET", "/api/documents/"+d.ID, nil).json(t, &full)
	if full.AIModel != "" {
		t.Fatalf("ai model with AI off: %q", full.AIModel)
	}
}
