package analysis

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Word matches Russian and Latin words, optionally joined by one hyphen.
var Word = regexp.MustCompile(`[A-Za-zА-Яа-яЁё]+(?:-[A-Za-zА-Яа-яЁё]+)?`)

func set(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}

// Normalize lowercases a word and folds ё into е so inflected forms share a key.
func Normalize(w string) string {
	return strings.ReplaceAll(strings.ToLower(w), "ё", "е")
}

// Case endings stripped to build a grouping key, longest first. Names keep
// -ов/-ев/-ей (Морозов, Сергей) so male and female forms stay consistent.
var nameEndings = []string{"иями", "ями", "ами", "ией", "ием", "иях", "иям", "ого", "его", "ому", "ему", "ыми", "ими",
	"ой", "ою", "ею", "ом", "ем", "ам", "ям", "ах", "ях", "ия", "ие", "ии", "ию", "ий", "ья", "ье", "ьи", "ью",
	"ый", "ая", "яя", "ое", "ее", "ые", "ых", "их", "ую", "юю", "ым", "им", "а", "я", "о", "е", "у", "ю", "ы", "и", "ь", "й"}
var wordEndings = append([]string{"ов", "ев", "ей"}, nameEndings...)

func sortedByLength(endings []string) []string {
	out := append([]string(nil), endings...)
	sort.SliceStable(out, func(i, j int) bool { return utf8.RuneCountInString(out[i]) > utf8.RuneCountInString(out[j]) })
	return out
}

var nameEndingsSorted = sortedByLength(nameEndings)
var wordEndingsSorted = sortedByLength(wordEndings)

