// Package export renders the PDF report and PNG graph with a Cyrillic font.
package export

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	"github.com/fogleman/gg"
	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"semanticatlas/backend/internal/analysis"
)

//go:embed assets/DejaVuSans.ttf
var fontData []byte

var typeLabels = map[string]string{"person": "Персонаж", "concept": "Понятие", "place": "Место", "event": "Событие"}
var typeColors = map[string]string{"person": "#2c6bba", "concept": "#288574", "place": "#b37b34", "event": "#9865b8"}

// Report holds what both exports need from a document.
type Report struct {
	Title    string
	Author   string
	Analysis analysis.Result
}

func sortedEntities(a analysis.Result) []analysis.Entity {
	out := append([]analysis.Entity(nil), a.Entities...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// PDF builds an A4 analytic report.
func PDF(r Report) ([]byte, error) {
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.AddUTF8FontFromBytes("Atlas", "", fontData)
	pdf.SetTitle(r.Title, true)
	pdf.SetAuthor("Семантический атлас", true)
	pdf.SetMargins(60, 55, 60)
	pdf.SetAutoPageBreak(true, 55)
	pageW, _ := pdf.GetPageSize()
	pdf.SetFooterFunc(func() {
		pdf.SetY(-38)
		pdf.SetFont("Atlas", "", 8)
		pdf.SetTextColor(0x57, 0x64, 0x77)
		pdf.CellFormat(0, 10, "Семантический атлас · Аналитический отчёт", "", 0, "L", false, 0, "")
		pdf.CellFormat(0, 10, fmt.Sprint(pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	pdf.AddPage()
	width := pageW - 120
	text := func(size, leading float64, value string, r, g, b int) {
		pdf.SetFont("Atlas", "", size)
		pdf.SetTextColor(r, g, b)
		pdf.MultiCell(width, leading, value, "", "L", false)
	}
	heading := func(value string) {
		pdf.Ln(12)
		text(14, 20, value, 0x17, 0x2d, 0x42)
		pdf.Ln(4)
	}
	a := r.Analysis
	text(22, 28, r.Title, 0x17, 0x2d, 0x42)
	pdf.Ln(8)
	author := r.Author
	if author == "" {
		author = "Автор не указан"
	}
	text(10, 14, author, 0x26, 0x26, 0x26)
	text(10, 14, fmt.Sprintf("Слов: %d · Сущностей: %d · Связей: %d · Разделов: %d", a.WordCount, len(a.Entities), len(a.Edges), len(a.Chapters)), 0x26, 0x26, 0x26)
	heading("Метод и ограничения")
	text(10, 14, a.Method, 0x26, 0x26, 0x26)
	pdf.Ln(4)
	text(10, 14, a.Limitations, 0x26, 0x26, 0x26)

	heading("Основные сущности")
	cols := []float64{260, 130, width - 390}
	row := func(values []string, header bool) {
		pdf.SetFont("Atlas", "", 9)
		lines := pdf.SplitText(values[0], cols[0]-12)
		h := float64(len(lines))*12 + 12
		if pdf.GetY()+h > 842-55 {
			pdf.AddPage()
		}
		x, y := pdf.GetX(), pdf.GetY()
		if header {
			pdf.SetFillColor(0xe9, 0xee, 0xf3)
		} else {
			pdf.SetFillColor(0xff, 0xff, 0xff)
		}
		pdf.SetDrawColor(0xd9, 0xd9, 0xd9)
		pdf.SetTextColor(0x26, 0x26, 0x26)
		offset := x
		for i, w := range cols {
			pdf.Rect(offset, y, w, h, "FD")
			align := "L"
			if i == 2 {
				align = "C"
			}
			cell := []string{values[i]}
			if i == 0 {
				cell = lines
			}
			for j, line := range cell {
				pdf.SetXY(offset+6, y+6+float64(j)*12)
				pdf.CellFormat(w-12, 12, line, "", 0, align, false, 0, "")
			}
			offset += w
		}
		pdf.SetXY(x, y+h)
	}
	row([]string{"Сущность", "Тип", "Упоминания"}, true)
	for _, e := range sortedEntities(a) {
		row([]string{e.Label, typeLabels[e.Type], fmt.Sprint(e.Count)}, false)
	}

	heading("Структура текста")
	for _, ch := range a.Chapters {
		text(10, 14, ch.Title, 0x26, 0x26, 0x26)
		themes := strings.Join(ch.Themes, ", ")
		if themes == "" {
			themes = "не определены"
		}
		text(8, 11.5, fmt.Sprintf("Слов: %d. Темы: %s.", ch.WordCount, themes), 0x57, 0x64, 0x77)
		pdf.Ln(3)
	}

	heading("Связи и подтверждающие фрагменты")
	text(8, 11.5, fmt.Sprintf("Показаны %d наиболее частых связей из %d.", min(30, len(a.Edges)), len(a.Edges)), 0x57, 0x64, 0x77)
	labels := map[string]string{}
	for _, e := range a.Entities {
		labels[e.ID] = e.Label
	}
	for i, edge := range a.Edges {
		if i >= 30 {
			break
		}
		pdf.Ln(3)
		text(10, 14, fmt.Sprintf("%s — %s: %s (предположение)", labels[edge.Source], labels[edge.Target], edge.Label), 0x26, 0x26, 0x26)
		if len(edge.Evidence) > 0 {
			text(8, 11.5, edge.Evidence[0], 0x57, 0x64, 0x77)
		}
	}
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func face(size float64) (font.Face, error) {
	f, err := opentype.Parse(fontData)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}

func hex(c string) color.Color {
	var r, g, b uint8
	fmt.Sscanf(c, "#%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}

// PNG renders the whole graph with non-overlapping, type-grouped slots.
func PNG(r Report) ([]byte, error) {
	const w = 2400
	background := hex("#f7f9fc")
	regular, err := face(27)
	if err != nil {
		return nil, err
	}
	small, _ := face(22)
	titleFace, _ := face(43)
	groupFace, _ := face(31)
	measure := gg.NewContext(1, 1)
	textWidth := func(f font.Face, s string) float64 {
		measure.SetFontFace(f)
		width, _ := measure.MeasureString(s)
		return width
	}
	wrap := func(value string, f font.Face, width float64) []string {
		var lines []string
		line := ""
		for _, word := range strings.Fields(value) {
			candidate := strings.TrimSpace(line + " " + word)
			if textWidth(f, candidate) <= width {
				line = candidate
				continue
			}
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			for textWidth(f, word) > width {
				runes := []rune(word)
				split := 1
				for split < len(runes) && textWidth(f, string(runes[:split+1])) <= width {
					split++
				}
				lines = append(lines, string(runes[:split]))
				word = string(runes[split:])
			}
			line = word
		}
		if line != "" {
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			lines = []string{""}
		}
		return lines
	}

	a := r.Analysis
	entities := sortedEntities(a)
	groups := [][2]string{{"person", "Персонажи"}, {"place", "Места"}, {"event", "События"}, {"concept", "Понятия"}}
	titleLines := wrap(r.Title, titleFace, w-120)
	headerEnd := 55 + float64(len(titleLines))*57 + 100
	const columns, margin = 6, 65.0
	slotWidth := (w - margin*2) / columns
	cursor := headerEnd + 25
	type section struct {
		y           float64
		kind, label string
		count       int
	}
	type slot struct {
		entity analysis.Entity
		x, y   float64
		lines  []string
	}
	var sections []section
	var slots []slot
	coords := map[string][2]float64{}
	for _, g := range groups {
		var members []analysis.Entity
		for _, e := range entities {
			if e.Type == g[0] {
				members = append(members, e)
			}
		}
		if len(members) == 0 {
			continue
		}
		sections = append(sections, section{cursor, g[0], g[1], len(members)})
		cursor += 75
		for offset := 0; offset < len(members); offset += columns {
			rowMembers := members[offset:min(offset+columns, len(members))]
			maxLines := 1
			wrapped := make([][]string, len(rowMembers))
			for i, e := range rowMembers {
				wrapped[i] = wrap(e.Label, regular, slotWidth-25)
				maxLines = max(maxLines, len(wrapped[i]))
			}
			rowHeight := math.Max(180, 112+float64(maxLines)*36)
			for col, e := range rowMembers {
				x := margin + (float64(col)+.5)*slotWidth
				y := cursor + 35
				coords[e.ID] = [2]float64{x, y}
				slots = append(slots, slot{e, x, y, wrapped[col]})
			}
			cursor += rowHeight
		}
		cursor += 30
	}
	h := int(math.Max(650, cursor+125))
	dc := gg.NewContext(w, h)
	dc.SetColor(background)
	dc.Clear()
	drawText := func(f font.Face, s string, x, y float64, c string) {
		dc.SetFontFace(f)
		dc.SetColor(hex(c))
		dc.DrawStringAnchored(s, x, y, 0, 0.85)
	}
	for i, line := range titleLines {
		drawText(titleFace, line, 60, 45+float64(i)*57, "#172d42")
	}
	baseline := 55 + float64(len(titleLines))*57
	drawText(regular, fmt.Sprintf("Семантический атлас · %d слов · %d из %d сущностей · %d связей", a.WordCount, len(entities), len(a.Entities), len(a.Edges)), 60, baseline, "#576477")
	drawText(small, "Узлы сгруппированы по типу. Связи являются предположениями по тексту.", 60, baseline+42, "#576477")

	// Links are deliberately subordinate; every node keeps a protected, measured label slot.
	dc.SetColor(hex("#dce3ed"))
	for _, e := range a.Edges {
		p, ok1 := coords[e.Source]
		q, ok2 := coords[e.Target]
		if ok1 && ok2 {
			dc.SetLineWidth(float64(min(3, 1+e.Weight/4)))
			dc.DrawLine(p[0], p[1], q[0], q[1])
			dc.Stroke()
		}
	}
	for _, s := range sections {
		dc.SetColor(background)
		dc.DrawRectangle(45, s.y-5, w-90, 60)
		dc.Fill()
		dc.SetColor(hex(typeColors[s.kind]))
		dc.DrawCircle(71, s.y+19, 11)
		dc.Fill()
		drawText(groupFace, fmt.Sprintf("%s · %d", s.label, s.count), 98, s.y, "#24384e")
		dc.SetColor(hex("#d9e1ea"))
		dc.SetLineWidth(1)
		dc.DrawLine(60, s.y+52, w-60, s.y+52)
		dc.Stroke()
	}
	for _, s := range slots {
		radius := math.Min(33, 18+math.Sqrt(float64(s.entity.Count))*2)
		dc.SetColor(background)
		dc.DrawCircle(s.x, s.y, radius+6)
		dc.Fill()
		dc.SetColor(hex(typeColors[s.entity.Type]))
		dc.DrawCircle(s.x, s.y, radius)
		dc.Fill()
		labelTop := s.y + 48
		labelHeight := float64(len(s.lines))*36 + 33
		dc.SetColor(background)
		dc.DrawRectangle(s.x-slotWidth/2+8, labelTop-3, slotWidth-16, labelHeight+3)
		dc.Fill()
		for i, line := range s.lines {
			drawText(regular, line, s.x-textWidth(regular, line)/2, labelTop+float64(i)*36, "#24384e")
		}
		count := fmt.Sprintf("Упоминаний: %d", s.entity.Count)
		drawText(small, count, s.x-textWidth(small, count)/2, labelTop+float64(len(s.lines))*36+2, "#576477")
	}
	drawText(small, "Локальный эвристический анализ. Проверяйте предполагаемые связи по цитатам в приложении.", 60, float64(h)-94, "#576477")
	drawText(small, "Все выделенные сущности показаны. Размер узла отражает число упоминаний.", 60, float64(h)-56, "#576477")
	var out bytes.Buffer
	if err := dc.EncodePNG(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
