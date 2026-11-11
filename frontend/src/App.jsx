import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Avatar, Button, Checkbox, Drawer, Input, Space, Tabs, Tooltip, Typography } from 'antd';
import {
  ArrowLeft, BarChart3, BookOpen, ChevronRight, Download, FilePlus2, FileText, HelpCircle, LogOut, Menu,
  MessageSquare, Network, PenLine, Pencil, Search, Trash2, Upload as UploadIcon, UserCog,
} from 'lucide-react';
import { createRequestGuard } from './requestGuard.js';
import { fileProblem, hasFiles } from './files.js';
import {
  api, Analytics, Auth, Brand, Busy, DeleteModal, ErrorBox, Graph, icon, initialEntity, defaultFilters, Inspector,
  MetaModal, MethodModal, Modal, Notes, num, ProfileModal, Reader, TYPES,
} from './workspace.jsx';
import { AddTextScreen, LibraryScreen, ProgressScreen } from './screens.jsx';

const { Text, Title, Paragraph } = Typography;

function DropOverlay() {
  return <div className="drop-overlay" role="presentation">
    <div className="drop-overlay-box">
      <UploadIcon size={40} />
      <Title level={3}>Отпустите, чтобы проанализировать</Title>
      <Text type="secondary">TXT, EPUB или PDF с текстовым слоем, до 12 МБ. Можно несколько книг сразу.</Text>
    </div>
  </div>;
}

function UserBlock({ user, onClick }) {
  return <button type="button" className="user-block" onClick={onClick} aria-label="Профиль">
    <span className="user-text"><strong>{user.name}</strong><em>@{user.login}</em></span>
    <Avatar size={40} src={user.avatar || undefined} className="user-avatar">{user.name?.slice(0, 1).toUpperCase()}</Avatar>
  </button>;
}

function Sidebar({ active, onNavigate, onProfile, onLogout }) {
  const item = (key, label, Icon) => <button type="button" key={key} className={`nav-item ${active === key ? 'active' : ''}`}
    onClick={() => onNavigate(key)} aria-current={active === key ? 'page' : undefined}><Icon size={18} /><span>{label}</span></button>;
  return <div className="sq-sidebar-inner">
    <div className="sq-logo"><Brand /></div>
    <nav className="sq-nav" aria-label="Разделы">
      <div className="nav-label">Основное</div>
      {item('library', 'Библиотека', BookOpen)}
      {item('add', 'Добавить текст', FilePlus2)}
      {item('graph', 'Анализ связей', Network)}
      {item('analytics', 'Аналитика', BarChart3)}
      <div className="nav-label">Инструменты</div>
      {item('questions', 'Вопросы к тексту', MessageSquare)}
      {item('notes', 'Заметки', PenLine)}
    </nav>
    <div className="sq-nav-bottom">
      <button type="button" className="nav-item" onClick={onProfile}><UserCog size={18} /><span>Профиль</span></button>
      <button type="button" className="nav-item danger" onClick={onLogout}><LogOut size={18} /><span>Выход</span></button>
    </div>
  </div>;
}

const firstName = name => (name || '').trim().split(/\s+/)[0] || 'читатель';

