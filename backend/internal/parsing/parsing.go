// Package parsing extracts plain text from TXT, PDF and EPUB files.
package parsing

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"
)

const (
	MaxBytes = 12 * 1024 * 1024
	MaxWords = 500_000
	MaxChars = 12_000_000
)

// UserError is a message that is safe to show to the user.
type UserError struct{ Message string }

func (e *UserError) Error() string { return e.Message }

func userErr(msg string) error { return &UserError{msg} }

var wordRe = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// ValidateContent normalises line endings and enforces size limits.
func ValidateContent(text string) (string, error) {
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxChars {
		return "", userErr("Текст слишком большой. Максимум 12 миллионов символов.")
	}
	words := len(wordRe.FindAllStringIndex(text, MaxWords+1))
	if words > MaxWords {
		return "", userErr("В тексте больше 500 000 слов. Разделите его на несколько частей.")
	}
	if words < 3 {
		return "", userErr("Нужно хотя бы три слова для анализа.")
	}
	return text, nil
}

// ParseFile detects the format by extension and returns validated text.
func ParseFile(filename string, data []byte) (string, error) {
	if len(data) > MaxBytes {
		return "", userErr("Файл больше 12 МБ.")
	}
	var (
		text string
		err  error
	)
	switch strings.ToLower(path.Ext(filename)) {
	case ".txt":
		text, err = decodeTXT(data)
	case ".pdf":
		text, err = parsePDF(data)
	case ".epub":
		text, err = parseEPUB(data)
	default:
		return "", userErr("Поддерживаются только TXT, PDF и EPUB.")
	}
	if err != nil {
		return "", err
	}
	return ValidateContent(text)
}

func decodeTXT(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if utf8.Valid(data) {
		return string(data), nil
	}
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
	if err != nil {
		return "", userErr("Не удалось прочитать TXT. Сохраните его в UTF-8.")
	}
	return string(decoded), nil
}

func parsePDF(data []byte) (text string, err error) {
	defer func() {
		// The PDF library panics on some malformed files.
		if r := recover(); r != nil {
			text, err = "", userErr("Не удалось прочитать PDF. Проверьте, что файл не повреждён.")
		}
	}()
	if !bytes.HasPrefix(bytes.TrimLeft(data, "\x00\t\r\n "), []byte("%PDF")) {
		return "", userErr("Не удалось прочитать PDF. Проверьте, что файл не повреждён.")
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "encrypt") || strings.Contains(strings.ToLower(err.Error()), "password") {
			return "", userErr("PDF защищён паролем. Загрузите незашифрованный файл.")
		}
		return "", userErr("Не удалось прочитать PDF. Проверьте, что файл не повреждён.")
	}
	if reader.NumPage() > 1500 {
		return "", userErr("В PDF больше 1500 страниц. Разделите документ.")
	}
	var chunks []string
	total := 0
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		chunk := pageText(page)
		total += len(chunk)
		if total > MaxChars*2 {
			return "", userErr("В PDF слишком много текста.")
		}
		chunks = append(chunks, chunk)
	}
	text = strings.Join(chunks, "\n\n")
	if strings.TrimSpace(text) == "" {
		return "", userErr("В PDF нет текстового слоя. Сначала распознайте скан с помощью OCR.")
	}
	return text, nil
}

// pageText joins text runs line by line so words keep their spaces.
func pageText(page pdf.Page) string {
	rows, err := page.GetTextByRow()
	if err != nil || len(rows) == 0 {
		plain, _ := page.GetPlainText(nil)
		return plain
	}
	var b strings.Builder
	for _, row := range rows {
		var line strings.Builder
		var lastX, lastW float64
		for i, word := range row.Content {
			if i > 0 && word.X-(lastX+lastW) > word.FontSize*0.15 && !strings.HasSuffix(line.String(), " ") && !strings.HasPrefix(word.S, " ") {
				line.WriteString(" ")
			}
			line.WriteString(word.S)
			lastX, lastW = word.X, word.W
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
	}
	return b.String()
}

type container struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opf struct {
	Items []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

func parseEPUB(data []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", userErr("Не удалось прочитать EPUB. Проверьте структуру файла.")
	}
	var unpacked uint64
	files := map[string]*zip.File{}
	for _, f := range archive.File {
		unpacked += f.UncompressedSize64
		files[f.Name] = f
	}
	if len(archive.File) > 2500 || unpacked > 40*1024*1024 {
		return "", userErr("EPUB слишком велик после распаковки. Разделите книгу.")
	}
	for _, f := range archive.File {
		size, packed := f.UncompressedSize64, max(f.CompressedSize64, 1)
		if size > 12*1024*1024 || (size > 1_000_000 && size/packed > 250) {
			return "", userErr("EPUB содержит чрезмерно сжатые или большие элементы.")
		}
	}
	read := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, errors.New("missing")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		// Never trust the declared size: read at most the limit plus one byte.
		out, err := io.ReadAll(io.LimitReader(rc, 12*1024*1024+1))
		if err != nil || len(out) > 12*1024*1024 {
			return nil, errors.New("too large")
		}
		return out, nil
	}
	raw, err := read("META-INF/container.xml")
	if err != nil {
		return "", userErr("В EPUB отсутствует описание контейнера.")
	}
	var c container
	if xml.Unmarshal(raw, &c) != nil || len(c.Rootfiles) == 0 {
		return "", userErr("Некорректная структура EPUB.")
	}
	opfPath := c.Rootfiles[0].FullPath
	raw, err = read(opfPath)
	if err != nil {
		return "", userErr("В EPUB не найдено оглавление.")
	}
	var pkg opf
	if xml.Unmarshal(raw, &pkg) != nil {
		return "", userErr("Некорректная структура EPUB.")
	}
	manifest := map[string]string{}
	for _, item := range pkg.Items {
		if item.ID != "" {
			manifest[item.ID] = item.Href
		}
	}
	if len(pkg.Spine) == 0 {
		return "", userErr("В EPUB отсутствует порядок глав.")
	}
	base := path.Dir(opfPath)
	var chunks []string
	for _, ref := range pkg.Spine {
		link, err := url.Parse(manifest[ref.IDRef])
		if err != nil || link.Scheme != "" || link.Host != "" {
			return "", userErr("EPUB содержит внешнюю ссылку вместо главы.")
		}
		name := path.Clean(path.Join(base, link.Path))
		if strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") || files[name] == nil || link.Path == "" {
			return "", userErr("В EPUB не найдена одна из глав. Проверьте целостность книги.")
		}
		content, err := read(name)
		if err != nil {
			return "", userErr("В EPUB не найдена одна из глав. Проверьте целостность книги.")
		}
		chunks = append(chunks, strings.TrimSpace(htmlText(bytes.TrimPrefix(content, []byte("\xef\xbb\xbf")))))
	}
	return strings.Join(chunks, "\n\n"), nil
}

var blockTags = map[string]bool{"p": true, "div": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true, "li": true}

// htmlText converts XHTML to text, putting block elements on separate lines.
func htmlText(content []byte) string {
	z := html.NewTokenizer(bytes.NewReader(content))
	var b strings.Builder
	hidden := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String()
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" {
				hidden++
			}
			if blockTags[tag] {
				b.WriteString("\n")
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" {
				hidden = max(0, hidden-1)
			}
			if blockTags[tag] && tag != "br" {
				b.WriteString("\n")
			}
		case html.TextToken:
			if hidden == 0 {
				b.Write(z.Text())
			}
		}
	}
}

// Describe returns a user-facing message for parse errors.
func Describe(err error) string {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue.Message
	}
	return fmt.Sprintf("Не удалось обработать файл: %v", err)
}
