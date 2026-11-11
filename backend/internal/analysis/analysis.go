// Package analysis performs offline text analysis. All relations are
// evidence-backed hypotheses, never facts.
package analysis

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type Quote struct {
	Text    string `json:"text"`
	Chapter string `json:"chapter"`
}

type Entity struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Type        string  `json:"type"`
	Count       int     `json:"count"`
	Description string  `json:"description"`
	Quotes      []Quote `json:"quotes"`
	Degree      int     `json:"degree"`
}

type Edge struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Weight   int      `json:"weight"`
	Evidence []string `json:"evidence"`
	Inferred bool     `json:"inferred"`
}

type Chapter struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Text         string         `json:"text"`
	WordCount    int            `json:"word_count"`
	Themes       []string       `json:"themes"`
	EntityCounts map[string]int `json:"entity_counts"`
	EventCount   int            `json:"event_count"`
}

type Keyword struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

type Result struct {
	Method      string    `json:"method"`
	Limitations string    `json:"limitations"`
	Entities    []Entity  `json:"entities"`
	Edges       []Edge    `json:"edges"`
	Chapters    []Chapter `json:"chapters"`
	Keywords    []Keyword `json:"keywords"`
	WordCount   int       `json:"word_count"`
}

const (
	MaxEntities = 70
	MaxEdges    = 220
	Method      = "Локальный частотный и контекстный анализ на Go (стемминг Snowball, эвристики имён и мест, граф совместных упоминаний)"
	Limitations = "Эвристический анализ без словаря и нейросети: имена могут разделяться или объединяться неверно, редкие слова могут ошибочно попасть в понятия. Рёбра — предположения по совместным упоминаниям и языковым маркерам; они не доказывают родство, причины или мотивы. Вопросы обрабатываются поиском цитат без генеративной модели. Граф ограничен 70 сущностями и 220 связями."
)

var headingRe = regexp.MustCompile(`(?im)^\s*((?:глава|часть|раздел|chapter|part)\s+(?:[\divxlcIVXLC]+|[А-Яа-яЁё]+)[^\n]{0,100})\s*$`)

// SplitChapters splits by headings, or into bounded paragraph blocks for unstructured text.
func SplitChapters(text string) []Chapter {
	type raw struct{ title, body string }
	var parts []raw
	matches := headingRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) > 0 {
		if pre := strings.TrimSpace(text[:matches[0][0]]); pre != "" {
			parts = append(parts, raw{"Вступление", pre})
		}
		for i, m := range matches {
			end := len(text)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			parts = append(parts, raw{strings.TrimSpace(text[m[2]:m[3]]), strings.TrimSpace(text[m[1]:end])})
		}
	} else {
		runes := []rune(text)
		start := 0
		for start < len(runes) {
			end := min(start+10000, len(runes))
			if end < len(runes) {
				if gap := lastIndexRunes(runes, start+3000, end, []rune("\n\n")); gap > start {
					end = gap
				}
			}
			parts = append(parts, raw{fmt.Sprintf("Блок %d", len(parts)+1), strings.TrimSpace(string(runes[start:end]))})
			start = end
		}
	}
	chapters := []Chapter{}
	for i, p := range parts {
		if p.body == "" {
			continue
		}
		chapters = append(chapters, Chapter{ID: fmt.Sprintf("ch-%d", i+1), Title: p.title, Text: p.body,
			WordCount: len(Word.FindAllStringIndex(p.body, -1)), Themes: []string{}, EntityCounts: map[string]int{}})
	}
	return chapters
}

