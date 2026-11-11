import React, { useEffect, useMemo, useRef, useState } from 'react';
import { AutoComplete, Button, Dropdown, Input, InputNumber, Tooltip } from 'antd';
import {
  AlertTriangle, ArrowLeft, ArrowRight, BookOpen, Calendar, Check, CheckCircle2, ChevronDown, ClipboardPaste,
  Clock, Cog, FileText, FlaskConical, GraduationCap, Loader2, MoreVertical, Network, Plus, RotateCcw,
  Tag, Trash2, Upload as UploadIcon, User, Users, X, Zap, BarChart3,
} from 'lucide-react';
import { api } from './workspace.jsx';
import { fileProblem } from './files.js';

const { TextArea } = Input;

export const KINDS = {
  'Художественный': { tone: 'blue', Icon: BookOpen },
  'Учебный': { tone: 'green', Icon: GraduationCap },
  'Научный': { tone: 'purple', Icon: FlaskConical },
};
const kindOf = genre => KINDS[genre] || { tone: 'slate', Icon: FileText };
const FORMAT = { pdf: 'PDF', txt: 'TXT', epub: 'EPUB', text: 'ТЕКСТ' };
const dayFormat = s => {
  if (!s) return '';
  const d = new Date(s.endsWith('Z') || s.includes('+') ? s : s + 'Z');
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' });
};
const PERIODS = [
  { key: '7', label: 'За неделю', days: 7 },
  { key: '30', label: 'За месяц', days: 30 },
  { key: '365', label: 'За год', days: 365 },
];

export const STAGES = [
  { key: 'extract', label: 'Извлечение текста', hint: 'Извлекаем текст и считаем слова…', Icon: FileText },
  { key: 'entities', label: 'Выделение сущностей', hint: 'Ищем персонажей, места и события…', Icon: Users },
  { key: 'relations', label: 'Определение связей', hint: 'Определяем связи между персонажами…', Icon: Network },
  { key: 'graph', label: 'Построение графа', hint: 'Строим граф связей…', Icon: Network },
  { key: 'report', label: 'Генерация отчёта', hint: 'Готовим аналитику по главам…', Icon: BarChart3 },
];
const STAGE_PROGRESS = { extract: 5, entities: 20, relations: 45, graph: 75, report: 90, done: 100 };
const stageIndex = stage => stage === 'done' ? STAGES.length : Math.max(0, STAGES.findIndex(s => s.key === stage));

function FileTile({ genre, source }) {
  const { tone, Icon } = kindOf(genre);
  return <div className={`file-tile tone-${tone}`}><Icon size={24} /><span>{FORMAT[source] || 'ТЕКСТ'}</span></div>;
}

function StatusTag({ status }) {
  if (status === 'processing') return <span className="sq-tag tone-orange"><Loader2 size={11} className="spin" />Анализируется</span>;
  if (status === 'error') return <span className="sq-tag tone-red"><span className="dot" />Ошибка</span>;
  return <span className="sq-tag tone-green"><CheckCircle2 size={11} />Готово</span>;
}

function FilterButton({ icon: Icon, label, value, options, onChange }) {
  const items = [{ key: '', label: 'Все' }, ...options.map(o => (typeof o === 'string' ? { key: o, label: o } : o))];
  const current = items.find(i => i.key === value);
  return <Dropdown trigger={['click']} menu={{ items, selectable: true, selectedKeys: [value || ''], onClick: ({ key }) => onChange(key) }}>
    <button type="button" className={`filter-button ${value ? 'active' : ''}`}>
      <Icon size={15} /><span>{value ? current?.label : label}</span><ChevronDown size={15} />
    </button>
  </Dropdown>;
}

