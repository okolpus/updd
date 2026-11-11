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
	return summary{d.ID, d.Title, d.Author, d.Year, d.Genre, d.CreatedAt, c.WordCount, len(c.Entities), len(c.Edges), "ready"}
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
	year                          *int
}

func validateMeta(in *docInput) string {
	in.title = strings.TrimSpace(in.title)
	in.author = strings.TrimSpace(in.author)
	in.genre = strings.TrimSpace(in.genre)
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
	case in.year != nil && (*in.year < 1 || *in.year > 2200):
		return "Год должен быть от 1 до 2200."
	}
	return ""
}

func (s *Server) saveDocument(ctx context.Context, user *store.User, title, author string, year *int, genre, content string) (*store.Document, error) {
	_, raw, err := analyzeJSON(content)
	if err != nil {
		return nil, err
	}
	doc := &store.Document{ID: uid(), UserID: user.ID, Title: title, Author: author, Year: year, Genre: genre,
		CreatedAt: store.Now(), Content: content, Analysis: raw}
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
	doc, err := s.saveDocument(r.Context(), user, in.title, in.author, in.year, in.genre, content)
	if err != nil {
		serverError(w, err)
		return
	}
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
	in := docInput{title: r.FormValue("title"), author: r.FormValue("author"), genre: r.FormValue("genre"), content: content}
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
	for _, key := range []string{"title", "author", "genre"} {
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
	in := docInput{title: doc.Title, author: doc.Author, genre: doc.Genre, year: doc.Year}
	if msg := validateMeta(&in); msg != "" {
		fail(w, http.StatusUnprocessableEntity, msg)
		return
	}
	doc.Title, doc.Author, doc.Genre = in.title, in.author, in.genre
	if err := s.store.UpdateDocument(r.Context(), doc); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, full(doc))
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
	doc := s.owned(w, r, user)
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
	doc := s.owned(w, r, user)
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
	raw, _ := json.Marshal(sources)
	q := &store.Question{ID: uid(), DocumentID: doc.ID, Question: question, Answer: answer, Sources: string(raw), Mode: "extractive", CreatedAt: store.Now()}
	if err := s.store.CreateQuestion(r.Context(), q); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, questionOut{q, raw})
}

func (s *Server) report(w http.ResponseWriter, r *http.Request, user *store.User) (export.Report, bool) {
	doc := s.owned(w, r, user)
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
