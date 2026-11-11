package analysis

import (
	"strings"
	"testing"
)

const story = `Глава 1. Письмо из Томска

Анна Морозова жила в Томске. Её брат Михаил Морозов работал на станции «Полюс». Анна — сестра Михаила. Михаил отправил Анне письмо. Виктор Лебедев согласился помочь Анне.

Глава 2. Дорога

Анна и Виктор встретились у реки Обь. Вечером началась буря, поэтому экспедиция задержалась. Михаилом гордилась Анна.`

func labels(r Result, kind string) []string {
	var out []string
	for _, e := range r.Entities {
		if e.Type == kind {
			out = append(out, e.Label)
		}
	}
	return out
}

func TestNamesAreGroupedAcrossCases(t *testing.T) {
	r := Analyze(story)
	people := strings.Join(labels(r, "person"), "|")
	for _, want := range []string{"Анна Морозова", "Михаил Морозов", "Виктор Лебедев"} {
		if !strings.Contains(people, want) {
			t.Fatalf("missing %q in %s", want, people)
		}
	}
	for _, unwanted := range []string{"Анне", "Михаилом", "Вечером"} {
		if strings.Contains(people, unwanted) {
			t.Fatalf("inflected form %q leaked into %s", unwanted, people)
		}
	}
	places := strings.Join(labels(r, "place"), "|")
	if !strings.Contains(places, "Томск") || !strings.Contains(places, "Река Обь") || !strings.Contains(places, "Станция Полюс") {
		t.Fatalf("places: %s", places)
	}
}

func TestRelationsAreEvidenceBacked(t *testing.T) {
	r := Analyze(story)
	family := false
	for _, e := range r.Edges {
		if len(e.Evidence) == 0 || !e.Inferred {
			t.Fatalf("edge without evidence: %+v", e)
		}
		for _, q := range e.Evidence {
			if !strings.Contains(story, q) {
				t.Fatalf("evidence is not a substring: %q", q)
			}
		}
		family = family || e.Type == "family"
	}
	if !family {
		t.Fatal("brother/sister sentence should give a possible kinship edge")
	}
}

func TestSentencesAreExactSubstrings(t *testing.T) {
	text := "Первое предложение! Второе?  Третье…\n\nАбзац без точки\nпродолжение"
	got := Sentences(text)
	want := []string{"Первое предложение!", "Второе?", "Третье…", "Абзац без точки\nпродолжение"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("слово ", 400)
	for _, s := range Sentences(long) {
		if len([]rune(s)) > 900 || !strings.Contains(long, s) {
			t.Fatal("long fragment not bounded or not a substring")
		}
	}
}

func TestChaptersAndBlocks(t *testing.T) {
	if n := len(SplitChapters(story)); n != 2 {
		t.Fatalf("chapters: %d", n)
	}
	blocks := SplitChapters(strings.Repeat("Абзац текста без заголовков. ", 1200))
	if len(blocks) < 2 || !strings.HasPrefix(blocks[0].Title, "Блок") {
		t.Fatalf("blocks: %d", len(blocks))
	}
}

func TestNominalForms(t *testing.T) {
	cases := map[string]map[string]int{
		"экспедиция": {"экспедиции": 2, "экспедиций": 1},
		"наблюдение": {"наблюдения": 2},
		"журнал":     {"журнале": 1, "журнал": 1},
		"Михаил":     {"Михаила": 1, "Михаилом": 1, "Михаил": 1},
		"Анна":       {"Анне": 2, "Анну": 1, "Анна": 1},
	}
	for want, forms := range cases {
		var sample string
		for f := range forms {
			sample = f
			break
		}
		if got := Nominal(Stem(sample), forms); got != want {
			t.Errorf("Nominal(%v) = %q, want %q", forms, got, want)
		}
	}
}

func TestAnswerCitesSources(t *testing.T) {
	r := Analyze(story)
	answer, sources := Answer("Куда отправил письмо Михаил?", r)
	if len(sources) == 0 || !strings.Contains(answer, sources[0].Text) {
		t.Fatalf("answer: %s", answer)
	}
	if _, none := Answer("Квазиплазменная нейтронизация", r); len(none) != 0 {
		t.Fatal("unrelated question should have no sources")
	}
}