function DocumentCard({ doc, onOpen, onDelete, onEdit, onRetry }) {
  const processing = doc.status === 'processing', failed = doc.status === 'error';
  const menu = [
    { key: 'open', label: 'Открыть', disabled: failed },
    { key: 'edit', label: 'Изменить сведения' },
    { key: 'retry', label: 'Проанализировать заново', disabled: processing },
    { type: 'divider' },
    { key: 'delete', label: 'Удалить', danger: true },
  ];
  const act = ({ key }) => ({ open: onOpen, edit: onEdit, retry: onRetry, delete: onDelete })[key]?.();
  return <article className={`doc-card ${processing ? 'is-processing' : ''}`}>
    <div className="doc-card-top">
      <FileTile genre={doc.genre} source={doc.source} />
      <div className="doc-card-info">
        <div className="doc-card-tags">
          <span className={`sq-tag tone-${kindOf(doc.genre).tone}`}>{doc.genre || 'Текст'}</span>
          <StatusTag status={doc.status} />
        </div>
        <h3><button type="button" onClick={failed ? onRetry : onOpen}>{doc.title}</button></h3>
        <p>{doc.author || 'Автор не указан'}{doc.subgenre ? ` · ${doc.subgenre}` : ''}</p>
      </div>
      <Dropdown trigger={['click']} menu={{ items: menu, onClick: act }} placement="bottomRight">
        <button type="button" className="kebab" aria-label={`Действия: ${doc.title}`}><MoreVertical size={18} /></button>
      </Dropdown>
    </div>
    {processing && <div className="card-progress">
      <div className="card-progress-label"><span>Прогресс анализа</span><span>{doc.progress || 0}%</span></div>
      <div className="bar"><span style={{ width: `${doc.progress || 0}%` }} /></div>
    </div>}
    {failed && <div className="card-error"><AlertTriangle size={18} /><span>{doc.error || 'Не удалось проанализировать текст.'}</span></div>}
    {!processing && !failed && doc.note && <p className="card-note">{doc.note}</p>}
    <div className="doc-card-footer">
      <span className="doc-date"><Clock size={14} />{dayFormat(doc.created_at)}</span>
      <div className="doc-card-actions">
        {processing ? <Button className="sq-btn-muted" onClick={onOpen}>В процессе…</Button> : failed ? <>
          <Button className="sq-btn-muted" icon={<RotateCcw size={15} />} onClick={onRetry}>Повторить анализ</Button>
          <Tooltip title="Удалить"><button type="button" className="icon-square" onClick={onDelete} aria-label="Удалить"><Trash2 size={16} /></button></Tooltip>
        </> : <>
          <Tooltip title="Удалить"><button type="button" className="icon-square" onClick={onDelete} aria-label="Удалить"><Trash2 size={16} /></button></Tooltip>
          <Button type="primary" className="sq-btn" onClick={onOpen}>Открыть <ArrowRight size={15} /></Button>
        </>}
      </div>
    </div>
  </article>;
}

function FailedUploadCard({ item, onReplace, onDismiss }) {
  const input = useRef(null);
  return <article className="doc-card">
    <div className="doc-card-top">
      <FileTile genre={item.meta?.genre} source={item.name.split('.').pop()?.toLowerCase()} />
      <div className="doc-card-info">
        <div className="doc-card-tags"><span className={`sq-tag tone-${kindOf(item.meta?.genre).tone}`}>{item.meta?.genre || 'Файл'}</span><StatusTag status="error" /></div>
        <h3>{item.name.replace(/\.[^.]+$/, '')}</h3>
        <p>Файл не загружен</p>
      </div>
    </div>
    <div className="card-error"><AlertTriangle size={18} /><span>{item.error}</span></div>
    <div className="doc-card-footer">
      <span className="doc-date"><Clock size={14} />{dayFormat(item.at)}</span>
      <div className="doc-card-actions">
        <input ref={input} type="file" accept=".txt,.pdf,.epub" hidden onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) onReplace(f); }} />
        <Button className="sq-btn-muted" onClick={() => input.current?.click()}>Перезагрузить</Button>
        <Tooltip title="Убрать"><button type="button" className="icon-square" onClick={onDismiss} aria-label="Убрать"><Trash2 size={16} /></button></Tooltip>
      </div>
    </div>
  </article>;
}

