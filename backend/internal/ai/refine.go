// Package ai refines the offline analysis with a local language model (Qwen via Ollama).
// The model only sees found entities and evidence sentences, never decides on its own
// what the evidence is: every quote still comes from the source text.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"semanticatlas/backend/internal/analysis"
	"semanticatlas/backend/internal/llm"
)

const system = `Ты помощник литературоведа и анализируешь русский текст. Опирайся только на приведённые цитаты, ничего не выдумывай. Отвечай строго одним JSON-объектом без пояснений.`

var entityTypes = map[string]bool{"person": true, "place": true, "event": true, "concept": true}

// Edge types the model may assign, with the Russian label shown when it gives none.
var edgeTypes = map[string]string{
	"family": "Родство", "romantic": "Романтические отношения", "friendship": "Дружба", "conflict": "Конфликт",
	"professional": "Работа или учёба", "social": "Взаимодействие", "location": "Связь с местом",
	"causal": "Причинная связь", "temporal": "Временная связь", "thematic": "Тематическая близость",
	"cooccurrence": "Совместное упоминание",
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// Refine checks entity names and types, merges duplicates, removes noise and labels
// the strongest links. Progress receives short notes for the user. On error the
// result keeps everything already applied and records the error in result.AI.
func Refine(ctx context.Context, client *llm.Client, r *analysis.Result, progress func(string)) error {
	info := &analysis.AIInfo{Model: client.Model}
	r.AI = info
	if progress == nil {
		progress = func(string) {}
	}
	progress(fmt.Sprintf("Qwen проверяет сущности: %d", len(r.Entities)))
	if err := refineEntities(ctx, client, r, info); err != nil {
		info.Error = err.Error()
		analysis.RecomputeThemes(r)
		return err
	}
	progress(fmt.Sprintf("Qwen подписывает связи · сущностей: %d", len(r.Entities)))
	if err := refineEdges(ctx, client, r, info); err != nil {
		info.Error = err.Error()
		analysis.RecomputeThemes(r)
		return err
	}
	analysis.RecomputeThemes(r)
	return nil
}

type entityVerdict struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func refineEntities(ctx context.Context, client *llm.Client, r *analysis.Result, info *analysis.AIInfo) error {
	verdicts := map[string]entityVerdict{}
	for start := 0; start < len(r.Entities); start += 20 {
		batch := r.Entities[start:min(start+20, len(r.Entities))]
		type item struct {
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Type   string   `json:"type"`
			Quotes []string `json:"quotes"`
		}
		items := make([]item, len(batch))
		for i, e := range batch {
			var quotes []string
			for _, q := range e.Quotes[:min(2, len(e.Quotes))] {
				quotes = append(quotes, clip(q.Text, 260))
			}
			items[i] = item{e.ID, e.Label, e.Type, quotes}
		}
		list, _ := json.Marshal(items)
		prompt := `Ниже сущности, автоматически найденные в тексте, с цитатами. Для каждой определи:
- type: person (персонаж), place (место), event (событие), concept (понятие) или noise (ошибка распознавания, служебное слово, не сущность);
- name: правильное название в именительном падеже, для персонажа — полное имя, как в тексте; если это тот же персонаж или место, что и другая сущность списка, дай им одинаковое name;
- description: одно короткое предложение о роли сущности по цитатам.
Верни {"entities":[{"id":"…","type":"…","name":"…","description":"…"}]} для всех id.

` + string(list)
		var out struct {
			Entities []entityVerdict `json:"entities"`
		}
		if err := client.JSON(ctx, system, prompt, &out); err != nil {
			return err
		}
		for _, v := range out.Entities {
			verdicts[v.ID] = v
		}
	}
	apply(r, verdicts, info)
	return nil
}

// apply renames, removes and merges entities according to the model's verdicts.
func apply(r *analysis.Result, verdicts map[string]entityVerdict, info *analysis.AIInfo) {
	remap := map[string]string{} // removed or merged id -> surviving id ("" = removed)
	keepIdx := map[string]int{}  // type|name -> index in kept
	var kept []analysis.Entity
	for _, e := range r.Entities {
		v, ok := verdicts[e.ID]
		if ok && v.Type == "noise" {
			remap[e.ID] = ""
			info.Removed++
			continue
		}
		if ok {
			if entityTypes[v.Type] {
				e.Type = v.Type
			}
			if name := strings.TrimSpace(v.Name); name != "" && utf8.RuneCountInString(name) <= 60 {
				e.Label = name
			}
			if d := strings.TrimSpace(v.Description); d != "" {
				e.Description = clip(d, 300) + " (описание Qwen, проверьте по цитатам)"
			}
			e.ByAI = true
			info.Entities++
		}
		key := e.Type + "|" + strings.ToLower(e.Label)
		if i, dup := keepIdx[key]; dup {
			target := &kept[i]
			target.Count += e.Count
			for _, q := range e.Quotes {
				if len(target.Quotes) < 5 && !hasQuote(target.Quotes, q.Text) {
					target.Quotes = append(target.Quotes, q)
				}
			}
			remap[e.ID] = target.ID
			info.Merged++
			continue
		}
		keepIdx[key] = len(kept)
		kept = append(kept, e)
	}
	r.Entities = kept
	for i := range kept {
		if kept[i].ByAI && !strings.Contains(kept[i].Description, "Упоминаний") {
			kept[i].Description += fmt.Sprintf(" Упоминаний: %d.", kept[i].Count)
		}
	}
	resolve := func(id string) (string, bool) {
		if to, moved := remap[id]; moved {
			return to, to != ""
		}
		return id, true
	}
	// Links: redirect merged ends, drop removed ones and self-links, combine duplicates.
	type key struct{ a, b string }
	byKey := map[key]int{}
	var edges []analysis.Edge
	for _, e := range r.Edges {
		a, okA := resolve(e.Source)
		b, okB := resolve(e.Target)
		if !okA || !okB || a == b {
			continue
		}
		k := key{min(a, b), max(a, b)}
		if i, dup := byKey[k]; dup {
			edges[i].Weight += e.Weight
			for _, q := range e.Evidence {
				if len(edges[i].Evidence) < 3 && !contains(edges[i].Evidence, q) {
					edges[i].Evidence = append(edges[i].Evidence, q)
				}
			}
			continue
		}
		e.Source, e.Target = a, b
		byKey[k] = len(edges)
		edges = append(edges, e)
	}
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].Weight > edges[j].Weight })
	for i := range edges {
		edges[i].ID = fmt.Sprintf("r-%d", i+1)
	}
	r.Edges = edges
	for ci := range r.Chapters {
		counts := map[string]int{}
		for id, n := range r.Chapters[ci].EntityCounts {
			if to, ok := resolve(id); ok {
				counts[to] += n
			}
		}
		r.Chapters[ci].EntityCounts = counts
	}
}

