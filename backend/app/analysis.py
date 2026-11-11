"""Offline text analysis. All relations are evidence-backed hypotheses, never facts."""
from __future__ import annotations
import re
from collections import Counter, defaultdict
from functools import lru_cache
from itertools import combinations

import networkx as nx
import pymorphy3

MORPH = pymorphy3.MorphAnalyzer()
WORD = re.compile(r'[A-Za-zА-Яа-яЁё]+(?:-[A-Za-zА-Яа-яЁё]+)?')
STOP = set('это этот тот такой свой наш ваш его её их весь каждый другой один два три первый второй третий глава часть том раздел день раз год человек время место сторона случай дело конец начало вопрос ответ пример текст слово автор просто очень только также уже ещё все всё она они оно был быть есть стать мочь который какой такой здесь там когда тогда потому поэтому чтобы если или для про при над под без через между после перед около'.split())
LOCATION_CUES = {'город', 'река', 'станция', 'деревня', 'озеро', 'море', 'остров', 'страна', 'улица', 'площадь', 'маяк', 'лес', 'бухта', 'архив', 'лаборатория', 'школа', 'университет', 'парк', 'дом'}
EVENT_CUES = {'экспедиция','встреча','поездка','буря','находка','возвращение','переезд','сражение','битва','смерть','пожар','война','свадьба','путешествие','открытие','исследование','выборы','рождение','бегство','прибытие','отъезд','спасение','падение','переворот','восстание','строительство','эксперимент','обучение'}

@lru_cache(maxsize=50000)
def parse_word(word):
    candidates = MORPH.parse(word.lower())
    first = candidates[0]
    if 'Name' in first.tag and 'Fixd' in first.tag:
        return next((p for p in candidates if 'Name' in p.tag and 'Fixd' not in p.tag), first)
    return first

@lru_cache(maxsize=50000)
def lemma(word):
    return parse_word(word).normal_form

@lru_cache(maxsize=50000)
def name_form(word, gender=None):
    candidates = MORPH.parse(word.lower())
    picked = parse_word(word)
    if gender:
        picked = next((p for p in candidates if 'Surn' in p.tag and gender in p.tag), picked)
    form = picked.inflect({'nomn'} | ({gender} if gender else set()))
    return (form.word if form else picked.normal_form).capitalize()

def split_chapters(text):
    heading = re.compile(r'(?im)^\s*((?:глава|часть|раздел|chapter|part)\s+(?:[\divxlcIVXLC]+|[А-Яа-яЁё]+)[^\n]{0,100})\s*$')
    matches = list(heading.finditer(text))
    chapters = []
    if matches:
        if text[:matches[0].start()].strip(): chapters.append(('Вступление',text[:matches[0].start()].strip()))
        for i,m in enumerate(matches):
            end = matches[i+1].start() if i+1 < len(matches) else len(text)
            chapters.append((m.group(1).strip(),text[m.end():end].strip()))
    else:
        # Bounded paragraph blocks preserve original wording, even for unstructured long texts.
        start = 0
        while start < len(text):
            end = min(start + 10000, len(text))
            if end < len(text):
                gap = text.rfind('\n\n', start+3000, end)
                if gap > start: end = gap
            chapters.append((f'Блок {len(chapters)+1}',text[start:end].strip()))
            start = end
    return [{'id':f'ch-{i+1}','title':title,'text':body,'word_count':len(WORD.findall(body)),'themes':[],'entity_counts':{},'event_count':0} for i,(title,body) in enumerate(chapters) if body]

def sentences(text):
    for fragment in re.split(r'(?<=[.!?…])\s+|\n\s*\n', text):
        fragment = fragment.strip()
        if fragment:
            # Quotes stay contiguous source substrings; long paragraphs are bounded for displays.
            for start in range(0, len(fragment), 900):
                yield fragment[start:start+900]