export function LibraryScreen({ documents, failed, filters, setFilters, onAdd, onOpen, onDelete, onEdit, onRetry, onReplaceFailed, onDismissFailed, onFiles, query }) {
  const authors = useMemo(() => [...new Set(documents.map(d => d.author).filter(Boolean))].sort(), [documents]);
  const subgenres = useMemo(() => [...new Set(documents.map(d => d.subgenre).filter(Boolean))].sort(), [documents]);
  const kinds = useMemo(() => [...new Set(documents.map(d => d.genre).filter(Boolean))], [documents]);
  const shown = documents.filter(d => {
    if (filters.author && d.author !== filters.author) return false;
    if (filters.subgenre && d.subgenre !== filters.subgenre) return false;
    if (filters.genre && d.genre !== filters.genre) return false;
    if (filters.period) {
      const days = PERIODS.find(p => p.key === filters.period)?.days || 0;
      if (Date.now() - new Date(d.created_at).getTime() > days * 86400000) return false;
    }
    return true;
  });
  const active = Object.values(filters).some(Boolean);
  return <div className="screen library-screen">
    <div className="screen-heading">
      <div><h1>Моя библиотека</h1><p>Управляйте вашими текстами и результатами семантического анализа</p></div>
      <Button type="primary" size="large" className="sq-btn sq-btn-lg" icon={<Plus size={18} />} onClick={onAdd}>Добавить текст</Button>
    </div>
    {!!documents.length && <div className="filter-row">
      <FilterButton icon={User} label="Автор" value={filters.author} options={authors} onChange={v => setFilters(f => ({ ...f, author: v }))} />
      <FilterButton icon={Tag} label="Жанр" value={filters.subgenre} options={subgenres} onChange={v => setFilters(f => ({ ...f, subgenre: v }))} />
      <FilterButton icon={BookOpen} label="Тип" value={filters.genre} options={kinds} onChange={v => setFilters(f => ({ ...f, genre: v }))} />
      <FilterButton icon={Calendar} label="Дата" value={filters.period} options={PERIODS} onChange={v => setFilters(f => ({ ...f, period: v }))} />
      {active && <><span className="filter-divider" /><button type="button" className="link-button" onClick={() => setFilters({})}>Сбросить всё</button></>}
    </div>}
    {!documents.length && !failed.length ? <EmptyLibrary onFiles={onFiles} onAdd={onAdd} query={query} /> : <div className="card-grid">
      {failed.map(item => <FailedUploadCard key={item.id} item={item} onReplace={f => onReplaceFailed(item, f)} onDismiss={() => onDismissFailed(item.id)} />)}
      {shown.map(d => <DocumentCard key={d.id} doc={d} onOpen={() => onOpen(d)} onDelete={() => onDelete(d)} onEdit={() => onEdit(d)} onRetry={() => onRetry(d)} />)}
      {!shown.length && !failed.length && <div className="grid-empty">Под выбранные фильтры ничего не подходит. <button type="button" className="link-button" onClick={() => setFilters({})}>Сбросить фильтры</button></div>}
    </div>}
  </div>;
}

function EmptyLibrary({ onFiles, onAdd, query }) {
  if (query) return <div className="grid-empty">По запросу «{query}» ничего не найдено.</div>;
  return <DropCard onFiles={onFiles} multiple title="Перетащите книгу сюда"
    text="Атлас разберёт текст на главы, найдёт персонажей, места и события, покажет, кто с кем связан, и подготовит аналитику."
    extra={<Button className="sq-btn-outline" onClick={e => { e.stopPropagation(); onAdd(); }}>Вставить текст вручную</Button>} />;
}

