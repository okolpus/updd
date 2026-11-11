package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"semanticatlas/backend/internal/ai"
	"semanticatlas/backend/internal/analysis"
	"semanticatlas/backend/internal/export"
	"semanticatlas/backend/internal/parsing"
	"semanticatlas/backend/internal/store"
)

type summary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Year        *int   `json:"year"`
	Genre       string `json:"genre"`
	CreatedAt   string `json:"created_at"`
	WordCount   int    `json:"word_count"`
	EntityCount int    `json:"entity_count"`
	EdgeCount   int    `json:"edge_count"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
	Stage       string `json:"stage"`
	Note        string `json:"note"`
	Error       string `json:"error"`
	Depth       string `json:"depth"`
	Source      string `json:"source"`
	Subgenre    string `json:"subgenre"`
	AIModel     string `json:"ai_model"`
}

type fullDocument struct {
	summary
	Content  string          `json:"content"`
	Analysis json.RawMessage `json:"analysis"`
}

// counts decodes only what a library summary needs from the stored analysis.
type counts struct {
	WordCount int               `json:"word_count"`
	Entities  []json.RawMessage `json:"entities"`
	Edges     []json.RawMessage `json:"edges"`
}

func summarize(d *store.Document) summary {
	var c counts
	json.Unmarshal([]byte(d.Analysis), &c)
	status := d.Status
	if status == "" {
		status = "ready"
	}
	return summary{d.ID, d.Title, d.Author, d.Year, d.Genre, d.CreatedAt, c.WordCount, len(c.Entities), len(c.Edges),
		status, d.Progress, d.Stage, d.Note, d.Error, d.Depth, d.Source, d.Subgenre, d.AIModel}
}

func full(d *store.Document) fullDocument {
	return fullDocument{summarize(d), d.Content, json.RawMessage(d.Analysis)}
}

func decodeAnalysis(d *store.Document) (analysis.Result, error) {
	var a analysis.Result
	err := json.Unmarshal([]byte(d.Analysis), &a)
	return a, err
}

// owned returns the document only when it belongs to the user; foreign ids are 404.
func (s *Server) owned(w http.ResponseWriter, r *http.Request, user *store.User) *store.Document {
	doc, err := s.store.Document(r.Context(), r.PathValue("id"))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		serverError(w, err)
		return nil
	}
	if doc == nil || doc.UserID != user.ID {
		fail(w, http.StatusNotFound, "Текст не найден.")
		return nil
	}
	return doc
}

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request, user *store.User) {
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "created"
	}
	if sortBy != "created" && sortBy != "title" && sortBy != "author" {
		fail(w, http.StatusUnprocessableEntity, "Сортировка: created, title или author.")
		return
	}
	docs, err := s.store.Documents(r.Context(), user.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := []summary{}
	for _, d := range docs {
		if query == "" || strings.Contains(strings.ToLower(d.Title+" "+d.Author+" "+d.Genre), query) {
			out = append(out, summarize(d))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		switch sortBy {
		case "title":
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		case "author":
			return strings.ToLower(out[i].Author) < strings.ToLower(out[j].Author)
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	writeJSON(w, http.StatusOK, out)
}

type docInput struct {
	title, author, genre, content string
	subgenre, depth, source       string
	ai                            bool
	year                          *int
}

func validateMeta(in *docInput) string {
	in.title = strings.TrimSpace(in.title)
	in.author = strings.TrimSpace(in.author)
	in.genre = strings.TrimSpace(in.genre)
	in.subgenre = strings.TrimSpace(in.subgenre)
	if in.genre == "" {
		in.genre = "Художественный"
	}
	switch {
	case in.title == "":
		return "Укажите название текста."
	case runeLen(in.title) > 250:
		return "Название не длиннее 250 символов."
	case runeLen(in.author) > 180:
		return "Автор не длиннее 180 символов."
	case runeLen(in.genre) > 80:
		return "Тип текста не длиннее 80 символов."
	case runeLen(in.subgenre) > 80:
		return "Жанр не длиннее 80 символов."
	case in.year != nil && (*in.year < 1 || *in.year > 2200):
		return "Год должен быть от 1 до 2200."
	}
	return ""
}

// saveDocument analyses synchronously; used for the demo story.
func (s *Server) saveDocument(ctx context.Context, user *store.User, title, author string, year *int, genre, content string) (*store.Document, error) {
	_, raw, err := analyzeJSON(content)
	if err != nil {
		return nil, err
	}
	doc := &store.Document{ID: uid(), UserID: user.ID, Title: title, Author: author, Year: year, Genre: genre,
		CreatedAt: store.Now(), Content: content, Analysis: raw, Status: "ready", Progress: 100, Stage: "done", Depth: "full", Source: "text"}
	return doc, s.store.CreateDocument(ctx, doc)
}

func (s *Server) finishCreate(w http.ResponseWriter, r *http.Request, user *store.User, in docInput) {
	if msg := validateMeta(&in); msg != "" {
		fail(w, http.StatusUnprocessableEntity, msg)
		return
	}
	content, err := parsing.ValidateContent(in.content)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, parsing.Describe(err))
		return
	}
	if in.source == "" {
		in.source = "text"
	}
	aiModel, msg := s.aiModel(r, in.ai)
	if msg != "" {
		fail(w, http.StatusUnprocessableEntity, msg)
		return
	}
	// The text is stored right away; entities and links are computed in the background.
	doc := &store.Document{ID: uid(), UserID: user.ID, Title: in.title, Author: in.author, Year: in.year, Genre: in.genre,
		CreatedAt: store.Now(), Content: content, Analysis: placeholderAnalysis(), Status: "processing", Progress: stageProgress["extract"],
		Stage: "extract", Depth: normalizeDepth(in.depth), Source: in.source, Subgenre: in.subgenre, AIModel: aiModel}
	if err := s.store.CreateDocument(r.Context(), doc); err != nil {
		serverError(w, err)
		return
	}
	s.enqueue(doc.ID)
	writeJSON(w, http.StatusCreated, full(doc))
}

func intField(body map[string]json.RawMessage, key string) (*int, bool) {
	raw, ok := body[key]
	if !ok || string(raw) == "null" {
		return nil, true
	}
	var v int
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	return &v, true
}

func (s *Server) createDocument(w http.ResponseWriter, r *http.Request, user *store.User) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	var in docInput
	var okTitle, okAuthor, okGenre, okContent, okYear bool
	in.title, _, okTitle = field(body, "title")
	in.author, _, okAuthor = field(body, "author")
	in.genre, _, okGenre = field(body, "genre")
	in.content, _, okContent = field(body, "content")
	var okSub, okDepth bool
	in.subgenre, _, okSub = field(body, "subgenre")
	in.depth, _, okDepth = field(body, "depth")
	okGenre = okGenre && okSub && okDepth
	if raw, ok := body["ai"]; ok && string(raw) != "null" {
		if json.Unmarshal(raw, &in.ai) != nil {
			okGenre = false
		}
	}
	in.year, okYear = intField(body, "year")
	if !okTitle || !okAuthor || !okGenre || !okContent || !okYear {
		fail(w, http.StatusUnprocessableEntity, "Проверьте поля формы.")
		return
	}
	if runeLen(in.content) > parsing.MaxChars {
		fail(w, http.StatusUnprocessableEntity, "Текст слишком большой. Максимум 12 миллионов символов.")
		return
	}
	s.finishCreate(w, r, user, in)
}

func (s *Server) uploadDocument(w http.ResponseWriter, r *http.Request, user *store.User) {
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "Размер запроса больше 16 МБ.")
			return
		}
		fail(w, http.StatusUnprocessableEntity, "Не удалось прочитать форму загрузки.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "Выберите файл для загрузки.")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, parsing.MaxBytes+1))
	if err != nil {
		serverError(w, err)
		return
	}
	if len(data) > parsing.MaxBytes {
		fail(w, http.StatusRequestEntityTooLarge, "Файл больше 12 МБ.")
		return
	}
	content, err := parsing.ParseFile(header.Filename, data)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, parsing.Describe(err))
		return
	}
	in := docInput{title: r.FormValue("title"), author: r.FormValue("author"), genre: r.FormValue("genre"), content: content,
		subgenre: r.FormValue("subgenre"), depth: r.FormValue("depth"), ai: truthy(r.FormValue("ai")), source: strings.TrimPrefix(strings.ToLower(path.Ext(header.Filename)), ".")}
	if strings.TrimSpace(in.title) == "" {
		name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
		in.title = strings.TrimSuffix(name, path.Ext(name))
		if strings.TrimSpace(in.title) == "" {
			in.title = "Текст"
		}
	}
	if raw := strings.TrimSpace(r.FormValue("year")); raw != "" {
		year, err := strconv.Atoi(raw)
		if err != nil {
			fail(w, http.StatusUnprocessableEntity, "Год должен быть числом.")
			return
		}
		in.year = &year
	}
	s.finishCreate(w, r, user, in)
}

func (s *Server) getDocument(w http.ResponseWriter, r *http.Request, user *store.User) {
	if doc := s.owned(w, r, user); doc != nil {
		writeJSON(w, http.StatusOK, full(doc))
	}
}

func (s *Server) patchDocument(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.owned(w, r, user)
	if doc == nil {
		return
	}
	body, ok := decode(w, r)
	if !ok {
		return
	}
	for _, key := range []string{"title", "author", "genre", "subgenre"} {
		raw, present := body[key]
		if !present {
			continue
		}
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			fail(w, http.StatusUnprocessableEntity, "Название и строковые поля не могут быть пустыми null.")
			return
		}
		switch key {
		case "title":
			doc.Title = value
		case "author":
			doc.Author = value
		case "genre":
			doc.Genre = value
		case "subgenre":
			doc.Subgenre = value
		}
	}
	if _, present := body["year"]; present {
		year, ok := intField(body, "year")
		if !ok {
			fail(w, http.StatusUnprocessableEntity, "Год должен быть числом.")
			return
		}
		doc.Year = year
	}
	in := docInput{title: doc.Title, author: doc.Author, genre: doc.Genre, subgenre: doc.Subgenre, year: doc.Year}
	if msg := validateMeta(&in); msg != "" {
		fail(w, http.StatusUnprocessableEntity, msg)
		return
	}
	doc.Title, doc.Author, doc.Genre, doc.Subgenre = in.title, in.author, in.genre, in.subgenre
	if err := s.store.UpdateDocument(r.Context(), doc); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, full(doc))
}

// readyDoc is owned() plus a check that the analysis has finished.
func (s *Server) readyDoc(w http.ResponseWriter, r *http.Request, user *store.User) *store.Document {
	doc := s.owned(w, r, user)
	if doc != nil && !ready(doc) {
		fail(w, http.StatusConflict, "Анализ текста ещё не завершён.")
		return nil
	}
	return doc
}

// documentStatus returns only the summary, for polling analysis progress.
func (s *Server) documentStatus(w http.ResponseWriter, r *http.Request, user *store.User) {
	if doc := s.owned(w, r, user); doc != nil {
		writeJSON(w, http.StatusOK, summarize(doc))
	}
}

func (s *Server) reanalyze(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.owned(w, r, user)
	if doc == nil {
		return
	}
	if doc.Status == "processing" {
		fail(w, http.StatusConflict, "Анализ уже выполняется.")
		return
	}
	depth, useAI := doc.Depth, doc.AIModel != ""
	if r.ContentLength > 0 {
		body, ok := decode(w, r)
		if !ok {
			return
		}
		if value, present, ok := field(body, "depth"); present && ok && value != "" {
			depth = value
		}
		if raw, ok := body["ai"]; ok {
			json.Unmarshal(raw, &useAI)
		}
	}
	depth = normalizeDepth(depth)
	aiModel, msg := s.aiModel(r, useAI)
	if msg != "" {
		fail(w, http.StatusUnprocessableEntity, msg)
		return
	}
	if err := s.store.StartAnalysis(r.Context(), doc.ID, depth, aiModel); err != nil {
		serverError(w, err)
		return
	}
	s.enqueue(doc.ID)
	doc.Status, doc.Progress, doc.Stage, doc.Note, doc.Error, doc.Depth, doc.AIModel = "processing", stageProgress["extract"], "extract", "", "", depth, aiModel
	writeJSON(w, http.StatusAccepted, summarize(doc))
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.owned(w, r, user)
	if doc == nil {
		return
	}
	if err := s.store.DeleteDocument(r.Context(), doc.ID); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.owned(w, r, user)
	if doc == nil {
		return
	}
	notes, err := s.store.Notes(r.Context(), doc.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.readyDoc(w, r, user)
	if doc == nil {
		return
	}
	body, ok := decode(w, r)
	if !ok {
		return
	}
	text, _, ok1 := field(body, "text")
	entityID, hasEntity, ok2 := field(body, "entity_id")
	fragment, hasFragment, ok3 := field(body, "fragment")
	if !ok1 || !ok2 || !ok3 {
		fail(w, http.StatusUnprocessableEntity, "Проверьте поля формы.")
		return
	}
	if strings.TrimSpace(text) == "" || runeLen(text) > 10000 {
		fail(w, http.StatusUnprocessableEntity, "Напишите заметку (до 10 000 символов).")
		return
	}
	note := &store.Note{ID: uid(), DocumentID: doc.ID, Text: text, CreatedAt: store.Now()}
	if hasEntity && entityID != "" {
		a, err := decodeAnalysis(doc)
		if err != nil {
			serverError(w, err)
			return
		}
		found := false
		for _, e := range a.Entities {
			found = found || e.ID == entityID
		}
		if !found {
			fail(w, http.StatusUnprocessableEntity, "Сущность не найдена в этом тексте.")
			return
		}
		note.EntityID = &entityID
	}
	if hasFragment && fragment != "" {
		if runeLen(fragment) > 5000 {
			fail(w, http.StatusUnprocessableEntity, "Фрагмент не длиннее 5000 символов.")
			return
		}
		if !strings.Contains(doc.Content, fragment) {
			fail(w, http.StatusUnprocessableEntity, "Выбранного фрагмента нет в тексте.")
			return
		}
		note.Fragment = &fragment
	}
	if err := s.store.CreateNote(r.Context(), note); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, note)
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.owned(w, r, user)
	if doc == nil {
		return
	}
	note, err := s.store.Note(r.Context(), r.PathValue("note"))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		serverError(w, err)
		return
	}
	if note == nil || note.DocumentID != doc.ID {
		fail(w, http.StatusNotFound, "Заметка не найдена.")
		return
	}
	if err := s.store.DeleteNote(r.Context(), note.ID); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type questionOut struct {
	*store.Question
	Sources json.RawMessage `json:"sources"`
}

func (s *Server) listQuestions(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.owned(w, r, user)
	if doc == nil {
		return
	}
	questions, err := s.store.Questions(r.Context(), doc.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]questionOut, len(questions))
	for i, q := range questions {
		out[i] = questionOut{q, json.RawMessage(q.Sources)}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createQuestion(w http.ResponseWriter, r *http.Request, user *store.User) {
	doc := s.readyDoc(w, r, user)
	if doc == nil {
		return
	}
	body, ok := decode(w, r)
	if !ok {
		return
	}
	question, _, ok := field(body, "question")
	question = strings.TrimSpace(question)
	if !ok || runeLen(question) < 2 || runeLen(question) > 1000 {
		fail(w, http.StatusUnprocessableEntity, "Напишите вопрос (от 2 до 1000 символов).")
		return
	}
	a, err := decodeAnalysis(doc)
	if err != nil {
		serverError(w, err)
		return
	}
	answer, sources := analysis.Answer(question, a)
	mode := "extractive"
	if st := s.cfg.AI.Status(r.Context()); st.Available {
		generated, quotes, err := ai.Answer(r.Context(), s.cfg.AI, question, a)
		if err == nil && generated != "" {
			answer, sources, mode = generated, quotes, "ai:"+st.Model
		} else if err != nil {
			answer = "Qwen не ответила, поэтому показаны найденные фрагменты.\n\n" + answer
		}
	}
	raw, _ := json.Marshal(sources)
	q := &store.Question{ID: uid(), DocumentID: doc.ID, Question: question, Answer: answer, Sources: string(raw), Mode: mode, CreatedAt: store.Now()}
	if err := s.store.CreateQuestion(r.Context(), q); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, questionOut{q, raw})
}

func (s *Server) report(w http.ResponseWriter, r *http.Request, user *store.User) (export.Report, bool) {
	doc := s.readyDoc(w, r, user)
	if doc == nil {
		return export.Report{}, false
	}
	a, err := decodeAnalysis(doc)
	if err != nil {
		serverError(w, err)
		return export.Report{}, false
	}
	return export.Report{Title: doc.Title, Author: doc.Author, Analysis: a}, true
}

func (s *Server) exportPDF(w http.ResponseWriter, r *http.Request, user *store.User) {
	rep, ok := s.report(w, r, user)
	if !ok {
		return
	}
	data, err := export.PDF(rep)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="semantic-atlas-report.pdf"`)
	w.Write(data)
}

func (s *Server) exportPNG(w http.ResponseWriter, r *http.Request, user *store.User) {
	rep, ok := s.report(w, r, user)
	if !ok {
		return
	}
	data, err := export.PNG(rep)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `attachment; filename="semantic-atlas-graph.png"`)
	w.Write(data)
}

func truthy(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

// aiModel returns the model to use when refinement was requested, or a message why it cannot be used.
func (s *Server) aiModel(r *http.Request, requested bool) (string, string) {
	if !requested {
		return "", ""
	}
	st := s.cfg.AI.Status(r.Context())
	if !st.Available {
		return "", "Qwen сейчас недоступна. " + st.Reason
	}
	return st.Model, ""
}
