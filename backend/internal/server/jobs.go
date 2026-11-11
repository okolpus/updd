package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"semanticatlas/backend/internal/ai"
	"semanticatlas/backend/internal/analysis"
	"semanticatlas/backend/internal/store"
)

// Progress (in percent) reached when a stage starts. The UI shows the same five steps.
var stageProgress = map[string]int{"extract": 5, "entities": 20, "relations": 45, "graph": 75, "ai": 82, "report": 90}

// placeholderAnalysis is stored while a document is still being analysed.
func placeholderAnalysis() string {
	raw, _ := json.Marshal(analysis.Result{Method: analysis.Method, Limitations: analysis.Limitations,
		Entities: []analysis.Entity{}, Edges: []analysis.Edge{}, Chapters: []analysis.Chapter{}, Keywords: []analysis.Keyword{}})
	return string(raw)
}

func normalizeDepth(depth string) string {
	if depth == "basic" {
		return "basic"
	}
	return "full"
}

// enqueue analyses a document in the background. At most two analyses run at once.
func (s *Server) enqueue(id string) {
	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		s.slots <- struct{}{}
		defer func() { <-s.slots }()
		s.runAnalysis(id)
	}()
}

func (s *Server) runAnalysis(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("analysis %s panicked: %v", id, r)
			s.store.FailAnalysis(ctx, id, "Не удалось проанализировать текст. Попробуйте запустить анализ ещё раз.")
		}
	}()
	doc, err := s.store.Document(ctx, id)
	if err != nil {
		log.Printf("analysis %s: %v", id, err)
		return
	}
	stage := func(name, note string) {
		if err := s.store.SetProgress(ctx, id, stageProgress[name], name, note); err != nil {
			log.Printf("analysis %s progress: %v", id, err)
		}
		if s.stageDelay > 0 {
			time.Sleep(s.stageDelay)
		}
	}
	stage("extract", fmt.Sprintf("Слов в тексте: %d", len(analysis.Word.FindAllStringIndex(doc.Content, -1))))
	useAI := doc.AIModel != "" && s.cfg.AI != nil
	result := analysis.AnalyzeWith(doc.Content, analysis.Options{Depth: doc.Depth, Progress: func(name, note string) {
		if name == "report" && useAI {
			return // The report stage follows the model's refinement.
		}
		stage(name, note)
	}})
	if useAI {
		stage("ai", "Qwen проверяет найденные сущности…")
		err := ai.Refine(ctx, s.cfg.AI, &result, func(note string) { s.store.SetProgress(ctx, id, stageProgress["ai"], "ai", note) })
		if err != nil {
			log.Printf("analysis %s: language model: %v", id, err)
		}
		stage("report", "")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		s.store.FailAnalysis(ctx, id, "Не удалось сохранить результат анализа.")
		return
	}
	persons := 0
	for _, e := range result.Entities {
		if e.Type == "person" {
			persons++
		}
	}
	note := fmt.Sprintf("Персонажей: %d · сущностей: %d · связей: %d", persons, len(result.Entities), len(result.Edges))
	if result.AI != nil {
		if result.AI.Error != "" {
			note += " · Qwen не ответила, использован локальный анализ"
		} else {
			note += " · проверено " + result.AI.Model
		}
	}
	if err := s.store.FinishAnalysis(ctx, id, string(raw), note); err != nil {
		log.Printf("analysis %s finish: %v", id, err)
	}
}

// resumeInterrupted restarts analyses that were running when the server stopped.
func (s *Server) resumeInterrupted() {
	ids, err := s.store.ProcessingIDs(context.Background())
	if err != nil {
		log.Printf("resume analyses: %v", err)
		return
	}
	for _, id := range ids {
		s.enqueue(id)
	}
}

// ready rejects operations that need a finished analysis.
func ready(doc *store.Document) bool { return doc.Status == "" || doc.Status == "ready" }