function DropCard({ onFiles, multiple = false, title, text, file, onClear, extra }) {
  const input = useRef(null);
  const [over, setOver] = useState(false);
  const pick = () => input.current?.click();
  return <div className={`drop-card ${over ? 'over' : ''}`} role="button" tabIndex={0} onClick={pick}
    onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pick(); } }}
    onDragOver={e => { e.preventDefault(); setOver(true); }} onDragLeave={() => setOver(false)} onDrop={() => setOver(false)}
    aria-label={`${title}: перетащите файл или нажмите, чтобы выбрать`}>
    <input ref={input} type="file" accept=".txt,.pdf,.epub" multiple={multiple} hidden
      onChange={e => { const files = Array.from(e.target.files || []); e.target.value = ''; if (files.length) onFiles(files); }} />
    <div className="drop-card-inner">
      {file ? <div className="chosen-file" onClick={e => e.stopPropagation()}>
        <FileTile genre="" source={file.name.split('.').pop()?.toLowerCase()} />
        <div><strong>{file.name}</strong><span>{(file.size / 1024 / 1024).toFixed(2)} МБ · будет проанализирован этот файл</span></div>
        <button type="button" className="icon-square" onClick={onClear} aria-label="Убрать файл"><X size={16} /></button>
      </div> : <>
        <div className="drop-icon"><UploadIcon size={26} /></div>
        <h2>{title}</h2>
        <p>{text}</p>
        <div className="format-chips"><span className="tone-blue">PDF</span><span className="tone-green">TXT</span><span className="tone-purple">EPUB</span></div>
        {extra && <div className="drop-extra">{extra}</div>}
      </>}
    </div>
  </div>;
}

const DRAFT_KEY = 'semantiq-draft';
const readDraft = () => { try { return JSON.parse(localStorage.getItem(DRAFT_KEY)) || {}; } catch { return {}; } };
const SUBGENRES = ['Роман', 'Повесть', 'Рассказ', 'Пьеса', 'Поэзия', 'Учебник', 'Статья', 'Монография'].map(value => ({ value }));

