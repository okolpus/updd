package server

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"

	"semanticatlas/backend/internal/parsing"
)

const sampleText = `Глава 1. Письмо
Анна встретила Ивана в Томске. Иван передал Анне письмо о научной экспедиции.
Мария изучала карту города Томска. Анна и Мария обсуждали исследование реки.
Иван рассказал Марии об экспедиции и передал карту. Томск стал отправной точкой путешествия.

Глава 2. Экспедиция
Анна и Иван приехали на реку Обь. Мария ждала Ивана на берегу реки.
Исследование воды помогло экспедиции. Иван и Мария записали результаты исследования.
Анна привезла карту из Томска. Экспедиция завершилась, и Мария отправила Анне письмо.
`

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Config{DataDir: dir, SecretKey: "test-only-secret-key-which-is-long-enough", Frontend: nil})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(func() { ts.Close(); s.Close() })
	return ts
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t, ts.URL, &http.Client{Jar: jar}}
}

type response struct {
	status int
	body   []byte
	header http.Header
}

func (r response) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (c *client) do(method, path string, body any, headers ...string) response {
	c.t.Helper()
	var reader io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case *multipart.Writer:
		c.t.Fatal("use upload")
	default:
		raw, _ := json.Marshal(b)
		reader = bytes.NewReader(raw)
		contentType = "application/json"
	}
	req, _ := http.NewRequest(method, c.base+path, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return response{resp.StatusCode, data, resp.Header}
}

func (c *client) upload(name string, data []byte, fields map[string]string) response {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", c.base+"/api/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return response{resp.StatusCode, body, resp.Header}
}