export default function App() {
  const [user, setUser] = useState(null), [checking, setChecking] = useState(true);
  const [documents, setDocuments] = useState([]), [doc, setDoc] = useState(null), [docBusy, setDocBusy] = useState(false);
  const [view, setView] = useState('library'), [progressId, setProgressId] = useState(null), [progressTitle, setProgressTitle] = useState('');
  const [error, setError] = useState(''), [toast, setToast] = useState('');
  const [query, setQuery] = useState(''), [libFilters, setLibFilters] = useState({}), [failed, setFailed] = useState([]);
  const [tab, setTab] = useState('graph'), [modal, setModal] = useState(null), [target, setTarget] = useState(null);
  const [selectedId, setSelectedId] = useState(null), [edge, setEdge] = useState(null);
  const [filters, setFilters] = useState(Object.keys(TYPES)), [entitySearch, setEntitySearch] = useState('');
  const [inspector, setInspector] = useState(() => window.innerWidth > 1100), [inspectorMode, setInspectorMode] = useState('entity');
  const [notes, setNotes] = useState([]), [questions, setQuestions] = useState([]);
  const [fragment, setFragment] = useState(null), [noteEntity, setNoteEntity] = useState(''), [exportBusy, setExportBusy] = useState('');
  const [mobile, setMobile] = useState(() => window.innerWidth <= 900), [narrow, setNarrow] = useState(() => window.innerWidth <= 1100);
  const [menuOpen, setMenuOpen] = useState(false), [submitting, setSubmitting] = useState(false);
  const [dragging, setDragging] = useState(false), [aiStatus, setAiStatus] = useState(null);
  const dragDepth = useRef(0), addDropRef = useRef(null);

  useEffect(() => {
    const resize = () => { setMobile(window.innerWidth <= 900); setNarrow(window.innerWidth <= 1100); };
    window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  }, []);
  useEffect(() => { if (narrow) setInspector(false); }, [narrow]);

  const guardRef = useRef(null); if (!guardRef.current) guardRef.current = createRequestGuard();
  const requestGuard = guardRef.current, { documentEpoch: docRequest, sessionEpoch, listEpoch: listRequest, sessionValid, documentValid } = requestGuard;
  const flash = t => { setToast(t); const epoch = sessionEpoch.current; setTimeout(() => { if (sessionValid(epoch)) setToast(''); }, 3800); };

  function clearWorkspace() {
    setDoc(null); setDocuments([]); setNotes([]); setQuestions([]); setSelectedId(null); setEdge(null); setFragment(null);
    setNoteEntity(''); setModal(null); setTarget(null); setDocBusy(false); setExportBusy(''); setToast(''); setError(''); setQuery('');
    setTab('graph'); setInspectorMode('entity'); setView('library'); setProgressId(null); setFailed([]); setLibFilters({});
  }

  useEffect(() => {
    const epoch = sessionEpoch.current, c = new AbortController();
    api('/auth/me', { signal: c.signal }).then(u => { if (sessionValid(epoch)) setUser(u); }).catch(() => {})
      .finally(() => { if (!c.signal.aborted && sessionValid(epoch)) setChecking(false); });
    return () => c.abort();
  }, []);

  const loadDocuments = useCallback(async () => {
    const epoch = sessionEpoch.current, req = ++listRequest.current;
    try {
      const r = await api(`/documents?q=${encodeURIComponent(query)}&sort=created`);
      if (sessionValid(epoch) && req === listRequest.current) setDocuments(r);
      return r;
    } catch (e) { if (sessionValid(epoch) && req === listRequest.current) setError(e.message); return []; }
  }, [query]);
  useEffect(() => { if (!user) return undefined; const t = setTimeout(loadDocuments, 250); return () => clearTimeout(t); }, [user, query, loadDocuments]);
  // Whether the local model (Qwen via Ollama) can be used; refreshed when adding a text.
  useEffect(() => {
    if (!user) { setAiStatus(null); return; }
    api('/ai').then(setAiStatus).catch(() => setAiStatus(null));
  }, [user, view]);
  // While something is being analysed, refresh the library so cards show live progress.
  const anyProcessing = documents.some(d => d.status === 'processing');
  useEffect(() => {
    if (!user || !anyProcessing) return undefined;
    const t = setInterval(loadDocuments, 1200);
    return () => clearInterval(t);
  }, [user, anyProcessing, loadDocuments]);

  async function openDoc(id, nextTab = 'graph') {
    const summary = documents.find(d => d.id === id);
    if (summary?.status === 'processing') { showProgress(summary); return; }
    const req = ++docRequest.current, epoch = sessionEpoch.current;
    setDocBusy(true); setError(''); setMenuOpen(false); setView('document');
    try {
      await requestGuard.runDocument(epoch, req,
        () => Promise.all([api(`/documents/${id}`), api(`/documents/${id}/notes`), api(`/documents/${id}/questions`)]),
        ([d, n, q]) => {
          if (d.status === 'processing') { showProgress(d); return; }
          setDoc(d); setNotes(n); setQuestions([...q].reverse()); setSelectedId(initialEntity(d.analysis)); setEdge(null);
          setTab(nextTab); setFilters(defaultFilters(d.analysis)); setEntitySearch(''); setFragment(null); setNoteEntity('');
          setInspectorMode(nextTab === 'questions' ? 'questions' : 'entity');
          setInspector(nextTab === 'questions' || window.innerWidth > 1100);
        });
    } catch (e) { if (documentValid(epoch, req)) { setError(e.message); setView('library'); } }
    finally { if (documentValid(epoch, req)) setDocBusy(false); }
  }

  function showProgress(summary) {
    docRequest.current++;
    setProgressId(summary.id); setProgressTitle(summary.title); setView('progress'); setMenuOpen(false); setDocBusy(false);
  }

  function onAuth(u, id) {
    sessionEpoch.current++; docRequest.current++; listRequest.current++; clearWorkspace(); setUser(u); setChecking(false);
    if (id) openDoc(id);
  }

  async function logout() {
    const epoch = ++sessionEpoch.current; docRequest.current++; listRequest.current++; clearWorkspace(); setUser(null); setChecking(true);
    try { await api('/auth/logout', { method: 'POST' }); } catch (e) { if (sessionValid(epoch)) setError(e.message); }
    finally { if (sessionValid(epoch)) setChecking(false); }
  }

  function uploadBody(file, meta = {}) {
    const body = new FormData();
    body.append('file', file);
    for (const key of ['title', 'author', 'genre', 'subgenre', 'depth']) if (meta[key]) body.append(key, meta[key]);
    if (meta.ai) body.append('ai', '1');
    if (meta.year) body.append('year', String(meta.year));
    return body;
  }

  // Add-text screen: one file or pasted text with metadata, then the progress screen.
  async function submitAdd({ file, text, meta }) {
    setSubmitting(true);
    try {
      const created = file ? await api('/documents/upload', { method: 'POST', body: uploadBody(file, meta) })
        : await api('/documents', { method: 'POST', body: { ...meta, year: meta.year || null, content: text } });
      await loadDocuments();
      showProgress(created);
    } finally { setSubmitting(false); }
  }

  // Dropped files: each is uploaded; analysis then continues in the background.
  async function uploadFiles(files) {
    const epoch = sessionEpoch.current;
    const problems = files.filter(fileProblem).map(f => ({ id: crypto.randomUUID(), name: f.name, error: fileProblem(f).replace(/^«[^»]*»: /, ''), file: f, meta: {}, at: new Date().toISOString() }));
    const valid = files.filter(f => !fileProblem(f));
    const created = [], failures = [...problems];
    if (valid.length) flash(valid.length > 1 ? `Загружаем книг: ${valid.length}…` : `Загружаем «${valid[0].name}»…`);
    for (const file of valid) {
      try { created.push(await api('/documents/upload', { method: 'POST', body: uploadBody(file, { genre: 'Художественный' }) })); }
      catch (e) { failures.push({ id: crypto.randomUUID(), name: file.name, error: e.message, file, meta: {}, at: new Date().toISOString() }); }
    }
    if (!sessionValid(epoch)) return;
    if (failures.length) setFailed(f => [...failures, ...f]);
    await loadDocuments();
    if (created.length === 1 && !failures.length) showProgress(created[0]);
    else {
      setView('library'); docRequest.current++;
      if (created.length) flash(`Добавлено книг: ${created.length}. Анализ идёт в фоне.`);
      else if (failures.length) setToast('');
    }
  }

  async function replaceFailed(item, file) {
    const problem = fileProblem(file);
    if (problem) { setFailed(list => list.map(x => x.id === item.id ? { ...x, error: problem } : x)); return; }
    try {
      const created = await api('/documents/upload', { method: 'POST', body: uploadBody(file, item.meta) });
      setFailed(list => list.filter(x => x.id !== item.id));
      await loadDocuments(); showProgress(created);
    } catch (e) { setFailed(list => list.map(x => x.id === item.id ? { ...x, name: file.name, file, error: e.message } : x)); }
  }

  async function retryAnalysis(id, body) {
    try { await api(`/documents/${id}/analyze`, { method: 'POST', body }); await loadDocuments(); }
    catch (e) { setError(e.message); throw e; }
  }

  const uploadRef = useRef(uploadFiles); uploadRef.current = uploadFiles;
  const viewRef = useRef(view); viewRef.current = view;
  // Window-wide drop target: books can be dropped anywhere once signed in.
  useEffect(() => {
    if (!user || modal) { dragDepth.current = 0; setDragging(false); return undefined; }
    const enter = e => { if (!hasFiles(e)) return; e.preventDefault(); dragDepth.current++; setDragging(true); };
    const over = e => { if (hasFiles(e)) { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; } };
    const leave = e => { if (!hasFiles(e)) return; dragDepth.current = Math.max(0, dragDepth.current - 1); if (!dragDepth.current) setDragging(false); };
    const drop = e => {
      if (!hasFiles(e)) return;
      e.preventDefault(); dragDepth.current = 0; setDragging(false);
      const files = Array.from(e.dataTransfer.files || []);
      if (!files.length) return;
      if (viewRef.current === 'add' && files.length === 1 && addDropRef.current) addDropRef.current(files);
      else uploadRef.current(files);
    };
    window.addEventListener('dragenter', enter); window.addEventListener('dragover', over);
    window.addEventListener('dragleave', leave); window.addEventListener('drop', drop);
    return () => {
      window.removeEventListener('dragenter', enter); window.removeEventListener('dragover', over);
      window.removeEventListener('dragleave', leave); window.removeEventListener('drop', drop);
    };
  }, [user, modal]);
  const registerAddDrop = useCallback(fn => { addDropRef.current = fn; return () => { addDropRef.current = null; }; }, []);

  function navigate(key) {
    setMenuOpen(false); setError('');
    if (key === 'library' || key === 'add') { docRequest.current++; setDocBusy(false); setView(key); return; }
    const docTab = key === 'questions' ? 'graph' : key;
    const open = () => {
      setView('document'); setTab(docTab);
      if (key === 'questions') { setInspectorMode('questions'); setInspector(true); }
    };
    if (doc) { open(); return; }
    const latest = documents.find(d => d.status === 'ready');
    if (latest) openDoc(latest.id, key);
    else { flash('Сначала добавьте текст: после анализа здесь появятся граф и аналитика.'); setView('add'); }
  }

  function selectEntity(id) {
    const n = doc?.analysis.entities.find(e => e.id === id);
    if (n && !filters.includes(n.type)) setFilters(f => [...new Set([...f, n.type])]);
    setSelectedId(id); setEdge(null); setInspector(true); setInspectorMode('entity');
  }
  async function addNote(body) {
    const epoch = sessionEpoch.current, req = docRequest.current, id = doc.id;
    await requestGuard.runDocument(epoch, req, () => api(`/documents/${id}/notes`, { method: 'POST', body }), n => { setNotes(v => [n, ...v]); flash('Заметка сохранена'); });
  }
  async function deleteNote(noteId) {
    const epoch = sessionEpoch.current, req = docRequest.current, id = doc.id;
    await requestGuard.runDocument(epoch, req, () => api(`/documents/${id}/notes/${noteId}`, { method: 'DELETE' }), () => { setNotes(v => v.filter(n => n.id !== noteId)); flash('Заметка удалена'); });
  }
  async function ask(question) {
    const epoch = sessionEpoch.current, req = docRequest.current, id = doc.id;
    await requestGuard.runDocument(epoch, req, () => api(`/documents/${id}/questions`, { method: 'POST', body: { question } }), q => setQuestions(v => [...v, q]));
  }
  async function exportDoc(type) {
    const epoch = sessionEpoch.current, req = docRequest.current, id = doc.id, title = doc.title;
    setExportBusy(type); setError('');
    try {
      const r = await fetch(`/api/documents/${id}/export.${type}`, { credentials: 'same-origin' });
      if (!r.ok) { const e = await r.json(); throw new Error(e.detail || 'Не удалось экспортировать файл.'); }
      const blob = await r.blob();
      if (!documentValid(epoch, req)) return;
      const url = URL.createObjectURL(blob), a = document.createElement('a');
      a.href = url; a.download = `${title.replace(/[\\/:*?"<>|]+/g, '_').slice(0, 80) || 'semantiq'}.${type}`;
      document.body.appendChild(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000);
      flash(type === 'pdf' ? 'Отчёт PDF сохранён' : 'Граф PNG сохранён');
    } catch (e) { if (documentValid(epoch, req)) setError(e.message); }
    finally { if (documentValid(epoch, req)) setExportBusy(''); }
  }

  if (checking) return <div className="boot"><Brand /><Busy text="Загрузка приложения…" /></div>;
  if (!user) return <Auth onAuth={onAuth} />;

  const progressDoc = documents.find(d => d.id === progressId);
  const activeNav = view === 'document' ? (inspector && inspectorMode === 'questions' ? 'questions' : tab === 'reader' ? 'graph' : tab) : view === 'progress' ? 'add' : view;
  const sidebar = <Sidebar active={activeNav} onNavigate={navigate} onProfile={() => { setMenuOpen(false); setModal('profile'); }} onLogout={logout} />;
  const back = <button type="button" className="back-arrow" onClick={() => navigate('library')} aria-label="Назад в библиотеку"><ArrowLeft size={20} /></button>;
  const statusBadge = s => s === 'ready' ? <span className="head-badge tone-green">Готово</span> : s === 'error' ? <span className="head-badge tone-red">Ошибка</span> : <span className="head-badge">В процессе</span>;

  const header = <header className="sq-header">
    {mobile && <button type="button" className="back-arrow" onClick={() => setMenuOpen(true)} aria-label="Открыть меню"><Menu size={20} /></button>}
    {view === 'library' && <>
      <h2 className="head-title">Привет, {firstName(user.name)}! 👋</h2>
      <Input className="head-search" allowClear prefix={<Search size={16} />} placeholder="Поиск по библиотеке…" aria-label="Поиск по библиотеке"
        value={query} onChange={e => setQuery(e.target.value)} />
    </>}
    {view === 'add' && <>{back}<h2 className="head-title">Добавить новый текст</h2></>}
    {view === 'progress' && <><h2 className="head-title ellipsis">Анализ текста: {progressDoc?.title || progressTitle}</h2>{statusBadge(progressDoc?.status === 'error' ? 'error' : 'processing')}</>}
    {view === 'document' && <>{back}<h2 className="head-title ellipsis">{doc?.title || 'Текст'}</h2></>}
    <div className="head-spacer" />
    <Tooltip title="Как работает анализ"><button type="button" className="icon-square head-help" onClick={() => setModal('method')} aria-label="Как работает анализ"><HelpCircle size={18} /></button></Tooltip>
    <UserBlock user={user} onClick={() => setModal('profile')} />
  </header>;

  const inspectorPanel = doc && inspector ? <Inspector key={doc.id} doc={doc} selectedId={selectedId} edge={edge}
    onSelect={selectEntity} onClose={() => setInspector(false)} onNote={id => { setNoteEntity(id); setTab('notes'); setFragment(null); if (narrow) setInspector(false); }}
    questions={questions} onAsk={ask} mode={inspectorMode} setMode={setInspectorMode} ai={aiStatus?.available ? aiStatus.model : ''} /> : null;

  const workspace = docBusy || !doc ? <Busy text="Загрузка текста и результатов анализа…" /> : <div className="screen doc-screen">
    <div className="document-heading">
      <div><Title level={3}>{doc.title}</Title><Space split={<span>·</span>} size={8} wrap>
        <Text type="secondary">{doc.author || 'Автор не указан'}</Text>{doc.year && <Text type="secondary">{doc.year}</Text>}
        <Text type="secondary">{doc.genre || 'Текст'}{doc.subgenre ? `, ${doc.subgenre.toLowerCase()}` : ''}</Text>
        <Text type="secondary">{num(doc.word_count)} слов</Text>
        <Text type="secondary">{doc.depth === 'basic' ? 'Базовый анализ' : 'Подробный анализ'}</Text>
      </Space></div>
      <Space className="document-actions" wrap>
        <Tooltip title="Изменить сведения"><Button icon={icon(Pencil)} aria-label="Изменить сведения о тексте" onClick={() => { setTarget(doc); setModal('meta'); }} /></Tooltip>
        <Tooltip title="Удалить текст"><Button danger icon={icon(Trash2)} aria-label="Удалить текст" onClick={() => { setTarget(doc); setModal('delete'); }} /></Tooltip>
        <Button icon={icon(Download)} loading={!!exportBusy} onClick={() => setModal('export')}>Экспорт</Button>
      </Space>
    </div>
    <div className="document-navigation"><Tabs activeKey={tab} onChange={setTab} items={[
      { key: 'graph', label: 'Граф' }, { key: 'reader', label: 'Текст' },
      { key: 'analytics', label: 'Аналитика' }, { key: 'notes', label: notes.length ? `Заметки (${notes.length})` : 'Заметки' },
    ]} /><Button aria-label="Вопрос по тексту" type={inspector && inspectorMode === 'questions' ? 'primary' : 'default'} icon={icon(MessageSquare)}
      onClick={() => { setInspectorMode('questions'); setInspector(value => inspectorMode === 'questions' ? !value : true); }}>
      <span className="question-button-label">Вопрос по тексту</span>
    </Button></div>
    <div className="analysis-notice"><Alert type="info" showIcon title="Связи определены автоматически. Проверяйте предположения по цитатам." /></div>
    <div className={`research-layout ${inspector && !narrow ? 'with-inspector' : ''}`}>
      <section className="research-main">
        {tab === 'graph' ? <>
          <div className="graph-toolbar"><div className="graph-filters">
            {Object.entries(TYPES).map(([type, t]) => <Checkbox key={type} checked={filters.includes(type)} onChange={() =>
              setFilters(values => values.includes(type) ? values.filter(x => x !== type) : [...values, type])}>
              <span className="filter-dot" style={{ background: t.color }} />{t.label} <Text type="secondary">({doc.analysis.entities.filter(n => n.type === type).length})</Text>
            </Checkbox>)}
          </div><Input allowClear prefix={icon(Search, 15)} placeholder="Поиск сущностей" aria-label="Поиск сущностей"
            value={entitySearch} onChange={e => { setEntitySearch(e.target.value); if (e.target.value.trim()) setFilters(Object.keys(TYPES)); }} />
          </div>
          <Graph analysis={doc.analysis} selected={selectedId} onSelect={selectEntity} filters={filters} search={entitySearch}
            onEdge={value => { setEdge({ ...value, source: typeof value.source === 'object' ? value.source.id : value.source,
              target: typeof value.target === 'object' ? value.target.id : value.target }); setInspector(true); setInspectorMode('entity'); }} />
        </> : tab === 'reader' ? <Reader key={doc.id} doc={doc} onFragment={value => { setFragment(value); setNoteEntity(''); setTab('notes'); if (narrow) setInspector(false); }} />
          : tab === 'analytics' ? <Analytics doc={doc} onEntity={id => { selectEntity(id); setTab('graph'); }} />
            : <Notes key={doc.id} notes={notes} entities={doc.analysis.entities} onAdd={addNote} onDelete={deleteNote}
              pendingFragment={fragment} onCancelFragment={() => setFragment(null)} selectedId={noteEntity} />}
      </section>
      {narrow ? <Drawer rootClassName="inspector-drawer" placement={mobile ? 'bottom' : 'right'} size={mobile ? '72vh' : 360}
        open={!!doc && inspector} onClose={() => setInspector(false)} closable={false} styles={{ body: { padding: 0 } }}>
        {inspectorPanel}
      </Drawer> : inspectorPanel}
    </div>
    <footer className="workspace-footer"><Text type="secondary">{num(doc.analysis.entities.length)} сущностей · {num(doc.analysis.edges.length)} связей · {doc.analysis.chapters.length} глав и блоков</Text>
      {!inspector && <Button type="link" size="small" onClick={() => { setInspector(true); setInspectorMode('entity'); }}>Открыть панель</Button>}
    </footer>
  </div>;

  return <div className="sq-shell">
    {mobile ? <Drawer placement="left" size={288} open={menuOpen} onClose={() => setMenuOpen(false)} closable={false} styles={{ body: { padding: 0 } }}>
      {sidebar}
    </Drawer> : <aside className="sq-sidebar">{sidebar}</aside>}
    <main className={`sq-main ${view === 'document' ? 'is-doc' : ''}`}>
      {header}
      {error && <div className="sq-error"><ErrorBox error={error} /></div>}
      <div className="sq-content">
        {view === 'library' && <LibraryScreen documents={documents} failed={failed} filters={libFilters} setFilters={setLibFilters} query={query}
          onAdd={() => navigate('add')} onOpen={d => openDoc(d.id)} onFiles={uploadFiles}
          onDelete={d => { setTarget(d); setModal('delete'); }} onEdit={d => { setTarget(d); setModal('meta'); }}
          onRetry={async d => { try { await retryAnalysis(d.id); showProgress(d); } catch { /* error shown */ } }}
          onRefine={aiStatus?.available ? async d => { try { await retryAnalysis(d.id, { ai: true }); showProgress(d); } catch { /* error shown */ } } : null}
          onReplaceFailed={replaceFailed} onDismissFailed={id => setFailed(list => list.filter(x => x.id !== id))} />}
        {view === 'add' && <AddTextScreen onSubmit={submitAdd} onCancel={() => navigate('library')} busy={submitting} registerDrop={registerAddDrop} ai={aiStatus} />}
        {view === 'progress' && progressId && <ProgressScreen key={progressId} id={progressId}
          onDone={id => { loadDocuments(); openDoc(id); }} onBack={() => navigate('library')} onRetry={retryAnalysis} />}
        {view === 'document' && workspace}
      </div>
    </main>
    {dragging && <DropOverlay />}
    {modal === 'meta' && target && <MetaModal doc={target} onClose={() => setModal(null)} onSaved={async d => {
      setModal(null); if (doc?.id === d.id) setDoc(d); await loadDocuments(); flash('Сведения о тексте сохранены');
    }} />}
    {modal === 'profile' && <ProfileModal user={user} onClose={() => setModal(null)} onSaved={u => { setUser(u); setModal(null); flash('Профиль сохранён'); }} />}
    {modal === 'method' && <MethodModal analysis={doc?.analysis} onClose={() => setModal(null)} />}
    {modal === 'delete' && target && <DeleteModal doc={target} onClose={() => setModal(null)} onDelete={async () => {
      await api(`/documents/${target.id}`, { method: 'DELETE' });
      setModal(null);
      if (doc?.id === target.id) { docRequest.current++; setDoc(null); setNotes([]); setQuestions([]); setView('library'); }
      await loadDocuments(); flash('Текст удалён');
    }} />}
    {modal === 'export' && doc && <Modal title="Экспорт" onClose={() => setModal(null)}>
      <Space orientation="vertical" className="full-width" size={12}>
        <Paragraph>Сохраните результаты анализа в удобном формате.</Paragraph>
        <Button block icon={icon(FileText)} onClick={() => { setModal(null); exportDoc('pdf'); }}>Скачать отчёт PDF</Button>
        <Button block icon={icon(Network)} onClick={() => { setModal(null); exportDoc('png'); }}>Скачать граф PNG</Button>
      </Space>
    </Modal>}
    {toast && <div className="toast"><Alert type="success" showIcon title={toast} /></div>}
  </div>;
}