export function AddTextScreen({ onSubmit, onCancel, busy, registerDrop }) {
  const draft = useMemo(readDraft, []);
  const [meta, setMeta] = useState({ title: '', author: '', year: null, subgenre: '', genre: 'Художественный', depth: 'full', ...draft.meta });
  const [text, setText] = useState(draft.text || '');
  const [file, setFile] = useState(null);
  const [error, setError] = useState('');
  const set = (key, value) => setMeta(m => ({ ...m, [key]: value }));
  const choose = files => {
    const f = files[0], problem = fileProblem(f);
    if (problem) { setError(problem); return; }
    setError(''); setFile(f);
    if (!meta.title.trim()) set('title', f.name.replace(/\.[^.]+$/, ''));
  };
  const chooseRef = useRef(choose);
  chooseRef.current = choose;
  useEffect(() => registerDrop(files => chooseRef.current(files)), [registerDrop]);
  useEffect(() => {
    try { localStorage.setItem(DRAFT_KEY, JSON.stringify({ meta, text })); } catch { /* Drafts are optional. */ }
  }, [meta, text]);
  async function paste() {
    try { const value = await navigator.clipboard.readText(); if (value) setText(t => t ? `${t}\n${value}` : value); }
    catch { setError('Браузер не дал доступ к буферу обмена. Вставьте текст сочетанием Ctrl+V (⌘V).'); }
  }
  async function submit() {
    setError('');
    if (!file && !text.trim()) { setError('Загрузите файл или вставьте текст.'); return; }
    if (!file && !meta.title.trim()) { setError('Укажите название текста.'); return; }
    try {
      await onSubmit({ file, text, meta });
      try { localStorage.removeItem(DRAFT_KEY); } catch { /* ignore */ }
    } catch (e) { setError(e.message); }
  }
  const kinds = [['Художественный'], ['Учебный'], ['Научный']];
  return <div className="screen add-screen">
    <div className="add-grid">
      <div className="add-side">
        <section className="sq-card form-card">
          <h4 className="card-label">Информация</h4>
          <label className="field"><span>Название</span><Input value={meta.title} onChange={e => set('title', e.target.value)} placeholder="Введите название…" maxLength={250} /></label>
          <label className="field"><span>Автор</span><Input value={meta.author} onChange={e => set('author', e.target.value)} placeholder="Имя автора…" maxLength={180} /></label>
          <div className="field-row">
            <label className="field"><span>Год</span><InputNumber value={meta.year} onChange={v => set('year', v)} placeholder="2024" min={1} max={2200} controls={false} /></label>
            <label className="field"><span>Жанр</span><AutoComplete value={meta.subgenre} onChange={v => set('subgenre', v)} options={SUBGENRES} placeholder="Роман" maxLength={80}
              filterOption={(input, option) => option.value.toLowerCase().includes(input.toLowerCase())} /></label>
          </div>
          <div className="field"><span>Тип текста</span>
            <div className="radio-cards" role="radiogroup" aria-label="Тип текста">
              {kinds.map(([value]) => <button type="button" role="radio" aria-checked={meta.genre === value} key={value}
                className={`radio-card ${meta.genre === value ? 'checked' : ''}`} onClick={() => set('genre', value)}>
                <span className="radio-dot" />{value}
              </button>)}
            </div>
          </div>
        </section>
        <section className="sq-card depth-card">
          <h4 className="card-label">Глубина анализа</h4>
          {[['basic', 'Базовый', 'Основные сущности и простые связи между ними (до 30 узлов).'],
            ['full', 'Подробный', 'Все персонажи, места, события и понятия: до 70 узлов и 220 связей.']].map(([value, title, hint]) =>
            <button type="button" key={value} className={`depth-option ${meta.depth === value ? 'checked' : ''}`} onClick={() => set('depth', value)} aria-pressed={meta.depth === value}>
              <strong>{title}</strong><span>{hint}</span>
            </button>)}
        </section>
      </div>
      <div className="add-main">
        <DropCard onFiles={choose} title="Загрузите файл" file={file} onClear={() => setFile(null)}
          text="Перетащите документ сюда или кликните для выбора на компьютере. Поддерживаются PDF, TXT, EPUB до 12 МБ." />
        <section className={`sq-card manual-card ${file ? 'dimmed' : ''}`}>
          <div className="manual-head">
            <div><h4 className="card-label">Ручной ввод текста</h4><p>Просто вставьте текст из буфера обмена ниже</p></div>
            <div className="manual-actions">
              <button type="button" onClick={() => setText('')}><Trash2 size={14} />Очистить</button>
              <button type="button" onClick={paste}><ClipboardPaste size={14} />Вставить</button>
            </div>
          </div>
          <TextArea value={text} onChange={e => setText(e.target.value)} placeholder={file ? 'Выбран файл — этот текст не будет использован.' : 'Начните печатать или вставьте текст здесь…'}
            autoSize={{ minRows: 9, maxRows: 18 }} aria-label="Текст для анализа" />
          <div className="manual-foot"><span>Символов: {new Intl.NumberFormat('ru-RU').format(text.length)}</span><em>Автосохранение включено</em></div>
        </section>
        {error && <div className="card-error form-error"><AlertTriangle size={18} /><span>{error}</span></div>}
        <div className="add-actions">
          <Button size="large" className="sq-btn-outline" onClick={onCancel} disabled={busy}>Отмена</Button>
          <Button size="large" type="primary" className="sq-btn sq-btn-lg" loading={busy} onClick={submit}>Начать анализ <ArrowRight size={17} /></Button>
        </div>
      </div>
    </div>
  </div>;
}