func (c *client) register() (map[string]string, map[string]any) {
	c.t.Helper()
	payload := map[string]string{"login": "user_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], "name": "Читатель", "password": "good-password-42"}
	r := c.do("POST", "/api/auth/register", payload)
	if r.status != 201 {
		c.t.Fatalf("register: %d %s", r.status, r.body)
	}
	var out map[string]map[string]any
	r.json(c.t, &out)
	return payload, out["user"]
}

type doc struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	WordCount int    `json:"word_count"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Stage     string `json:"stage"`
	Depth     string `json:"depth"`
	Source    string `json:"source"`
	Subgenre  string `json:"subgenre"`
	Analysis  struct {
		Method      string `json:"method"`
		Limitations string `json:"limitations"`
		Entities    []struct {
			ID     string                  `json:"id"`
			Type   string                  `json:"type"`
			Count  int                     `json:"count"`
			Quotes []struct{ Text string } `json:"quotes"`
		} `json:"entities"`
		Edges []struct {
			Source, Target string
			Inferred       bool
			Evidence       []string
		} `json:"edges"`
		Chapters []json.RawMessage `json:"chapters"`
	} `json:"analysis"`
}

func (c *client) document(content string) doc {
	c.t.Helper()
	r := c.do("POST", "/api/documents", map[string]any{"title": "Экспедиция", "author": "Учебный пример", "year": 2026, "genre": "Художественный", "content": content})
	if r.status != 201 {
		c.t.Fatalf("create: %d %s", r.status, r.body)
	}
	var d doc
	r.json(c.t, &d)
	return c.waitReady(d.ID)
}

// waitReady polls a document until its background analysis has finished.
func (c *client) waitReady(id string) doc {
	c.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var d doc
		r := c.do("GET", "/api/documents/"+id, nil)
		r.json(c.t, &d)
		if d.Status == "ready" {
			return d
		}
		if d.Status == "error" {
			c.t.Fatalf("analysis failed: %s", r.body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatal("analysis did not finish")
	return doc{}
}

func TestAuthProfileAndRevokedCookie(t *testing.T) {
	ts := newTestServer(t)
	c := newClient(t, ts)
	if r := c.do("GET", "/api/auth/me", nil); r.status != 401 {
		t.Fatalf("anonymous me: %d", r.status)
	}
	payload, user := c.register()
	var me map[string]any
	c.do("GET", "/api/auth/me", nil).json(t, &me)
	if me["id"] != user["id"] {
		t.Fatal("me mismatch")
	}
	cookies := c.http.Jar.Cookies(mustURL(ts.URL))
	if len(cookies) != 1 {
		t.Fatal("expected session cookie")
	}
	r := c.do("PATCH", "/api/profile", map[string]any{"name": "Новое имя", "language": "ru", "private": false})
	if r.status != 200 || !strings.Contains(string(r.body), "Новое имя") {
		t.Fatalf("profile: %d %s", r.status, r.body)
	}
	if r := c.do("POST", "/api/auth/logout", nil); r.status != 200 {
		t.Fatal("logout")
	}
	old := cookies[0].Name + "=" + cookies[0].Value
	if r := (&client{t, ts.URL, http.DefaultClient}).do("GET", "/api/auth/me", nil, "Cookie", old); r.status != 401 {
		t.Fatalf("revoked cookie accepted: %d", r.status)
	}
	if r := c.do("POST", "/api/auth/login", map[string]string{"login": payload["login"], "password": "wrong-password"}); r.status != 401 {
		t.Fatal("wrong password accepted")
	}
	if r := c.do("POST", "/api/auth/login", payload); r.status != 200 {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	c.do("GET", "/api/auth/me", nil).json(t, &me)
	if me["name"] != "Новое имя" {
		t.Fatal("name not persisted")
	}
	if c.http.Jar.Cookies(mustURL(ts.URL))[0].Value == cookies[0].Value {
		t.Fatal("cookie should rotate")
	}
}

func TestDuplicateAndValidation(t *testing.T) {
	c := newClient(t, newTestServer(t))
	payload, _ := c.register()
	if r := c.do("POST", "/api/auth/register", payload); r.status != 409 {
		t.Fatalf("duplicate: %d", r.status)
	}
	if r := c.do("POST", "/api/documents", map[string]string{"title": "Пусто", "content": "   "}); r.status != 422 {
		t.Fatalf("empty content: %d", r.status)
	}
	for _, avatar := range []string{"data:image/svg+xml;base64,PHN2Zz4=", "https://example.com/a.png"} {
		if r := c.do("PATCH", "/api/profile", map[string]string{"avatar": avatar}); r.status != 422 {
			t.Fatalf("avatar %s: %d", avatar, r.status)
		}
	}
	if r := c.do("POST", "/api/auth/register", map[string]string{"login": "x", "name": "a", "password": "short"}); r.status != 422 {
		t.Fatalf("invalid register: %d", r.status)
	}
}

func TestDocumentAnalysisCRUDAndSearch(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	d := c.document(sampleText)
	a := d.Analysis
	if a.Method == "" || a.Limitations == "" || len(a.Chapters) < 2 || len(a.Entities) == 0 || len(a.Edges) == 0 || d.WordCount <= 30 {
		t.Fatalf("weak analysis: %+v", a)
	}
	ids := map[string]bool{}
	for _, e := range a.Entities {
		ids[e.ID] = true
		if !strings.Contains("person concept place event", e.Type) || e.Count < 1 {
			t.Fatalf("bad entity %+v", e)
		}
		for _, q := range e.Quotes {
			if !strings.Contains(d.Content, q.Text) {
				t.Fatalf("quote not in content: %q", q.Text)
			}
		}
	}
	for _, e := range a.Edges {
		if !ids[e.Source] || !ids[e.Target] || !e.Inferred || len(e.Evidence) == 0 {
			t.Fatalf("bad edge %+v", e)
		}
		for _, q := range e.Evidence {
			if !strings.Contains(d.Content, q) {
				t.Fatalf("evidence not in content: %q", q)
			}
		}
	}
	var again doc
	c.do("GET", "/api/documents/"+d.ID, nil).json(t, &again)
	if len(again.Analysis.Entities) != len(a.Entities) {
		t.Fatal("stored analysis differs")
	}
	var list []map[string]any
	c.do("GET", "/api/documents?q=Экспедиция&sort=title", nil).json(t, &list)
	if len(list) != 1 || list[0]["id"] != d.ID {
		t.Fatalf("search: %v", list)
	}
	c.do("GET", "/api/documents?q=неттакоготекста", nil).json(t, &list)
	if len(list) != 0 {
		t.Fatal("search should be empty")
	}
	if r := c.do("GET", "/api/documents?sort=bogus", nil); r.status != 422 {
		t.Fatal("bad sort accepted")
	}
	r := c.do("PATCH", "/api/documents/"+d.ID, map[string]any{"title": "Новое название", "author": "Другой автор", "year": nil, "genre": "Научный"})
	if r.status != 200 || !strings.Contains(string(r.body), "Новое название") {
		t.Fatalf("patch: %d %s", r.status, r.body)
	}
	if r := c.do("DELETE", "/api/documents/"+d.ID, nil); r.status != 200 {
		t.Fatal("delete")
	}
	if r := c.do("GET", "/api/documents/"+d.ID, nil); r.status != 404 {
		t.Fatal("deleted document still visible")
	}
}

func TestCrossUserAccessIsolation(t *testing.T) {
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register()
	d := c.document(sampleText)
	path := "/api/documents/" + d.ID
	var note map[string]any
	c.do("POST", path+"/notes", map[string]string{"text": "Личная заметка"}).json(t, &note)
	other := newClient(t, ts)
	other.register()
	if r := other.do("GET", "/api/documents", nil); string(bytes.TrimSpace(r.body)) != "[]" {
		t.Fatalf("other library: %s", r.body)
	}
	checks := []struct {
		method, url string
		body        any
	}{
		{"GET", path, nil}, {"PATCH", path, map[string]string{"title": "Взлом"}}, {"DELETE", path, nil},
		{"GET", path + "/notes", nil}, {"POST", path + "/notes", map[string]string{"text": "Нет"}},
		{"DELETE", path + "/notes/" + note["id"].(string), nil}, {"GET", path + "/questions", nil},
		{"POST", path + "/questions", map[string]string{"question": "Где Иван?"}},
		{"GET", path + "/export.pdf", nil}, {"GET", path + "/export.png", nil},
		{"GET", path + "/status", nil}, {"POST", path + "/analyze", nil},
	}
	for _, ch := range checks {
		if r := other.do(ch.method, ch.url, ch.body); r.status != 404 {
			t.Fatalf("%s %s: %d %s", ch.method, ch.url, r.status, r.body)
		}
	}
}

func TestNotesAndQuestionsArePersistentAndSourced(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	d := c.document(sampleText)
	path := "/api/documents/" + d.ID
	r := c.do("POST", path+"/notes", map[string]string{"text": "Проверить связь", "entity_id": d.Analysis.Entities[0].ID})
	if r.status != 201 {
		t.Fatalf("note: %d %s", r.status, r.body)
	}
	var note map[string]any
	r.json(t, &note)
	var notes []map[string]any
	c.do("GET", path+"/notes", nil).json(t, &notes)
	if len(notes) != 1 || notes[0]["id"] != note["id"] {
		t.Fatal("note not listed")
	}
	r = c.do("POST", path+"/questions", map[string]string{"question": "Что делал Иван?"})
	if r.status != 201 {
		t.Fatalf("question: %d %s", r.status, r.body)
	}
	var answer struct {
		Answer, Mode string
		Sources      []struct{ Text string }
	}
	r.json(t, &answer)
	if answer.Answer == "" || answer.Mode == "" || len(answer.Sources) == 0 {
		t.Fatalf("answer: %+v", answer)
	}
	for _, s := range answer.Sources {
		if !strings.Contains(d.Content, s.Text) {
			t.Fatalf("source not in content: %q", s.Text)
		}
	}
	var questions []map[string]any
	c.do("GET", path+"/questions", nil).json(t, &questions)
	if len(questions) != 1 {
		t.Fatal("question not persisted")
	}
	if r := c.do("POST", path+"/notes", map[string]string{"text": "   "}); r.status != 422 {
		t.Fatal("blank note accepted")
	}
	if r := c.do("POST", path+"/notes", map[string]string{"text": "x", "entity_id": "nonexistent-entity"}); r.status != 422 {
		t.Fatal("unknown entity accepted")
	}
	if r := c.do("DELETE", path+"/notes/"+note["id"].(string), nil); r.status != 200 {
		t.Fatal("delete note")
	}
	if r := c.do("GET", path+"/notes", nil); string(bytes.TrimSpace(r.body)) != "[]" {
		t.Fatal("note not deleted")
	}
}

func epub(files map[string]string) []byte {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := z.Create(name)
		w.Write([]byte(content))
	}
	z.Close()
	return buf.Bytes()
}

func TestUploadTXTAndEPUB(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	r := c.upload("story.txt", []byte(sampleText), map[string]string{"title": "Файл", "author": "Автор", "genre": "Учебный"})
	if r.status != 201 || !strings.Contains(string(r.body), "Анна") {
		t.Fatalf("txt: %d %s", r.status, r.body)
	}
	data := epub(map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"OEBPS/content.opf":      `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata/><manifest><item id="one" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="one"/></spine></package>`,
		"OEBPS/chapter.xhtml":    `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Глава 1</h1><p>` + sampleText + `</p></body></html>`,
	})
	r = c.upload("story.epub", data, map[string]string{"title": "EPUB"})
	if r.status != 201 || !strings.Contains(string(r.body), "Иван") {
		t.Fatalf("epub: %d %s", r.status, r.body)
	}
	var created doc
	r.json(t, &created)
	if ready := c.waitReady(created.ID); ready.Source != "epub" || len(ready.Analysis.Entities) == 0 {
		t.Fatalf("epub source/analysis: %+v", ready)
	}
	// The title defaults to the file name, as with drag-and-drop uploads.
	r = c.upload("Моя книга.txt", []byte(sampleText), nil)
	var d doc
	r.json(t, &d)
	if r.status != 201 || d.Title != "Моя книга" {
		t.Fatalf("default title: %d %q", r.status, d.Title)
	}
	cp1251 := []byte{0xC0, 0xED, 0xED, 0xE0, ' ', 0xE2, 0xF1, 0xF2, 0xF0, 0xE5, 0xF2, 0xE8, 0xEB, 0xE0, ' ', 0xC8, 0xE2, 0xE0, 0xED, 0xE0, '.'}
	r = c.upload("win.txt", cp1251, nil)
	if r.status != 201 || !strings.Contains(string(r.body), "Анна встретила Ивана") {
		t.Fatalf("cp1251: %d %s", r.status, r.body)
	}
}

func makePDF(text string) []byte {
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.AddPage()
	if text != "" {
		pdf.SetFont("Helvetica", "", 12)
		pdf.Text(50, 100, text)
	}
	var buf bytes.Buffer
	pdf.Output(&buf)
	return buf.Bytes()
}

func TestPDFUploadAndExports(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	d := c.document(sampleText)
	path := "/api/documents/" + d.ID
	r := c.do("GET", path+"/export.pdf", nil)
	if r.status != 200 || !bytes.HasPrefix(r.body, []byte("%PDF")) {
		t.Fatalf("pdf export: %d", r.status)
	}
	text, err := parsing.ParseFile("report.pdf", r.body)
	if err != nil || !strings.Contains(text, "Экспедиция") {
		t.Fatalf("pdf export text not extractable: %v %q", err, text[:min(len(text), 300)])
	}
	r = c.do("GET", path+"/export.png", nil)
	if r.status != 200 {
		t.Fatal("png export")
	}
	img, err := png.Decode(bytes.NewReader(r.body))
	if err != nil || img.Bounds().Dx() < 600 || img.Bounds().Dy() < 400 {
		t.Fatalf("png: %v", err)
	}
	r = c.upload("story.pdf", makePDF("Anna met Ivan in Tomsk. Ivan gave Anna a letter about science."), map[string]string{"title": "PDF"})
	if r.status != 201 || !strings.Contains(string(r.body), "Ivan") {
		t.Fatalf("pdf upload: %d %s", r.status, r.body)
	}
}

func TestRejectedFilesAndSafeProfilePhoto(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	for name, data := range map[string][]byte{"bad.exe": []byte("executable"), "bad.pdf": []byte("not a PDF"), "bad.epub": []byte("not a zip"), "empty.txt": []byte("   ")} {
		if r := c.upload(name, data, map[string]string{"title": "Ошибка"}); r.status != 422 {
			t.Fatalf("%s: %d %s", name, r.status, r.body)
		}
	}
	if r := c.upload("huge.txt", bytes.Repeat([]byte("x"), 13*1024*1024), map[string]string{"title": "Большой"}); r.status != 413 {
		t.Fatalf("huge: %d %s", r.status, r.body)
	}
	if r := c.upload("scan.pdf", makePDF(""), map[string]string{"title": "Пустой PDF"}); r.status != 422 || !strings.Contains(string(r.body), "OCR") {
		t.Fatalf("scan: %d %s", r.status, r.body)
	}
}

func TestDemoAndHealth(t *testing.T) {
	c := newClient(t, newTestServer(t))
	if r := c.do("GET", "/api/health", nil); !strings.Contains(string(r.body), `"ok"`) || !strings.Contains(string(r.body), `"server":"go"`) {
		t.Fatal("health")
	}
	r := c.do("POST", "/api/auth/demo", map[string]any{})
	if r.status != 200 {
		t.Fatalf("demo: %d %s", r.status, r.body)
	}
	var out struct {
		DocumentID string `json:"document_id"`
	}
	r.json(t, &out)
	var d doc
	c.do("GET", "/api/documents/"+out.DocumentID, nil).json(t, &d)
	if len(d.Analysis.Chapters) < 5 || len(d.Analysis.Entities) == 0 || len(d.Analysis.Edges) == 0 {
		t.Fatal("demo analysis")
	}
	persons := 0
	for _, e := range d.Analysis.Entities {
		if e.Type == "person" {
			persons++
		}
	}
	if persons != 5 {
		t.Fatalf("demo should have exactly 5 characters, got %d", persons)
	}
}

func TestOriginAndProfilePhoto(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	if r := c.do("POST", "/api/auth/logout", nil, "Origin", "https://unrelated.example"); r.status != 403 {
		t.Fatalf("foreign origin: %d", r.status)
	}
	if r := c.do("GET", "/api/auth/me", nil); r.status != 200 {
		t.Fatal("session lost")
	}
	img := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.RGBA{0, 0, 255, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	avatar := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	if r := c.do("PATCH", "/api/profile", map[string]string{"avatar": avatar}); r.status != 200 {
		t.Fatalf("avatar: %d %s", r.status, r.body)
	}
	var me map[string]any
	c.do("GET", "/api/auth/me", nil).json(t, &me)
	if me["avatar"] != avatar {
		t.Fatal("avatar not saved")
	}
}

func TestEPUBDecompressionLimit(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, _ := z.Create("huge.xhtml")
	w.Write(bytes.Repeat([]byte("x"), 13*1024*1024))
	z.Close()
	if r := c.upload("bomb.epub", buf.Bytes(), map[string]string{"title": "Сжатый"}); r.status != 422 {
		t.Fatalf("bomb: %d %s", r.status, r.body)
	}
}

func TestUnknownQuestionAndFragmentValidation(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	d := c.document(sampleText)
	path := "/api/documents/" + d.ID
	r := c.do("POST", path+"/questions", map[string]string{"question": "Квазиплазменная нейтронизация?"})
	if r.status != 201 || !strings.Contains(string(r.body), `"sources":[]`) {
		t.Fatalf("unknown question: %d %s", r.status, r.body)
	}
	if r := c.do("POST", path+"/notes", map[string]string{"text": "Заметка", "fragment": "Такого фрагмента тут нет"}); r.status != 422 {
		t.Fatal("foreign fragment accepted")
	}
	if r := c.do("PATCH", path, map[string]any{"title": nil}); r.status != 422 {
		t.Fatal("null title accepted")
	}
}

func TestEPUBParentPathsPreserveAllChapters(t *testing.T) {
	c := newClient(t, newTestServer(t))
	c.register()
	files := map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="OEBPS/pkg/content.opf"/></rootfiles></container>`,
		"OEBPS/pkg/content.opf":  `<package><manifest><item id="one" href="../Text/ch1.xhtml"/><item id="two" href="../Text/ch2.xhtml"/></manifest><spine><itemref idref="one"/><itemref idref="two"/></spine></package>`,
		"OEBPS/Text/ch1.xhtml":   `<html><body><h1>Глава 1</h1><p>Анна встретила Ивана на берегу реки.</p></body></html>`,
		"OEBPS/Text/ch2.xhtml":   `<html><body><h1>Глава 2</h1><p>Мария передала Анне письмо из Томска.</p></body></html>`,
	}
	r := c.upload("valid.epub", epub(files), map[string]string{"title": "Главы"})
	var d doc
	r.json(t, &d)
	d = c.waitReady(d.ID)
	if r.status != 201 || !strings.Contains(d.Content, "Анна встретила Ивана") || !strings.Contains(d.Content, "Мария передала Анне") || len(d.Analysis.Chapters) != 2 {
		t.Fatalf("epub chapters: %d %s", r.status, r.body)
	}
	delete(files, "OEBPS/Text/ch2.xhtml")
	if r := c.upload("broken.epub", epub(files), map[string]string{"title": "Пропавшая глава"}); r.status != 422 {
		t.Fatalf("broken epub: %d", r.status)
	}
}