type edgeVerdict struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func refineEdges(ctx context.Context, client *llm.Client, r *analysis.Result, info *analysis.AIInfo) error {
	labels := map[string]analysis.Entity{}
	for _, e := range r.Entities {
		labels[e.ID] = e
	}
	// The strongest links that involve a character or a place are worth a model call.
	var picked []int
	for i, e := range r.Edges {
		if len(picked) >= 40 {
			break
		}
		a, b := labels[e.Source], labels[e.Target]
		if a.Type == "person" || b.Type == "person" || a.Type == "place" || b.Type == "place" {
			picked = append(picked, i)
		}
	}
	for start := 0; start < len(picked); start += 10 {
		batch := picked[start:min(start+10, len(picked))]
		type item struct {
			ID       string   `json:"id"`
			A        string   `json:"a"`
			B        string   `json:"b"`
			Evidence []string `json:"evidence"`
		}
		items := make([]item, len(batch))
		for i, idx := range batch {
			e := r.Edges[idx]
			var ev []string
			for _, q := range e.Evidence {
				ev = append(ev, clip(q, 260))
			}
			items[i] = item{e.ID, labels[e.Source].Label, labels[e.Target].Label, ev}
		}
		list, _ := json.Marshal(items)
		prompt := `Для каждой пары сущностей определи связь только по цитатам-доказательствам.
type — одно из: family, romantic, friendship, conflict, professional, social, location, causal, temporal, thematic, cooccurrence (если цитаты не показывают связи, а только упоминают обоих).
label — короткая подпись по-русски (2–4 слова), например «брат и сестра», «вместе ищут карту», «живёт в».
description — одно предложение, что именно сказано в цитатах.
Верни {"edges":[{"id":"…","type":"…","label":"…","description":"…"}]} для всех id.

` + string(list)
		var out struct {
			Edges []edgeVerdict `json:"edges"`
		}
		if err := client.JSON(ctx, system, prompt, &out); err != nil {
			return err
		}
		byID := map[string]edgeVerdict{}
		for _, v := range out.Edges {
			byID[v.ID] = v
		}
		for _, idx := range batch {
			e := &r.Edges[idx]
			v, ok := byID[e.ID]
			if !ok {
				continue
			}
			fallback, known := edgeTypes[v.Type]
			if !known {
				continue
			}
			e.Type = v.Type
			e.Label = fallback
			if l := strings.TrimSpace(v.Label); l != "" && utf8.RuneCountInString(l) <= 40 && v.Type != "cooccurrence" {
				e.Label = l
			}
			if d := strings.TrimSpace(v.Description); d != "" {
				e.Description = clip(d, 300)
			}
			e.ByAI = true
			info.Edges++
		}
	}
	return nil
}

// Answer writes a short answer from retrieved quotes only, citing them as [1], [2]…
func Answer(ctx context.Context, client *llm.Client, question string, r analysis.Result) (string, []analysis.Quote, error) {
	sources := analysis.Retrieve(question, r, 6)
	if len(sources) == 0 {
		return "В тексте не найдено фрагментов с ключевыми словами вопроса. Уточните имя, место или понятие.", sources, nil
	}
	var b strings.Builder
	for i, s := range sources {
		fmt.Fprintf(&b, "[%d] (%s) %s\n", i+1, s.Chapter, clip(s.Text, 600))
	}
	prompt := "Вопрос: " + question + "\n\nЦитаты из текста:\n" + b.String() +
		"\nОтветь по-русски в 2–4 предложениях только на основе цитат и ссылайся на них номерами в квадратных скобках. " +
		"Если в цитатах нет ответа, прямо скажи, что в найденных фрагментах ответа нет."
	answer, err := client.Text(ctx, `Ты помощник читателя. Отвечай только по приведённым цитатам, не добавляй фактов от себя.`, prompt)
	if err != nil {
		return "", sources, err
	}
	return strings.TrimSpace(answer), sources, nil
}

func hasQuote(quotes []analysis.Quote, text string) bool {
	for _, q := range quotes {
		if q.Text == text {
			return true
		}
	}
	return false
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