def analyze(text):
    chapters = split_chapters(text)
    sentence_rows = []
    nouns = Counter()
    proper = Counter()
    full_names = Counter()
    token_to_names = defaultdict(set)
    for ch_idx, chapter in enumerate(chapters):
        for sentence in sentences(chapter['text']):
            words = WORD.findall(sentence)
            lemmas = [lemma(w) for w in words]
            sentence_rows.append((ch_idx,sentence,words,lemmas))
            for w,l in zip(words,lemmas):
                p = parse_word(w)
                if len(l)>2 and l not in STOP and p.tag.POS in ('NOUN','ADJF'):
                    nouns[l] += 1
                if w[0].isupper() and (('Name' in p.tag or 'Surn' in p.tag) or any('Name' in q.tag for q in MORPH.parse(w.lower())[:3])):
                    proper[name_form(w)] += 1
            for match in re.finditer(r'(?=\b([А-ЯЁ][а-яё]+)[ \t]+([А-ЯЁ][а-яё]+)\b)',sentence):
                a,b = match.groups()
                pa,pb = parse_word(a),parse_word(b)
                if 'Name' in pa.tag and 'Surn' in pb.tag:
                    full = f'{name_form(a)} {name_form(b, pa.tag.gender)}'
                    full_names[full] += 1
    for full in full_names:
        for part in full.split(): token_to_names[lemma(part)].add(full)
    candidates = []
    for full in full_names:
        candidates.append((full,'person',sum(proper[p] for p in full.split())))
    for name,count in proper.most_common(25):
        if not token_to_names[lemma(name)] and count >= 1: candidates.append((name,'person',count))
    people_lemmas = {lemma(part) for name,typ,_ in candidates if typ == 'person' for part in name.split()}
    place_candidates = Counter()
    for _,sentence,words,ls in sentence_rows:
        for i,(w,l) in enumerate(zip(words,ls)):
            p = parse_word(w)
            if 'Geox' in p.tag:
                place_candidates[l.capitalize()] += 1
            if l in LOCATION_CUES:
                label = l.capitalize()
                if i+1<len(words) and words[i+1][0].isupper() and lemma(words[i+1]) not in people_lemmas:
                    label += ' ' + lemma(words[i+1]).capitalize()
                place_candidates[label] += 1
    for label,count in place_candidates.most_common(14): candidates.append((label,'place',count))
    excluded = people_lemmas | {lemma(w) for label in place_candidates for w in label.split()}
    for word,count in nouns.most_common():
        if word in EVENT_CUES:
            candidates.append((word.capitalize(),'event',count))
    for word,count in nouns.most_common():
        if word not in excluded and word not in EVENT_CUES and parse_word(word).tag.POS == 'NOUN':
            candidates.append((word.capitalize(),'concept',count))
            if sum(x[1]=='concept' for x in candidates)>=24: break
    # De-duplicate and bound graph complexity while preserving person/location/event priority.
    unique = {}
    for label,typ,count in candidates:
        if label.casefold() not in unique: unique[label.casefold()] = (label,typ,count)
    candidates = list(unique.values())[:70]
    entities = []
    matchers = {}
    for i,(label,typ,_) in enumerate(candidates):
        eid = f'e-{i+1}'
        target = {lemma(p) for p in label.split()}
        if typ == 'person':
            aliases = set()
            for p in label.split():
                if len(token_to_names[lemma(p)])<=1: aliases.add(lemma(p))
            if not aliases: aliases = target
            matchers[eid] = (typ,aliases)
        else: matchers[eid] = (typ,target)
        entities.append({'id':eid,'label':label,'type':typ,'count':0,'description':'','quotes':[]})
    by_id = {e['id']:e for e in entities}
    edge_rows = {}
    for ch_idx,sentence,words,ls in sentence_rows:
        counts = Counter(ls)
        present = []
        for eid,(typ,target) in matchers.items():
            if typ == 'person': count = max((counts[l] for l in target),default=0)
            else:
                count = min((counts[l] for l in target),default=0)
            if not count: continue
            entity = by_id[eid]
            entity['count'] += count
            chapter = chapters[ch_idx]
            chapter['entity_counts'][eid] = chapter['entity_counts'].get(eid,0)+count
            if typ=='event': chapter['event_count'] += count
            if len(entity['quotes'])<5 and not any(q['text']==sentence for q in entity['quotes']):
                entity['quotes'].append({'text':sentence,'chapter':chapter['title']})
            present.append(eid)
        # At most eight entities per sentence limits quadratic work for noisy paragraphs.
        for a,b in combinations(present[:8],2):
            key = tuple(sorted((a,b)))
            if key not in edge_rows: edge_rows[key] = {'weight':0,'evidence':[],'type':'cooccurrence','label':'Совместное упоминание'}
            edge = edge_rows[key]
            edge['weight'] += 1
            if len(edge['evidence'])<3 and sentence not in edge['evidence']: edge['evidence'].append(sentence)
            types = {by_id[a]['type'],by_id[b]['type']}
            person_count = sum(by_id[x]['type']=='person' for x in present)
            if types=={'person'} and person_count==2 and re.search(r'\b(?:брат|сестр|отец|мать|сын|дочь|муж|жен[аыу])',sentence.lower()):
                edge.update(type='family',label='Возможное родство')
            elif types=={'person'} and re.search(r'\b(?:встрет|помо[гч]|помощ|сотруднич|вместе|друг)',sentence.lower()) and edge['type']=='cooccurrence':
                edge.update(type='social',label='Возможное взаимодействие')
            elif types=={'event'} and re.search(r'\b(?:поэтому|из-за|привело к|привела к|вызвал[аои]?|потому что)\b',sentence.lower()):
                edge.update(type='causal',label='Возможная причинная связь')
            elif types=={'event'} and re.search(r'\b(?:после|перед|затем|позже|раньше|сначала)\b',sentence.lower()):
                edge.update(type='temporal',label='Возможная временная связь')
            elif 'concept' in types and edge['type']=='cooccurrence':
                edge.update(type='thematic',label='Тематическая близость')
    entities = [e for e in entities if e['count']]
    for entity in entities:
        kind = {'person':'Персонаж или имя','place':'Место','concept':'Ключевое понятие','event':'Событие или процесс'}[entity['type']]
        entity['description'] = f'{kind}, автоматически выделенное по словам текста. Упоминаний: {entity["count"]}. Проверьте контекст в цитатах.'
    edges = []
    valid = {e['id'] for e in entities}
    for (a,b),edge in sorted(edge_rows.items(),key=lambda row:row[1]['weight'],reverse=True):
        if a in valid and b in valid:
            edges.append({'id':f'r-{len(edges)+1}','source':a,'target':b,**edge,'inferred':True})
        if len(edges)>=220: break
    graph = nx.Graph()
    graph.add_nodes_from(valid)
    graph.add_edges_from((e['source'],e['target']) for e in edges)
    for entity in entities: entity['degree'] = graph.degree(entity['id'])
    for chapter in chapters:
        tops = sorted(chapter['entity_counts'], key=chapter['entity_counts'].get, reverse=True)
        chapter['themes'] = [by_id[x]['label'] for x in tops if by_id[x]['type'] in ('concept','event')][:5]
    keywords = [{'word':w,'count':c} for w,c in nouns.most_common(35) if w not in people_lemmas]
    return {'method':'Локальный морфологический и частотный анализ (pymorphy3 + NetworkX)',
            'limitations':'Эвристический анализ: имена могут разделяться или объединяться неверно. Рёбра — предположения по совместным упоминаниям и языковым маркерам; они не доказывают родство, причины или мотивы. Вопросы обрабатываются поиском цитат без генеративной модели. Граф ограничен 70 сущностями и 220 связями.',
            'entities':entities,'edges':edges,'chapters':chapters,'keywords':keywords,'word_count':len(WORD.findall(text))}

def answer_question(question, analysis):
    query_lemmas = {lemma(w) for w in WORD.findall(question) if lemma(w) not in STOP and len(w)>2}
    # Queries without a lexical match return an explicit non-answer, never an invented summary.
    scored = []
    seen = set()
    for chapter in analysis['chapters']:
        for sentence in sentences(chapter['text']):
            if sentence in seen: continue
            seen.add(sentence)
            ls = {lemma(w) for w in WORD.findall(sentence)}
            shared = ls & query_lemmas
            if shared:
                scored.append((len(shared)/max(len(query_lemmas),1),len(shared),{'text':sentence,'chapter':chapter['title']}))
    scored.sort(key=lambda x:(x[0],x[1]),reverse=True)
    sources = [row[2] for row in scored[:4]]
    if not sources:
        return 'В тексте не найдено фрагментов с ключевыми словами вопроса. Уточните имя, место или понятие. По имеющимся данным нельзя сделать достоверный вывод.',[]
    answer = 'Найдены фрагменты по ключевым словам вопроса. Они помогают проверить содержание, но не являются сгенерированным объяснением мотивов или причин.\n\n'
    answer += '\n\n'.join(f'«{s["text"]}» — {s["chapter"]}' for s in sources)
    return answer,sources