func TestUnknownAPIRouteAndSecurityHeaders(t *testing.T) {
	c := newClient(t, newTestServer(t))
	r := c.do("GET", "/api/nothing-here", nil)
	if r.status != 404 || r.header.Get("X-Frame-Options") != "DENY" || r.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unknown route: %d %v", r.status, r.header)
	}
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func TestBackgroundAnalysisProgressAndDepth(t *testing.T) {
	dir := t.TempDir()
	srv, err := New(Config{DataDir: dir, SecretKey: "test-only-secret-key-which-is-long-enough"})
	if err != nil {
		t.Fatal(err)
	}
	srv.stageDelay = 80 * time.Millisecond
	ts := httptest.NewServer(srv)
	defer func() { ts.Close(); srv.Close() }()
	c := newClient(t, ts)
	c.register()
	r := c.do("POST", "/api/documents", map[string]any{"title": "Фон", "content": sampleText, "subgenre": "Повесть", "depth": "basic"})
	var d doc
	r.json(t, &d)
	if r.status != 201 || d.Status != "processing" || d.Stage != "extract" || d.Subgenre != "Повесть" || d.Depth != "basic" || d.Source != "text" {
		t.Fatalf("created: %d %s", r.status, r.body)
	}
	path := "/api/documents/" + d.ID
	// While the analysis runs, dependent operations are refused.
	if r := c.do("POST", path+"/questions", map[string]string{"question": "Где Иван?"}); r.status != 409 {
		t.Fatalf("question during analysis: %d", r.status)
	}
	if r := c.do("GET", path+"/export.pdf", nil); r.status != 409 {
		t.Fatalf("export during analysis: %d", r.status)
	}
	stages := map[string]bool{}
	lastProgress := 0
	for i := 0; i < 400; i++ {
		var cur doc
		c.do("GET", path+"/status", nil).json(t, &cur)
		if cur.Progress < lastProgress {
			t.Fatalf("progress went back: %d -> %d", lastProgress, cur.Progress)
		}
		lastProgress = cur.Progress
		stages[cur.Stage] = true
		if cur.Status == "ready" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, st := range []string{"entities", "relations", "graph", "report", "done"} {
		if !stages[st] {
			t.Fatalf("stage %s not observed: %v", st, stages)
		}
	}
	basic := c.waitReady(d.ID)
	if basic.Depth != "basic" || len(basic.Analysis.Entities) > 30 {
		t.Fatalf("basic depth: %s %d", basic.Depth, len(basic.Analysis.Entities))
	}
	srv.stageDelay = 0
	if r := c.do("POST", path+"/analyze", map[string]string{"depth": "full"}); r.status != 202 {
		t.Fatalf("reanalyze: %d %s", r.status, r.body)
	}
	if full := c.waitReady(d.ID); full.Depth != "full" || len(full.Analysis.Entities) < len(basic.Analysis.Entities) {
		t.Fatalf("full depth: %s %d", full.Depth, len(full.Analysis.Entities))
	}
	var list []map[string]any
	c.do("GET", "/api/documents", nil).json(t, &list)
	if list[0]["status"] != "ready" || list[0]["note"] == "" {
		t.Fatalf("summary: %v", list[0])
	}
}

func TestOldDatabaseIsMigrated(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "atlas.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Schema of the first (Python) release, without background-analysis columns.
	for _, st := range []string{
		`CREATE TABLE users (id VARCHAR(36) PRIMARY KEY, login VARCHAR(80) UNIQUE, name VARCHAR(100), password_hash TEXT, language VARCHAR(5), private BOOLEAN, avatar TEXT)`,
		`CREATE TABLE documents (id VARCHAR(36) PRIMARY KEY, user_id VARCHAR(36), title VARCHAR(250), author VARCHAR(180), year INTEGER, genre VARCHAR(80), created_at VARCHAR(40), content TEXT, analysis JSON)`,
		`INSERT INTO users VALUES ('u1', 'old_user', 'Старый', '` + hashPassword("old-password-1") + `', 'ru', 1, NULL)`,
		`INSERT INTO documents VALUES ('d1', 'u1', 'Старый текст', '', NULL, 'Художественный', '2026-01-01T00:00:00+00:00', 'Анна встретила Ивана.', '{"entities":[],"edges":[],"chapters":[],"keywords":[],"word_count":3}')`,
	} {
		if _, err := db.Exec(st); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	srv, err := New(Config{DataDir: dir, SecretKey: "test-only-secret-key-which-is-long-enough"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer func() { ts.Close(); srv.Close() }()
	c := newClient(t, ts)
	if r := c.do("POST", "/api/auth/login", map[string]string{"login": "old_user", "password": "old-password-1"}); r.status != 200 {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	var list []map[string]any
	c.do("GET", "/api/documents", nil).json(t, &list)
	if len(list) != 1 || list[0]["status"] != "ready" || list[0]["source"] != "text" {
		t.Fatalf("migrated list: %v", list)
	}
}