func lastIndexRunes(r []rune, from, to int, needle []rune) int {
	for i := to - len(needle); i >= from; i-- {
		match := true
		for j := range needle {
			if r[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Sentences splits after terminal punctuation or at blank lines. Each fragment
// is an exact substring of the input so quotes remain verifiable.
func Sentences(text string) []string {
	var out []string
	emit := func(fragment string) {
		fragment = strings.TrimSpace(fragment)
		if fragment == "" {
			return
		}
		runes := []rune(fragment)
		for s := 0; s < len(runes); s += 900 {
			out = append(out, string(runes[s:min(s+900, len(runes))]))
		}
	}
	runes := []rune(text)
	start := 0
	for i := 0; i < len(runes); {
		if !unicode.IsSpace(runes[i]) {
			i++
			continue
		}
		j, newlines := i, 0
		for j < len(runes) && unicode.IsSpace(runes[j]) {
			if runes[j] == '\n' {
				newlines++
			}
			j++
		}
		afterStop := i > 0 && strings.ContainsRune(".!?…", runes[i-1])
		if afterStop || newlines >= 2 {
			emit(string(runes[start:i]))
			start = j
		}
		i = j
	}
	emit(string(runes[start:]))
	return out
}

type token struct {
	word, norm, stem string
	capital, initial bool
}

func tokenize(sentence string) []token {
	words := Word.FindAllString(sentence, -1)
	out := make([]token, len(words))
	for i, w := range words {
		out[i] = token{word: w, norm: Normalize(w), stem: Stem(w), capital: isCapitalized(w), initial: i == 0}
	}
	return out
}

type stat struct {
	total, capMid, lower, prep, adj, verb, placeCtx, attr int
	forms                                                 map[string]int
	capForms                                              map[string]int
	order                                                 int
}

type sentenceRow struct {
	chapter int
	text    string
	tokens  []token
}

// counter keeps insertion order so ties are resolved like Python's Counter.
type counter struct {
	counts map[string]int
	order  []string
}

func newCounter() *counter { return &counter{counts: map[string]int{}} }
func (c *counter) add(k string, n int) {
	if _, ok := c.counts[k]; !ok {
		c.order = append(c.order, k)
	}
	c.counts[k] += n
}
func (c *counter) mostCommon() []string {
	keys := append([]string(nil), c.order...)
	sort.SliceStable(keys, func(i, j int) bool { return c.counts[keys[i]] > c.counts[keys[j]] })
	return keys
}

type candidate struct {
	label, kind string
	count       int
	targets     []string
}

var (
	familyRe   = regexp.MustCompile(`(?:^|[^\p{L}])(?:брат|сестр|отец|мать|сын|дочь|муж|жен[аыу])`)
	socialRe   = regexp.MustCompile(`(?:^|[^\p{L}])(?:встрет|помо[гч]|помощ|сотруднич|вместе|друг)`)
	causalRe   = regexp.MustCompile(`(?:^|[^\p{L}])(?:поэтому|из-за|привело к|привела к|вызвал[аои]?|потому что)(?:$|[^\p{L}])`)
	temporalRe = regexp.MustCompile(`(?:^|[^\p{L}])(?:после|перед|затем|позже|раньше|сначала)(?:$|[^\p{L}])`)
)

// Analyze builds the entity graph, chapter statistics and keywords for a text.
func Analyze(text string) Result {
	chapters := SplitChapters(text)
	var rows []sentenceRow
	stats := map[string]*stat{}
	get := func(stem string) *stat {
		s := stats[stem]
		if s == nil {
			s = &stat{forms: map[string]int{}, capForms: map[string]int{}, order: len(stats)}
			stats[stem] = s
		}
		return s
	}
	for ci, ch := range chapters {
		for _, sentence := range Sentences(ch.Text) {
			tokens := tokenize(sentence)
			rows = append(rows, sentenceRow{ci, sentence, tokens})
			for i, t := range tokens {
				if stopWords[t.norm] {
					continue
				}
				s := get(t.stem)
				s.total++
				if t.capital {
					s.capForms[t.word]++
					if !t.initial {
						s.capMid++
					}
				} else {
					s.lower++
					s.forms[t.norm]++
				}
				if isAdjForm(t.norm) {
					s.adj++
				}
				if isVerbForm(t.norm) {
					s.verb++
				}
				if i+1 < len(tokens) {
					next := tokens[i+1]
					if isAttributive(t.norm) && !next.capital && !prepositions[next.norm] && !IsStop(next.word) &&
						len([]rune(next.norm)) > 2 && !isVerbForm(next.norm) {
						s.attr++
					}
				}
				if i > 0 {
					prev := tokens[i-1]
					if prepositions[prev.norm] || (isAdjForm(prev.norm) && i > 1 && prepositions[tokens[i-2].norm]) {
						s.prep++
					}
					if placePrepositions[prev.norm] || locationStems[prev.stem] != "" {
						s.placeCtx++
					}
				}
			}
		}
	}

	isProper := func(stem string) bool {
		s := stats[stem]
		if s == nil || s.capMid == 0 || s.lower*10 > s.total || len([]rune(stem)) < 2 || locationStems[stem] != "" || eventStems[stem] != "" {
			return false
		}
		for f := range s.capForms {
			if stopWords[Normalize(f)] {
				return false
			}
		}
		return true
	}
	isPlaceName := func(stem string) bool {
		s := stats[stem]
		return s.placeCtx*10 >= s.total*3
	}
	isNounish := func(stem string) bool {
		s := stats[stem]
		if s == nil || stopStems[stem] || s.lower == 0 || s.adj*10 >= s.total*6 || s.attr*2 >= s.total {
			return false
		}
		if s.prep > 0 {
			return true
		}
		suffix := false
		for f := range s.forms {
			if hasNounSuffix(f) {
				suffix = true
			}
		}
		return suffix && s.verb*2 < s.total
	}
	properLabel := func(stem string) string {
		s := stats[stem]
		if len(s.capForms) > 0 {
			return Nominal(stem, s.capForms)
		}
		return Capitalize(Nominal(stem, s.forms))
	}

	// Full names: two adjacent capitalised person-like words in one sentence.
	type pair struct{ a, b string }
	fullNames := newCounter()
	pairForms := map[pair]map[[2]string]int{}
	for _, row := range rows {
		for i := 0; i+1 < len(row.tokens); i++ {
			a, b := row.tokens[i], row.tokens[i+1]
			if !a.capital || !b.capital || !isProper(a.stem) || !isProper(b.stem) || isPlaceName(a.stem) || isPlaceName(b.stem) || a.stem == b.stem {
				continue
			}
			if !onlySpacesBetween(row.text, a.word, b.word) {
				continue
			}
			p := pair{a.stem, b.stem}
			fullNames.add(p.a+" "+p.b, 1)
			if pairForms[p] == nil {
				pairForms[p] = map[[2]string]int{}
			}
			pairForms[p][[2]string{a.word, b.word}]++
		}
	}
	stemToNames := map[string]map[string]bool{}
	var candidates []candidate
	personStems := map[string]bool{}
	for _, key := range fullNames.order {
		parts := strings.SplitN(key, " ", 2)
		p := pair{parts[0], parts[1]}
		first := properLabel(p.a)
		surnames := map[string]int{}
		for f, n := range pairForms[p] {
			if f[0] == first {
				surnames[f[1]] += n
			}
		}
		if len(surnames) == 0 {
			for f, n := range pairForms[p] {
				surnames[f[1]] += n
			}
		}
		label := first + " " + Nominal(p.b, surnames)
		for _, s := range []string{p.a, p.b} {
			if stemToNames[s] == nil {
				stemToNames[s] = map[string]bool{}
			}
			stemToNames[s][label] = true
			personStems[s] = true
		}
		candidates = append(candidates, candidate{label, "person", stats[p.a].total + stats[p.b].total, []string{p.a, p.b}})
	}
	// Single proper names, ordered by frequency.
	var propers []string
	for stem := range stats {
		if isProper(stem) {
			propers = append(propers, stem)
		}
	}
	sort.Slice(propers, func(i, j int) bool {
		a, b := stats[propers[i]], stats[propers[j]]
		if a.total != b.total {
			return a.total > b.total
		}
		return a.order < b.order
	})
	singles := 0
	var placeNames []string
	for _, stem := range propers {
		if isPlaceName(stem) {
			placeNames = append(placeNames, stem)
			continue
		}
		if personStems[stem] || singles >= 25 {
			continue
		}
		singles++
		personStems[stem] = true
		candidates = append(candidates, candidate{properLabel(stem), "person", stats[stem].total, []string{stem}})
	}

	// Places: proper names in locative context and location cue words (+ following name).
	places := newCounter()
	placeTargets := map[string][]string{}
	covered := map[string]int{}
	for _, row := range rows {
		for i, t := range row.tokens {
			if cue := locationStems[t.stem]; cue != "" {
				label, targets := Capitalize(cue), []string{t.stem}
				if i+1 < len(row.tokens) {
					next := row.tokens[i+1]
					if next.capital && isProper(next.stem) && !personStems[next.stem] {
						label += " " + properLabel(next.stem)
						targets = append(targets, next.stem)
						covered[next.stem]++
					}
				}
				places.add(label, 1)
				placeTargets[label] = targets
			}
		}
	}
	for _, stem := range placeNames {
		if covered[stem] >= stats[stem].total {
			continue // Always written with its cue word, e.g. «станция Полюс».
		}
		label := properLabel(stem)
		places.add(label, stats[stem].total)
		placeTargets[label] = []string{stem}
	}
	placeStems := map[string]bool{}
	for i, label := range places.mostCommon() {
		if i >= 14 {
			break
		}
		candidates = append(candidates, candidate{label, "place", places.counts[label], placeTargets[label]})
	}
	for _, targets := range placeTargets {
		for _, s := range targets {
			placeStems[s] = true
		}
	}

	// Nouns: words seen after prepositions or with typical noun suffixes.
	nouns := newCounter()
	nounForms := map[string]map[string]int{}
	for _, row := range rows {
		for _, t := range row.tokens {
			if t.capital && isProper(t.stem) {
				continue
			}
			if len([]rune(t.norm)) <= 2 || !isNounish(t.stem) {
				continue
			}
			if eventStems[t.stem] == "" && (isVerbForm(t.norm) || isAdjForm(t.norm)) && !prepositionsFollowed(stats[t.stem]) {
				continue
			}
			nouns.add(t.stem, 1)
			if nounForms[t.stem] == nil {
				nounForms[t.stem] = map[string]int{}
			}
			nounForms[t.stem][t.norm]++
		}
	}
	nounLabel := func(stem string) string {
		if cue := locationStems[stem]; cue != "" {
			return cue
		}
		if cue := eventStems[stem]; cue != "" {
			return cue
		}
		return Nominal(stem, nounForms[stem])
	}
	ranked := nouns.mostCommon()
	for _, stem := range ranked {
		if cue := eventStems[stem]; cue != "" {
			candidates = append(candidates, candidate{Capitalize(cue), "event", nouns.counts[stem], []string{stem}})
		}
	}
	concepts := 0
	for _, stem := range ranked {
		if concepts >= 24 {
			break
		}
		if personStems[stem] || placeStems[stem] || eventStems[stem] != "" || locationStems[stem] != "" {
			continue
		}
		candidates = append(candidates, candidate{Capitalize(nounLabel(stem)), "concept", nouns.counts[stem], []string{stem}})
		concepts++
	}

	// De-duplicate and bound graph complexity while preserving person/place/event priority.
	seen := map[string]bool{}
	var unique []candidate
	for _, c := range candidates {
		k := strings.ToLower(c.label)
		if seen[k] {
			continue
		}
		seen[k] = true
		unique = append(unique, c)
		if len(unique) >= MaxEntities {
			break
		}
	}

	type matcher struct {
		kind    string
		targets []string
	}
	entities := make([]Entity, len(unique))
	matchers := make([]matcher, len(unique))
	for i, c := range unique {
		entities[i] = Entity{ID: fmt.Sprintf("e-%d", i+1), Label: c.label, Type: c.kind, Quotes: []Quote{}}
		targets := c.targets
		if c.kind == "person" {
			var aliases []string
			for _, s := range c.targets {
				if len(stemToNames[s]) <= 1 {
					aliases = append(aliases, s)
				}
			}
			if len(aliases) > 0 {
				targets = aliases
			}
		}
		matchers[i] = matcher{c.kind, targets}
	}

	type edgeRow struct {
		edge  Edge
		order int
	}
	edgeRows := map[[2]int]*edgeRow{}
	chapterOrder := make([][]string, len(chapters))
	for _, row := range rows {
		counts := map[string]int{}
		for _, t := range row.tokens {
			counts[t.stem]++
		}
		var present []int
		for i, m := range matchers {
			count := 0
			if m.kind == "person" {
				for _, s := range m.targets {
					count = max(count, counts[s])
				}
			} else {
				count = -1
				for _, s := range m.targets {
					if count < 0 || counts[s] < count {
						count = counts[s]
					}
				}
			}
			if count <= 0 {
				continue
			}
			e := &entities[i]
			e.Count += count
			ch := &chapters[row.chapter]
			if _, ok := ch.EntityCounts[e.ID]; !ok {
				chapterOrder[row.chapter] = append(chapterOrder[row.chapter], e.ID)
			}
			ch.EntityCounts[e.ID] += count
			if e.Type == "event" {
				ch.EventCount += count
			}
			if len(e.Quotes) < 5 && !hasQuote(e.Quotes, row.text) {
				e.Quotes = append(e.Quotes, Quote{row.text, ch.Title})
			}
			present = append(present, i)
		}
		// At most eight entities per sentence limits quadratic work for noisy paragraphs.
		limited := present[:min(8, len(present))]
		lower := strings.ToLower(row.text)
		persons := 0
		for _, i := range present {
			if entities[i].Type == "person" {
				persons++
			}
		}
		for x := 0; x < len(limited); x++ {
			for y := x + 1; y < len(limited); y++ {
				a, b := limited[x], limited[y]
				key := [2]int{min(a, b), max(a, b)}
				r := edgeRows[key]
				if r == nil {
					r = &edgeRow{Edge{Type: "cooccurrence", Label: "Совместное упоминание", Evidence: []string{}}, len(edgeRows)}
					edgeRows[key] = r
				}
				r.edge.Weight++
				if len(r.edge.Evidence) < 3 && !contains(r.edge.Evidence, row.text) {
					r.edge.Evidence = append(r.edge.Evidence, row.text)
				}
				ta, tb := entities[a].Type, entities[b].Type
				switch {
				case ta == "person" && tb == "person" && persons == 2 && familyRe.MatchString(lower):
					r.edge.Type, r.edge.Label = "family", "Возможное родство"
				case ta == "person" && tb == "person" && socialRe.MatchString(lower) && r.edge.Type == "cooccurrence":
					r.edge.Type, r.edge.Label = "social", "Возможное взаимодействие"
				case ta == "event" && tb == "event" && causalRe.MatchString(lower):
					r.edge.Type, r.edge.Label = "causal", "Возможная причинная связь"
				case ta == "event" && tb == "event" && temporalRe.MatchString(lower):
					r.edge.Type, r.edge.Label = "temporal", "Возможная временная связь"
				case (ta == "concept" || tb == "concept") && r.edge.Type == "cooccurrence":
					r.edge.Type, r.edge.Label = "thematic", "Тематическая близость"
				}
			}
		}
	}

	kinds := map[string]string{"person": "Персонаж или имя", "place": "Место", "concept": "Ключевое понятие", "event": "Событие или процесс"}
	valid := map[string]int{}
	kept := []Entity{}
	for i, e := range entities {
		if e.Count == 0 {
			continue
		}
		e.Description = fmt.Sprintf("%s, автоматически выделенное по словам текста. Упоминаний: %d. Проверьте контекст в цитатах.", kinds[e.Type], e.Count)
		valid[e.ID] = i
		kept = append(kept, e)
	}
	ordered := make([]*edgeRow, 0, len(edgeRows))
	keys := map[*edgeRow][2]int{}
	for k, r := range edgeRows {
		ordered = append(ordered, r)
		keys[r] = k
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].edge.Weight > ordered[j].edge.Weight })
	edges := []Edge{}
	degree := map[string]int{}
	for _, r := range ordered {
		k := keys[r]
		a, b := entities[k[0]].ID, entities[k[1]].ID
		_, okA := valid[a]
		_, okB := valid[b]
		if okA && okB {
			e := r.edge
			e.ID, e.Source, e.Target, e.Inferred = fmt.Sprintf("r-%d", len(edges)+1), a, b, true
			edges = append(edges, e)
			degree[a]++
			degree[b]++
		}
		if len(edges) >= MaxEdges {
			break
		}
	}
	byID := map[string]Entity{}
	for i := range kept {
		kept[i].Degree = degree[kept[i].ID]
		byID[kept[i].ID] = kept[i]
	}
	for ci := range chapters {
		ch := &chapters[ci]
		ids := append([]string(nil), chapterOrder[ci]...)
		sort.SliceStable(ids, func(i, j int) bool { return ch.EntityCounts[ids[i]] > ch.EntityCounts[ids[j]] })
		for _, id := range ids {
			if e, ok := byID[id]; ok && (e.Type == "concept" || e.Type == "event") && len(ch.Themes) < 5 {
				ch.Themes = append(ch.Themes, e.Label)
			}
		}
	}
	keywords := []Keyword{}
	for _, stem := range ranked {
		if len(keywords) >= 35 {
			break
		}
		if personStems[stem] {
			continue
		}
		keywords = append(keywords, Keyword{nounLabel(stem), nouns.counts[stem]})
	}
	return Result{Method: Method, Limitations: Limitations, Entities: kept, Edges: edges, Chapters: chapters,
		Keywords: keywords, WordCount: len(Word.FindAllStringIndex(text, -1))}
}

func prepositionsFollowed(s *stat) bool { return s != nil && s.prep*3 >= s.total }

func onlySpacesBetween(text, a, b string) bool {
	return strings.Contains(text, a+" "+b) || strings.Contains(text, a+"\t"+b)
}

func hasQuote(quotes []Quote, text string) bool {
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

// Answer finds source sentences sharing key words with the question.
// Queries without a lexical match return an explicit non-answer.
func Answer(question string, result Result) (string, []Quote) {
	query := map[string]bool{}
	for _, w := range Word.FindAllString(question, -1) {
		if len([]rune(w)) > 2 && !IsStop(w) {
			query[Stem(w)] = true
		}
	}
	type scored struct {
		ratio  float64
		shared int
		quote  Quote
	}
	var rows []scored
	seen := map[string]bool{}
	for _, ch := range result.Chapters {
		for _, s := range Sentences(ch.Text) {
			if seen[s] {
				continue
			}
			seen[s] = true
			stems := map[string]bool{}
			for _, w := range Word.FindAllString(s, -1) {
				stems[Stem(w)] = true
			}
			shared := 0
			for q := range query {
				if stems[q] {
					shared++
				}
			}
			if shared > 0 {
				rows = append(rows, scored{float64(shared) / float64(max(len(query), 1)), shared, Quote{s, ch.Title}})
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ratio != rows[j].ratio {
			return rows[i].ratio > rows[j].ratio
		}
		return rows[i].shared > rows[j].shared
	})
	sources := []Quote{}
	for i := 0; i < len(rows) && i < 4; i++ {
		sources = append(sources, rows[i].quote)
	}
	if len(sources) == 0 {
		return "В тексте не найдено фрагментов с ключевыми словами вопроса. Уточните имя, место или понятие. По имеющимся данным нельзя сделать достоверный вывод.", sources
	}
	var b strings.Builder
	b.WriteString("Найдены фрагменты по ключевым словам вопроса. Они помогают проверить содержание, но не являются сгенерированным объяснением мотивов или причин.\n\n")
	for i, s := range sources {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "«%s» — %s", s.Text, s.Chapter)
	}
	return b.String(), sources
}