func isCyrillic(w string) bool {
	for _, r := range w {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func stripEnding(n string, endings []string) string {
	if !isCyrillic(n) {
		return n
	}
	length := utf8.RuneCountInString(n)
	for _, e := range endings {
		if strings.HasSuffix(n, e) && length-utf8.RuneCountInString(e) >= 2 {
			return strings.TrimSuffix(n, e)
		}
	}
	return n
}

// Stem returns the grouping key for a word form by removing case endings.
// Capitalised words use the name rules. This replaces dictionary lemmatisation.
func Stem(w string) string {
	n := Normalize(w)
	endings := wordEndingsSorted
	if isCapitalized(w) {
		endings = nameEndingsSorted
	}
	if strings.Contains(n, "-") {
		parts := strings.Split(n, "-")
		for i, p := range parts {
			parts[i] = stripEnding(p, endings)
		}
		return strings.Join(parts, "-")
	}
	return stripEnding(n, endings)
}

var stopWords = set(`это этот эта эти тот та те то такой такая такое такие свой своя свое свои наш наша ваш ваша его ее её их весь вся все всё каждый другой один одна два две три первый второй третий
глава часть том раздел день раз год человек время место сторона случай дело конец начало вопрос ответ пример текст слово автор
просто очень только также уже еще ещё она они оно он был была было были быть есть стать мочь который которая которое которые какой какая какое
здесь там тут когда тогда потому поэтому чтобы если или для про при над под без через между после перед около
что как так не ни но да нет и а в во на с со о об обо по к ко у из от до за же ли бы вот вдруг себя себе собой сам сама само сами самый мой моя твой
кто где куда откуда почему зачем чем чём кого кому ним ней нему него нее неё них нам вам мне тебе меня тебя нас вас ему ей им ими
этого этому этим этом той тех тем того тому всех всем всего всему всеми всю свою своей своего своему своих своим нашей нашего
сказал сказала говорил говорила может можно нужно надо будет будут было стало стал стала потом теперь сейчас снова опять даже лишь
the and for with that this from was were are has have had his her its they them their into about there which what when who will would`)

var stopStems = func() map[string]bool {
	out := map[string]bool{}
	for w := range stopWords {
		out[Stem(w)] = true
		out[Stem(Capitalize(w))] = true
	}
	return out
}()

// IsStop reports whether a word (or its stem) is a function word.
func IsStop(w string) bool {
	n := Normalize(w)
	return stopWords[n] || stopWords[strings.ToLower(w)] || stopStems[Stem(w)]
}

var prepositions = set("в во на с со о об обо по к ко у из от до за для без при про через после перед около между над под среди возле")
var placePrepositions = set("в во из на")

var locationCues = []string{"город", "река", "станция", "деревня", "озеро", "море", "остров", "страна", "улица", "площадь", "маяк", "лес", "бухта", "архив", "лаборатория", "школа", "университет", "парк", "дом"}
var eventCues = []string{"экспедиция", "встреча", "поездка", "буря", "находка", "возвращение", "переезд", "сражение", "битва", "смерть", "пожар", "война", "свадьба", "путешествие", "открытие", "исследование", "выборы", "рождение", "бегство", "прибытие", "отъезд", "спасение", "падение", "переворот", "восстание", "строительство", "эксперимент", "обучение"}

func cueMap(words []string) map[string]string {
	out := map[string]string{}
	for _, w := range words {
		out[Stem(w)] = w
	}
	return out
}

var locationStems = cueMap(locationCues)
var eventStems = cueMap(eventCues)

var adjEndings = []string{"ого", "его", "ому", "ему", "ыми", "ими", "ый", "ий", "ой", "ая", "яя", "ое", "ее", "ые", "ие", "ым", "им", "ых", "их", "ую", "юю"}
var verbEndings = []string{"ться", "тся", "ть", "лся", "лась", "лось", "лись", "ешь", "ишь", "ете", "ите", "ют", "ят", "ует", "ает", "яет", "еет", "ил", "ал", "ял", "ел", "ыл", "ул", "ила", "ала", "яла", "ела", "ыла", "ула", "или", "али", "яли", "ели", "ыли", "ули", "ило", "ало", "яло", "ело", "ыло", "уло", "вшись", "вши"}

var nounEndings = func() []string {
	var out []string
	families := map[string][]string{
		"ни":   {"е", "я", "ю", "ем", "и", "й", "ям", "ями", "ях"},
		"ти":   {"е", "я", "ю", "ем", "и", "й", "ям", "ями", "ях"},
		"ци":   {"я", "и", "ю", "ей", "й", "ям", "ями", "ях"},
		"ость": {""},
		"ост":  {"и", "ью", "ей", "ям", "ями", "ях"},
		"ств":  {"о", "а", "у", "ом", "е", ""},
		"тел":  {"ь", "я", "ю", "ем", "е", "и", "ей", "ям", "ями", "ях"},
		"ник":  {"", "а", "у", "ом", "е", "и", "ов", "ам", "ами", "ах"},
		"изм":  {"", "а", "у", "ом", "е"},
		"ия":   {""}, "ии": {""}, "ию": {""}, "ией": {""},
		"к": {"а", "и", "у", "ой", "е"},
	}
	for base, ends := range families {
		for _, e := range ends {
			out = append(out, base+e)
		}
	}
	return out
}()

func endsAny(w string, endings []string) bool {
	for _, e := range endings {
		if strings.HasSuffix(w, e) && utf8.RuneCountInString(w) > utf8.RuneCountInString(e)+1 {
			return true
		}
	}
	return false
}

func hasNounSuffix(n string) bool {
	for _, e := range nounEndings {
		if !strings.HasSuffix(n, e) || utf8.RuneCountInString(n) <= utf8.RuneCountInString(e)+1 {
			continue
		}
		// -ской/-цкая are adjectives, not nouns in -ка.
		if strings.HasPrefix(e, "к") {
			before := []rune(strings.TrimSuffix(n, e))
			if last := before[len(before)-1]; last == 'с' || last == 'ц' {
				continue
			}
		}
		return true
	}
	return false
}

// isAttributive reports adjective-like forms including -ом/-ем (в сухом контейнере).
func isAttributive(n string) bool {
	return (isAdjForm(n) || endsAny(n, []string{"ом", "ем"})) && !hasNounSuffix(n)
}
func isAdjForm(n string) bool  { return endsAny(n, adjEndings) && !hasNounSuffix(n) }
func isVerbForm(n string) bool { return endsAny(n, verbEndings) && !hasNounSuffix(n) }

func isCapitalized(w string) bool {
	r, _ := utf8.DecodeRuneInString(w)
	return unicode.IsUpper(r)
}

// Capitalize uppercases the first letter and keeps the rest of the word.
func Capitalize(w string) string {
	r, size := utf8.DecodeRuneInString(w)
	if r == utf8.RuneError {
		return w
	}
	return string(unicode.ToUpper(r)) + w[size:]
}

// endingRank orders what is left after the stem: dictionary (nominative)
// forms usually leave nothing, a soft sign or a typical singular ending.
var endingRank = map[string]int{"": 0, "ие": 1, "ия": 2, "ь": 3, "й": 4, "о": 5, "а": 6, "я": 7, "ей": 8, "ы": 9, "и": 10, "е": 11}

func remainders(stem string, forms map[string]int) map[string]string {
	out := map[string]string{}
	for w := range forms {
		if norm := Normalize(w); strings.HasPrefix(norm, stem) {
			out[norm[len(stem):]] = w
		}
	}
	return out
}

func total(forms map[string]int) int {
	n := 0
	for _, c := range forms {
		n += c
	}
	return n
}

func hasAny(r map[string]string, keys ...string) bool {
	for _, k := range keys {
		if _, ok := r[k]; ok {
			return true
		}
	}
	return false
}

// Nominal picks or reconstructs the dictionary form of a word from its observed forms.
// Capitalised names are only chosen from observed spellings; common nouns may be
// rebuilt from case endings (реки, реке → река is not guessed, экспедиции → экспедиция is).
func Nominal(stem string, forms map[string]int) string {
	if len(forms) == 0 {
		return stem
	}
	r := remainders(stem, forms)
	var sample string
	for w := range forms {
		sample = w
		break
	}
	if !isCapitalized(sample) {
		deverbal := (strings.HasSuffix(stem, "ен") || strings.HasSuffix(stem, "ан") || strings.HasSuffix(stem, "ти")) && utf8.RuneCountInString(stem) >= 5 && !hasAny(r, "ией")
		switch {
		case hasAny(r, ""):
			return r[""]
		case hasAny(r, "ие"):
			return r["ие"]
		case hasAny(r, "ия", "ии", "ию", "ией", "иям", "иями", "иях", "ием"):
			if deverbal || hasAny(r, "ием") {
				return stem + "ие"
			}
			return stem + "ия"
		case strings.HasSuffix(stem, "тел") && hasAny(r, "ей", "ям", "ями", "ях", "я", "ю", "ем", "и"):
			return stem + "ь"
		case total(forms) < 2:
			// A single occurrence is shown as written rather than guessed.
		case hasAny(r, "ь"):
			return r["ь"]
		case hasAny(r, "о"):
			return r["о"]
		case hasAny(r, "а", "я") && hasAny(r, "ом", "ов") && !hasAny(r, "ой", "ою"):
			return stem
		case hasAny(r, "ом", "ов") && !hasAny(r, "ой", "ою"):
			return stem
		case hasAny(r, "ой", "ою") && !hasAny(r, "а", "я"):
			return stem + "а"
		}
	}
	maxCount := 0
	for _, n := range forms {
		maxCount = max(maxCount, n)
	}
	type option struct {
		word  string
		count int
		rank  int
	}
	var options []option
	for w, n := range forms {
		rank := 99
		if norm := Normalize(w); strings.HasPrefix(norm, stem) {
			if v, ok := endingRank[norm[len(stem):]]; ok {
				rank = v
			}
		}
		options = append(options, option{w, n, rank})
	}
	sort.Slice(options, func(i, j int) bool {
		a, b := options[i], options[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.count != b.count {
			return a.count > b.count
		}
		la, lb := utf8.RuneCountInString(a.word), utf8.RuneCountInString(b.word)
		if la != lb {
			return la < lb
		}
		return a.word < b.word
	})
	return options[0].word
}