export function ProgressScreen({ id, onDone, onBack, onRetry, onStatus }) {
  const [status, setStatus] = useState(null);
  const [shown, setShown] = useState(0);
  const [error, setError] = useState('');
  const [round, setRound] = useState(0);
  const doneCalled = useRef(false);
  useEffect(() => {
    let alive = true, timer;
    const poll = async () => {
      try {
        const s = await api(`/documents/${id}/status`);
        if (!alive) return;
        setStatus(s); onStatus?.(s); setError('');
        if (s.status === 'processing') timer = setTimeout(poll, 450);
      } catch (e) { if (alive) { setError(e.message); timer = setTimeout(poll, 1500); } }
    };
    poll();
    return () => { alive = false; clearTimeout(timer); };
  }, [id, round]);
  const actual = status ? stageIndex(status.status === 'ready' ? 'done' : status.stage) : 0;
  // Steps advance one at a time so short analyses still show every real stage.
  useEffect(() => {
    if (shown >= actual) return undefined;
    const t = setTimeout(() => setShown(v => v + 1), 420);
    return () => clearTimeout(t);
  }, [shown, actual]);
  useEffect(() => {
    if (status?.status === 'ready' && shown >= STAGES.length && !doneCalled.current) {
      doneCalled.current = true;
      const t = setTimeout(() => onDone(id), 650);
      return () => clearTimeout(t);
    }
    return undefined;
  }, [status, shown, id, onDone]);
  const failed = status?.status === 'error';
  const percent = shown >= STAGES.length ? 100 : STAGE_PROGRESS[STAGES[shown].key];
  const hint = failed ? (status.error || 'Не удалось проанализировать текст.') : shown >= STAGES.length ? 'Анализ завершён. Открываем граф…' : STAGES[shown].hint;
  return <div className="screen progress-screen">
    <section className="sq-card progress-card">
      <div className={`progress-icon ${failed ? 'failed' : ''}`}>
        {failed ? <AlertTriangle size={34} /> : <Network size={34} />}
        <span className="progress-icon-badge">{failed ? <X size={12} /> : <Cog size={12} className={shown < STAGES.length ? 'spin-slow' : ''} />}</span>
      </div>
      <h1>{failed ? 'Анализ не удался' : 'Обработка семантики'}</h1>
      <p className="progress-hint" aria-live="polite">{hint}</p>
      {error && <p className="progress-warning">{error}</p>}
      <div className="progress-meter">
        <div className="progress-meter-head"><span>Прогресс анализа</span><strong>{percent}%</strong></div>
        <div className={`meter ${failed ? 'failed' : ''}`}><span style={{ width: `${percent}%` }} /></div>
      </div>
      <ol className="steps">
        {STAGES.map((s, i) => {
          const state = failed && i === shown ? 'failed' : i < shown ? 'done' : i === shown ? 'active' : 'pending';
          return <li key={s.key} className={`step ${state}`}>
            <span className="step-dot">{state === 'done' ? <Check size={16} /> : state === 'active' ? <Loader2 size={16} className="spin" /> : state === 'failed' ? <X size={16} /> : <s.Icon size={15} />}</span>
            <span className="step-label">{s.label}</span>
          </li>;
        })}
      </ol>
      {failed && <Button type="primary" className="sq-btn" icon={<RotateCcw size={15} />} onClick={async () => {
        try { await onRetry(id); doneCalled.current = false; setShown(0); setStatus(null); setRound(r => r + 1); } catch { /* error shown */ }
      }}>Повторить анализ</Button>}
    </section>
    <Button size="large" className="sq-btn-outline back-button" icon={<ArrowLeft size={17} />} onClick={onBack}>Вернуться в библиотеку</Button>
    {status?.note && !failed && <aside className="progress-toast">
      <span className="toast-icon"><Zap size={16} /></span>
      <div><strong>Ход анализа</strong><span>{status.note}</span><div className="toast-bar"><span style={{ width: `${percent}%` }} /></div></div>
    </aside>}
  </div>;
}
