import React, { useEffect, useMemo, useRef, useState } from 'react';
import * as d3 from 'd3';
import {
  Alert, Avatar, Badge, Button, Checkbox, Descriptions, Divider, Drawer,
  Empty, Form, Input, InputNumber, List, Modal as AntModal, Popconfirm,
  Segmented, Select, Space, Spin, Switch, Table, Tabs, Tag, Tooltip,
  Typography, Upload,
} from 'antd';
import {
  ArrowRight, BookOpen, Check, ChevronRight, Download, FileText,
  HelpCircle, Link2, LogOut, Menu, MessageSquare, Network, Pencil,
  Plus, Search, Send, Settings, StickyNote, Trash2, Upload as UploadIcon,
  X, ZoomIn, ZoomOut, Maximize2,
} from 'lucide-react';
import { createRequestGuard } from './requestGuard.js';

const { TextArea, Password } = Input;
const { Text, Title, Paragraph } = Typography;
const TYPES = {
  person: { label: 'Персонажи', one: 'Персонаж', color: '#1677ff' },
  concept: { label: 'Понятия', one: 'Понятие', color: '#389e0d' },
  place: { label: 'Места', one: 'Место', color: '#d48806' },
  event: { label: 'События', one: 'Событие', color: '#722ed1' },
};
const GENRES = ['Художественный', 'Учебный', 'Научный', 'Другое'];
const num = n => new Intl.NumberFormat('ru-RU').format(n || 0);
const date = s => s ? new Date(s.endsWith('Z') || s.includes('+') ? s : s + 'Z')
  .toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' }) : '';
const defaultFilters = a => a.entities.some(n => n.type === 'person') ? ['person'] : Object.keys(TYPES);
const initialEntity = a => [...a.entities].sort((x, y) =>
  (x.type === 'person' ? -1 : 0) - (y.type === 'person' ? -1 : 0) || y.count - x.count)[0]?.id || null;
const icon = (Component, size = 16) => <Component size={size} />;

async function api(path, { method = 'GET', body, ...opts } = {}) {
  const form = body instanceof FormData;
  const response = await fetch('/api' + path, {
    method,
    credentials: 'same-origin',
    headers: body && !form ? { 'Content-Type': 'application/json' } : {},
    body: body ? (form ? body : JSON.stringify(body)) : undefined,
    ...opts,
  });
  if (!response.ok) {
    let data;
    try { data = await response.json(); } catch { /* Use the status fallback. */ }
    const detail = data?.detail;
    throw new Error(typeof detail === 'string' ? detail : Array.isArray(detail)
      ? detail.map(x => x.msg).join('. ') : `Ошибка ${response.status}. Попробуйте ещё раз.`);
  }
  return response.status === 204 ? null : response.json();
}

function IconButton({ label, children, className = '', ...props }) {
  return <Tooltip title={label}><Button type="text" className={className}
    aria-label={label} icon={children} {...props} /></Tooltip>;
}
function Busy({ text = 'Загрузка…' }) {
  return <div className="busy"><Spin /><Text type="secondary">{text}</Text></div>;
}
function ErrorBox({ error }) {
  return error ? <Alert type="error" showIcon title={error} className="error-message" /> : null;
}
function Modal({ title, children, onClose, wide = false, ...props }) {
  return <AntModal open title={title} onCancel={onClose} footer={null}
    width={wide ? 640 : 480} destroyOnHidden {...props}>{children}</AntModal>;
}
function Brand() {
  return <div className="brand"><Network size={20} /><span>Семантический атлас</span></div>;
}
function EntityTag({ type }) {
  const t = TYPES[type];
  return t ? <Tag color={t.color}>{t.one}</Tag> : null;
}

function Auth({ onAuth }) {
  const [register, setRegister] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  async function submit(values) {
    setBusy('auth'); setError('');
    try {
      const result = await api('/auth/' + (register ? 'register' : 'login'), { method: 'POST', body: values });
      onAuth(result.user);
    } catch (e) { setError(e.message); } finally { setBusy(''); }
  }
  async function demo() {
    setBusy('demo'); setError('');
    try {
      const result = await api('/auth/demo', { method: 'POST', body: {} });
      onAuth(result.user, result.document_id);
    } catch (e) { setError(e.message); } finally { setBusy(''); }
  }
  return <main className="auth-page">
    <section className="auth-panel">
      <Brand />
      <Paragraph type="secondary">Анализ текстов и графы связей</Paragraph>
      <Segmented block value={register ? 'register' : 'login'} disabled={!!busy}
        options={[{ label: 'Вход', value: 'login' }, { label: 'Регистрация', value: 'register' }]}
        onChange={value => { setRegister(value === 'register'); setError(''); }} />
      <Form layout="vertical" onFinish={submit} key={String(register)} requiredMark={false} className="auth-form">
        {register && <Form.Item label="Имя" name="name" rules={[{ required: true, message: 'Укажите имя.' }]}>
          <Input autoComplete="name" maxLength={80} disabled={!!busy} />
        </Form.Item>}
        <Form.Item label="Логин" name="login" rules={[{ required: true, message: 'Укажите логин.' },
          { min: 3, message: 'Не менее 3 символов.' }]}>
          <Input autoComplete="username" maxLength={64} disabled={!!busy} />
        </Form.Item>
        <Form.Item label="Пароль" name="password" rules={[{ required: true, message: 'Укажите пароль.' },
          { min: 8, message: 'Не менее 8 символов.' }]}>
          <Password autoComplete={register ? 'new-password' : 'current-password'} maxLength={128} disabled={!!busy} />
        </Form.Item>
        <ErrorBox error={error} />
        <Button type="primary" htmlType="submit" block loading={busy === 'auth'} disabled={!!busy && busy !== 'auth'}>
          {register ? 'Создать аккаунт' : 'Войти'}
        </Button>
      </Form>
      <Divider plain>Демонстрация</Divider>
      <Button block onClick={demo} loading={busy === 'demo'} disabled={!!busy && busy !== 'demo'}
        icon={icon(Network)}>Открыть пример</Button>
      <Paragraph type="secondary" className="auth-note">Отдельная демобиблиотека с учебным рассказом. Платные ключи не нужны.</Paragraph>
    </section>
  </main>;
}
function Graph({analysis,selected,onSelect,onEdge,filters,search}){
 const host=useRef(null),svgRef=useRef(null),zoomRef=useRef(null);
 const [size,setSize]=useState({w:800,h:600});
 useEffect(()=>{const ro=new ResizeObserver(e=>{const r=e[0].contentRect;setSize({w:r.width,h:r.height})});if(host.current)ro.observe(host.current);return()=>ro.disconnect()},[]);
 const entities=analysis.entities||[],edges=analysis.edges||[];
 const visible=useMemo(()=>entities.filter(n=>filters.includes(n.type)&&(!search||n.label.toLowerCase().includes(search.toLowerCase()))),[entities,filters,search]);
 useEffect(()=>{
  const width=size.w,height=size.h;if(width<1||height<1)return;
  const ids=new Set(visible.map(n=>n.id)),nodes=visible.map((n,i)=>({...n,x:width/2+Math.cos(i*2.4)*Math.sqrt(i)*25,y:height/2+Math.sin(i*2.4)*Math.sqrt(i)*25})),links=edges.filter(e=>ids.has(e.source)&&ids.has(e.target)).map(e=>({...e}));
  const svg=d3.select(svgRef.current);svg.selectAll('*').remove();svg.attr('viewBox',`0 0 ${width} ${height}`);const group=svg.append('g');
  const zoom=d3.zoom().scaleExtent([.15,3.5]).on('zoom',e=>group.attr('transform',e.transform));svg.call(zoom).on('dblclick.zoom',null);const fit=()=>{if(!nodes.length)return;const x0=d3.min(nodes,n=>n.x)-65,x1=d3.max(nodes,n=>n.x)+65,y0=d3.min(nodes,n=>n.y)-48,y1=d3.max(nodes,n=>n.y)+68;const k=Math.min(1.15,Math.max(.15,Math.min((width-55)/(x1-x0),(height-85)/(y1-y0))));svg.transition().duration(250).call(zoom.transform,d3.zoomIdentity.translate(width/2-k*(x0+x1)/2,height/2-k*(y0+y1)/2).scale(k))};zoomRef.current={svg,zoom,fit};
  const max=d3.max(nodes,n=>n.count)||1,radius=n=>13+Math.sqrt(n.count/max)*22;
  const line=group.append('g').attr('class','graph-edges').selectAll('line').data(links).join('line').attr('stroke','#bcc7c9').attr('stroke-width',e=>Math.min(1+Math.sqrt(e.weight||1)*.45,3)).attr('tabindex',0).attr('role','button').attr('aria-label',e=>`Связь: ${e.label}. Показать подтверждение`).on('click',(e,d)=>{e.stopPropagation();onEdge(d)}).on('keydown',(e,d)=>{if(e.key==='Enter')onEdge(d)});
  line.append('title').text(e=>`${e.label} · предположение на основе текста`);
  const node=group.append('g').selectAll('g').data(nodes).join('g').attr('class','graph-node').attr('data-id',n=>n.id).attr('tabindex',0).attr('role','button').attr('aria-label',n=>`${n.label}, ${TYPES[n.type]?.one}, ${n.count} упоминаний`).on('click',(e,n)=>{e.stopPropagation();onSelect(n.id)}).on('keydown',(e,n)=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();onSelect(n.id)}});
  node.append('circle').attr('class','selection-ring').attr('r',n=>radius(n)+7).attr('fill','none').attr('stroke',n=>TYPES[n.type]?.color||'#2c6bba').attr('stroke-width',1.4).attr('opacity',n=>n.id===selected?1:0);
  node.append('circle').attr('class','node-circle').attr('r',radius).attr('fill',n=>TYPES[n.type]?.color||'#2c6bba').attr('stroke','#fcfcfa').attr('stroke-width',2.5);
  node.append('text').attr('class','node-count').attr('text-anchor','middle').attr('dy','.35em').attr('fill','white').attr('font-size',n=>radius(n)>25?13:11).attr('font-weight',600).text(n=>num(n.count));
  node.append('text').attr('class','node-label').attr('text-anchor','middle').attr('dy',n=>radius(n)+19).attr('fill','#253641').attr('font-size',12).attr('font-weight',500).attr('paint-order','stroke').attr('stroke','#f9faf8').attr('stroke-width',5).attr('stroke-linejoin','round').text(n=>n.label.length>24?n.label.slice(0,23)+'…':n.label);
  node.append('title').text(n=>n.label);
  const simulation=d3.forceSimulation(nodes).force('link',d3.forceLink(links).id(n=>n.id).distance(e=>110+Math.random()*15).strength(.13)).force('charge',d3.forceManyBody().strength(-290)).force('center',d3.forceCenter(width/2,height/2)).force('collision',d3.forceCollide().radius(n=>radius(n)+34)).force('x',d3.forceX(width/2).strength(.045)).force('y',d3.forceY(height/2).strength(.055));
  const render=()=>{line.attr('x1',e=>e.source.x).attr('y1',e=>e.source.y).attr('x2',e=>e.target.x).attr('y2',e=>e.target.y);node.attr('transform',n=>`translate(${n.x},${n.y})`)};
  simulation.on('end',fit).on('tick',render);simulation.stop().tick(180);render();fit();
  node.call(d3.drag().on('start',(e,n)=>{if(!e.active)simulation.alphaTarget(.15).restart();n.fx=n.x;n.fy=n.y}).on('drag',(e,n)=>{n.fx=e.x;n.fy=e.y}).on('end',(e,n)=>{if(!e.active)simulation.alphaTarget(0);n.fx=null;n.fy=null}));
  return()=>simulation.stop();
 },[visible,edges,size.w,size.h]);
 useEffect(()=>{d3.select(svgRef.current).selectAll('.graph-node .selection-ring').attr('opacity',function(){return this.parentNode.getAttribute('data-id')===String(selected)?1:0})},[selected]);
 const zoomBy=k=>{const z=zoomRef.current;if(z)z.svg.transition().duration(220).call(z.zoom.scaleBy,k)};
 const fit=()=>zoomRef.current?.fit?.();
 return <div className="graph-canvas" ref={host}><svg ref={svgRef} className="graph-svg" aria-label="Интерактивный граф сущностей текста"/>{!visible.length&&<div className="graph-empty"><Network size={36}/><h3>Нет сущностей для показа</h3><p>Измените фильтры или попробуйте другой текст.</p></div>}<div className="canvas-caption"><span className="status-dot"/>{num(visible.length)} сущностей на карте <span className="canvas-separator">/</span><span>Размер узла — частота упоминаний</span></div><div className="graph-controls"><IconButton label="Приблизить граф" onClick={()=>zoomBy(1.3)}><ZoomIn size={18}/></IconButton><IconButton label="Отдалить граф" onClick={()=>zoomBy(.77)}><ZoomOut size={18}/></IconButton><IconButton label="Сбросить масштаб" onClick={fit}><Maximize2 size={18}/></IconButton></div><div className="graph-tip">Перетаскивайте узлы · прокручивайте для масштаба</div></div>
}

function MetadataFields({ genre }) {
  const genres = [...new Set([genre, ...GENRES].filter(Boolean))];
  return <>
    <Form.Item label="Название" name="title" rules={[{ required: true, whitespace: true, message: 'Укажите название.' }]}>
      <Input maxLength={200} />
    </Form.Item>
    <div className="form-columns">
      <Form.Item label="Автор" name="author"><Input maxLength={160} /></Form.Item>
      <Form.Item label="Год" name="year"><InputNumber min={1} max={2200} placeholder="Необязательно" /></Form.Item>
    </div>
    <Form.Item label="Тип текста" name="genre"><Select options={genres.map(value => ({ value, label: value }))} /></Form.Item>
  </>;
}
function UploadModal({ onClose, onCreated }) {
  const [mode, setMode] = useState('text');
  const [file, setFile] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function submit(values) {
    setError(''); setBusy(true);
    try {
      let result;
      if (mode === 'file') {
        if (!file) throw new Error('Выберите файл для загрузки.');
        const body = new FormData();
        body.append('file', file);
        Object.entries(values).forEach(([key, value]) => {
          if (key !== 'content' && value !== undefined && value !== null) body.append(key, value);
        });
        result = await api('/documents/upload', { method: 'POST', body });
      } else {
        result = await api('/documents', { method: 'POST', body: { ...values, year: values.year || null } });
      }
      await onCreated(result);
    } catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  function beforeUpload(nextFile) {
    if (nextFile.size > 12 * 1024 * 1024) { setError('Файл больше 12 МБ.'); return Upload.LIST_IGNORE; }
    if (!/\.(txt|pdf|epub)$/i.test(nextFile.name)) { setError('Поддерживаются TXT, PDF и EPUB.'); return Upload.LIST_IGNORE; }
    setFile(nextFile); setError(''); return false;
  }
  return <Modal title="Добавить текст" onClose={() => !busy && onClose()} wide closable={!busy} mask={{ closable: !busy }}>
    <Form layout="vertical" onFinish={submit} initialValues={{ genre: 'Художественный' }} requiredMark={false} disabled={busy}>
      <Segmented block value={mode} disabled={busy} onChange={setMode} className="form-segmented"
        options={[{ label: 'Вставить текст', value: 'text' }, { label: 'Загрузить файл', value: 'file' }]} />
      <MetadataFields />
      {mode === 'text' ? <Form.Item label="Текст" name="content" rules={[{ required: true, whitespace: true, message: 'Вставьте текст.' }]}>
        <TextArea rows={7} placeholder="Вставьте произведение или фрагмент текста. Заголовки глав сохраняются." />
      </Form.Item> : <Form.Item label="Файл">
        <Upload.Dragger accept=".txt,.pdf,.epub" beforeUpload={beforeUpload} maxCount={1}
          fileList={file ? [{ uid: 'selected', name: file.name, status: 'done' }] : []}
          onRemove={() => { setFile(null); return true; }}>
          <UploadIcon size={24} />
          <p>Выберите файл или перетащите его сюда</p>
          <Text type="secondary">TXT, EPUB или PDF с текстовым слоем, до 12 МБ</Text>
        </Upload.Dragger>
      </Form.Item>}
      <ErrorBox error={error} />
      {busy && <Alert type="info" showIcon title="Анализируем текст. Большой документ потребует больше времени." />}
      <div className="modal-actions"><Button onClick={onClose} disabled={busy}>Отмена</Button>
        <Button htmlType="submit" type="primary" loading={busy}>Добавить и проанализировать</Button></div>
    </Form>
  </Modal>;
}
function MetaModal({ doc, onClose, onSaved }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  async function submit(values) {
    setBusy(true); setError('');
    try { await onSaved(await api(`/documents/${doc.id}`, { method: 'PATCH', body: { ...values, year: values.year || null } })); }
    catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  return <Modal title="Сведения о тексте" onClose={onClose}>
    <Form layout="vertical" initialValues={doc} onFinish={submit} requiredMark={false} disabled={busy}>
      <MetadataFields genre={doc.genre} /><ErrorBox error={error} />
      <div className="modal-actions"><Button onClick={onClose}>Отмена</Button>
        <Button type="primary" htmlType="submit" loading={busy}>Сохранить</Button></div>
    </Form>
  </Modal>;
}
function ProfileModal({ user, onClose, onSaved }) {
  const [avatar, setAvatar] = useState(user.avatar || '');
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  function upload(file) {
    if (file.size > 500000) { setError('Фото должно быть меньше 500 КБ.'); return Upload.LIST_IGNORE; }
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) { setError('Выберите PNG, JPEG или WebP.'); return Upload.LIST_IGNORE; }
    const reader = new FileReader();
    reader.onload = () => { setAvatar(reader.result); setError(''); };
    reader.readAsDataURL(file);
    return false;
  }
  async function submit(values) {
    setBusy(true); setError('');
    try { await onSaved(await api('/profile', { method: 'PATCH', body: { name: values.name, private: values.private, language: 'ru', avatar: avatar || null } })); }
    catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  return <Modal title="Профиль" onClose={onClose}>
    <Form layout="vertical" initialValues={user} onFinish={submit} requiredMark={false} disabled={busy}>
      <div className="profile-photo"><Avatar size={56} src={avatar || undefined}>{user.name?.slice(0, 1)}</Avatar>
        <Upload accept="image/png,image/jpeg,image/webp" showUploadList={false} beforeUpload={upload}>
          <Button icon={icon(UploadIcon)}>Выбрать фото</Button></Upload>
        {avatar && <IconButton label="Удалить фото" onClick={() => setAvatar('')}>{icon(Trash2)}</IconButton>}
      </div>
      <Form.Item label="Имя" name="name" rules={[{ required: true, whitespace: true, message: 'Укажите имя.' }]}><Input maxLength={80} /></Form.Item>
      <Form.Item label="Логин"><Input value={user.login} disabled /></Form.Item>
      <Form.Item label="Язык интерфейса"><Select value="ru" disabled options={[{ label: 'Русский', value: 'ru' }]} /></Form.Item>
      <Form.Item label="Приватный профиль" name="private" valuePropName="checked" extra="Доступ к текстам всегда есть только у вас."><Switch /></Form.Item>
      <ErrorBox error={error} /><div className="modal-actions"><Button onClick={onClose}>Отмена</Button>
        <Button type="primary" htmlType="submit" loading={busy}>Сохранить</Button></div>
    </Form>
  </Modal>;
}
function MethodModal({ onClose, analysis }) {
  return <Modal title="Как работает анализ" onClose={onClose} wide>
    <Typography className="method-content">
      <Title level={5}>Сущности и связи</Title>
      <Paragraph>Локальный анализ использует морфологию русского языка, частотность слов и правила распознавания имён, мест и событий. Совместные упоминания образуют связи. Типизированные связи — предположения по языковым признакам.</Paragraph>
      <Title level={5}>Вопросы по тексту</Title>
      <Paragraph>Система находит подходящие фрагменты и сущности графа. Ответы опираются на источник без генеративной модели и не дополняют содержание произведения.</Paragraph>
      <Title level={5}>Ограничения</Title>
      <Paragraph>{analysis?.limitations || 'Имена могут объединяться или разделяться ошибочно. Значения терминов, мотивы персонажей и причинные связи требуют проверки. Граф показывает наиболее значимые сущности текста.'}</Paragraph>
      <Title level={5}>Файлы и хранение</Title>
      <Paragraph>Поддерживаются TXT, EPUB и PDF с текстовым слоем до 12 МБ. Для сканов нужен предварительный OCR. Результаты, вопросы и заметки сохраняются в базе данных сервера.</Paragraph>
    </Typography><div className="modal-actions"><Button type="primary" onClick={onClose}>Закрыть</Button></div>
  </Modal>;
}
function Analytics({ doc, onEntity }) {
  const a = doc.analysis;
  const max = Math.max(...a.entities.map(e => e.count), 1);
  const columns = [
    { title: 'Сущность', dataIndex: 'label', key: 'label', render: (label, row) => <Button type="link" className="table-link" onClick={() => onEntity(row.id)}>{label}</Button> },
    { title: 'Тип', dataIndex: 'type', key: 'type', width: 130, filters: Object.entries(TYPES).map(([value, t]) => ({ value, text: t.one })), onFilter: (value, row) => row.type === value, render: value => <EntityTag type={value} /> },
    { title: 'Упоминания', dataIndex: 'count', key: 'count', width: 155, defaultSortOrder: 'descend', sorter: (x, y) => x.count - y.count, render: (count, row) => <div className="frequency-cell"><span className="frequency-bar" style={{ width: `${Math.max(3, count / max * 100)}%`, background: TYPES[row.type]?.color }} /><span>{num(count)}</span></div> },
  ];
  const chapters = [
    { title: 'Глава или блок', dataIndex: 'title', key: 'title' },
    { title: 'Ключевые темы', dataIndex: 'themes', key: 'themes', render: themes => <Space size={[4, 4]} wrap>{themes.map(t => <Tag key={t}>{t}</Tag>)}</Space> },
    { title: 'События', dataIndex: 'event_count', key: 'event_count', width: 95, render: num },
    { title: 'Слова', dataIndex: 'word_count', key: 'word_count', width: 95, render: num },
  ];
  return <div className="content-page analytics-page">
    <Title level={4}>Аналитика</Title>
    <Descriptions size="small" column={{ xs: 2, sm: 2, md: 2, lg: 4 }} className="analysis-summary" items={[
      { key: 'words', label: 'Слова', children: num(doc.word_count) },
      { key: 'entities', label: 'Сущности', children: a.entities.length },
      { key: 'edges', label: 'Связи', children: a.edges.length },
      { key: 'chapters', label: 'Главы и блоки', children: a.chapters.length },
    ]} />
    <Title level={5}>Сущности и частота упоминаний</Title>
    <Table size="small" columns={columns} dataSource={a.entities} rowKey="id"
      pagination={{ pageSize: 10, showSizeChanger: false, hideOnSinglePage: true }} scroll={{ x: 470 }} />
    <Title level={5}>Ключевые слова</Title>
    <div className="keyword-list">{a.keywords.map(k => <Tag key={k.word}>{k.word} <Text type="secondary">{num(k.count)}</Text></Tag>)}</div>
    <Title level={5}>Динамика по главам</Title>
    <Table size="small" columns={chapters} dataSource={a.chapters} rowKey="id"
      pagination={false} scroll={{ x: 500 }} />
  </div>;
}
function Reader({ doc, onFragment }) {
  const [chapter, setChapter] = useState('all'), [selection, setSelection] = useState('');
  const chapters = doc.analysis.chapters || [];
  const shown = chapter === 'all' ? chapters : chapters.filter(c => String(c.id) === chapter);
  function select() {
    const selected = window.getSelection()?.toString().trim();
    if (!selected || selected.length > 3000) { setSelection(''); return; }
    if (doc.content.includes(selected)) { setSelection(selected); return; }
    const escaped = selected.split(/\s+/).map(t => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
    setSelection(doc.content.match(new RegExp(escaped.join('\\s+'), 'u'))?.[0] || '');
  }
  return <div className="reader-page">
    <div className="reader-toolbar"><Select value={chapter} onChange={setChapter} aria-label="Глава"
      options={[{ label: 'Весь текст', value: 'all' }, ...chapters.map(c => ({ label: c.title, value: String(c.id) }))]} />
      <Text type="secondary">Выделите фрагмент, чтобы добавить заметку</Text>
    </div>
    {selection && <Alert type="info" title="Выбран фрагмент текста" description={selection.slice(0, 130)}
      action={<Space><Button type="primary" size="small" onClick={() => { onFragment(selection); setSelection(''); }}>Добавить заметку</Button>
        <IconButton label="Снять выделение" onClick={() => setSelection('')}>{icon(X)}</IconButton></Space>} />}
    <article className="reader-text" onMouseUp={select} onTouchEnd={select}>
      <Title level={3}>{doc.title}</Title><Paragraph type="secondary">{doc.author || 'Автор не указан'}{doc.year ? ` · ${doc.year}` : ''}</Paragraph>
      {shown.length ? shown.map(c => <section key={c.id}><Title level={4}>{c.title}</Title>
        {c.text.split(/\n\s*\n|\n/).filter(Boolean).map((p, i) => <Paragraph key={i}>{p}</Paragraph>)}
      </section>) : <Paragraph>{doc.content}</Paragraph>}
    </article>
  </div>;
}
function Notes({ notes, entities, onAdd, onDelete, pendingFragment, onCancelFragment, selectedId }) {
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  useEffect(() => { form.setFieldValue('entity_id', selectedId || ''); }, [selectedId, form]);
  async function submit(values) {
    setBusy(true); setError('');
    try {
      await onAdd({ text: values.text.trim(), entity_id: values.entity_id || null, fragment: pendingFragment || null });
      form.setFieldValue('text', ''); onCancelFragment();
    } catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  return <div className="content-page notes-page">
    <Title level={4}>Заметки</Title>
    <Form form={form} layout="vertical" onFinish={submit} requiredMark={false} initialValues={{ entity_id: selectedId || '' }}>
      <Form.Item name="text" label="Новая заметка" rules={[{ required: true, whitespace: true, message: 'Напишите заметку.' }]}>
        <TextArea rows={3} maxLength={10000} placeholder="Текст заметки" disabled={busy} />
      </Form.Item>
      {pendingFragment && <Alert type="info" closable onClose={onCancelFragment} title="Привязка к фрагменту" description={pendingFragment} className="fragment-preview" />}
      <div className="note-compose-actions"><Form.Item name="entity_id" className="note-entity-select">
        <Select aria-label="Привязка к сущности" options={[{ label: 'Ко всему произведению', value: '' }, ...entities.map(n => ({ label: n.label, value: n.id }))]} />
      </Form.Item><Button type="primary" htmlType="submit" loading={busy} icon={icon(Plus)}>Сохранить заметку</Button></div>
      <ErrorBox error={error} />
    </Form>
    <Divider />
    <Text strong>Все заметки ({notes.length})</Text>
    {notes.length ? <div className="notes-list">{notes.map(note => <NoteItem key={note.id} note={note} entities={entities} onDelete={onDelete} />)}</div>
      : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Заметок пока нет" />}
  </div>;
}
function NoteItem({ note, entities, onDelete }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  return <article className="note-item">
    <div className="note-meta"><Tag>{note.entity_id ? entities.find(e => e.id === note.entity_id)?.label || 'Сущность' : 'Произведение'}</Tag>
      <Text type="secondary">{date(note.created_at)}</Text>
      <Popconfirm title="Удалить заметку?" okText="Удалить" onConfirm={async () => {
        setBusy(true); try { await onDelete(note.id); } catch (e) { setError(e.message); } finally { setBusy(false); }
      }}><Button type="text" danger icon={icon(Trash2)} loading={busy} aria-label="Удалить заметку" /></Popconfirm>
    </div>
    <Paragraph className="note-text">{note.text}</Paragraph>
    {note.fragment && <blockquote className="source-quote">{note.fragment}</blockquote>}
    <ErrorBox error={error} />
  </article>;
}
function Questions({ questions, onAsk }) {
  const [question, setQuestion] = useState(''), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const bottom = useRef();
  useEffect(() => { bottom.current?.scrollIntoView({ block: 'nearest' }); }, [questions.length]);
  async function submit() {
    if (!question.trim()) return;
    setBusy(true); setError('');
    try { await onAsk(question.trim()); setQuestion(''); }
    catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  return <div className="questions-panel">
    <Text type="secondary" className="qa-description">Поиск по содержанию текста с цитатами. Без генерации дополнительных фактов.</Text>
    <div className="qa-history">
      {!questions.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="История вопросов пуста" />}
      {questions.map(q => <article className="qa-message" key={q.id}>
        <Text strong>{q.question}</Text><Paragraph className="qa-answer">{q.answer}</Paragraph>
        {q.sources?.map((s, i) => <details className="qa-source" key={i}><summary>{s.chapter || 'Фрагмент текста'}</summary><blockquote>{s.text}</blockquote></details>)}
      </article>)}<div ref={bottom} />
    </div>
    <div className="qa-form"><TextArea aria-label="Вопрос по тексту" value={question} onChange={e => setQuestion(e.target.value)}
      placeholder="Например, что нашла Анна?" rows={3} maxLength={1000} disabled={busy}
      onPressEnter={e => { if (!e.shiftKey) { e.preventDefault(); submit(); } }} />
      <ErrorBox error={error} /><Button type="primary" block onClick={submit} loading={busy} disabled={!question.trim()} icon={icon(Send)}>Отправить вопрос</Button>
    </div>
  </div>;
}
function Inspector({ doc, selectedId, edge, onSelect, onClose, onNote, questions, onAsk, mode, setMode }) {
  const { entities, edges } = doc.analysis;
  const selected = entities.find(n => n.id === selectedId);
  const related = selected ? edges.filter(e => e.source === selected.id || e.target === selected.id) : [];
  return <aside className="inspector">
    <div className="inspector-heading"><Tabs activeKey={mode} onChange={setMode} size="small" items={[
      { key: 'entity', label: 'Сущность' }, { key: 'questions', label: `Вопросы${questions.length ? ` (${questions.length})` : ''}` },
    ]} /><IconButton label="Закрыть панель" onClick={onClose}>{icon(X)}</IconButton></div>
    {mode === 'questions' ? <Questions questions={questions} onAsk={onAsk} /> : edge ? <div className="inspector-body">
      <Text type="secondary">Предполагаемая связь</Text>
      <Title level={4}>{entities.find(n => n.id === edge.source)?.label} → {entities.find(n => n.id === edge.target)?.label}</Title>
      <Tag>{edge.label}</Tag><Paragraph type="secondary">Тип определён автоматически и требует проверки.</Paragraph>
      <Title level={5}>Подтверждение в тексте</Title>
      {edge.evidence?.map((q, i) => <blockquote className="source-quote" key={i}>{typeof q === 'string' ? q : q.text}</blockquote>)}
      <Button block onClick={() => onSelect(edge.source)}>Открыть сущность</Button>
    </div> : selected ? <div className="inspector-body">
      <EntityTag type={selected.type} /><Title level={4}>{selected.label}</Title>
      <Paragraph type="secondary">{selected.description}</Paragraph>
      <Descriptions size="small" column={2} items={[
        { key: 'count', label: 'Упоминания', children: num(selected.count) },
        { key: 'related', label: 'Связи', children: related.length },
      ]} />
      <Title level={5}>Цитаты</Title>
      {selected.quotes?.length ? selected.quotes.slice(0, 5).map((q, i) => <blockquote className="source-quote" key={i}>
        <Text type="secondary">{q.chapter}</Text><Paragraph>{q.text}</Paragraph>
      </blockquote>) : <Text type="secondary">Цитаты не найдены.</Text>}
      <Title level={5}>Связанные сущности</Title>
      <List size="small" dataSource={related.slice(0, 12)} locale={{ emptyText: 'Связи не найдены' }} renderItem={e => {
        const target = entities.find(n => n.id === (e.source === selected.id ? e.target : e.source));
        return target ? <List.Item><Button type="link" className="related-entity" onClick={() => onSelect(target.id)}>
          <span>{target.label}<Text type="secondary">{e.label}</Text></span><ChevronRight size={15} />
        </Button></List.Item> : null;
      }} />
      <Button block icon={icon(StickyNote)} onClick={() => onNote(selected.id)}>Добавить заметку</Button>
    </div> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Выберите узел или связь на графе" />}
  </aside>;
}
function DeleteModal({ doc, onClose, onDelete }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  return <Modal title="Удалить текст?" onClose={onClose}>
    <Paragraph>«{doc.title}», его граф, заметки и история вопросов будут удалены.</Paragraph><ErrorBox error={error} />
    <div className="modal-actions"><Button onClick={onClose} disabled={busy}>Отмена</Button>
      <Button danger type="primary" loading={busy} onClick={async () => {
        setBusy(true); try { await onDelete(); } catch (e) { setError(e.message); setBusy(false); }
      }}>Удалить</Button></div>
  </Modal>;
}

function SidebarContent({ user, documents, doc, query, setQuery, sort, setSort, openDoc, setModal, logout }) {
  return <>
    <div className="sidebar-brand"><Brand /></div>
    <div className="sidebar-tools"><div className="sidebar-title"><Text strong>Библиотека</Text><Badge count={documents.length} showZero color="#8c8c8c" /></div>
      <Button type="primary" block icon={icon(Plus)} onClick={() => setModal('upload')}>Добавить текст</Button>
      <Input allowClear prefix={icon(Search, 15)} placeholder="Поиск по библиотеке" aria-label="Поиск по библиотеке" value={query} onChange={e => setQuery(e.target.value)} />
      <Select aria-label="Сортировка библиотеки" value={sort} onChange={setSort} options={[
        { label: 'Сначала новые', value: 'created' }, { label: 'По названию', value: 'title' }, { label: 'По автору', value: 'author' },
      ]} />
    </div>
    <nav className="document-list" aria-label="Тексты в библиотеке">
      {documents.map(d => <button className={`document-item ${doc?.id === d.id ? 'selected' : ''}`} key={d.id} onClick={() => openDoc(d.id)}>
        <FileText size={17} /><span><strong>{d.title}</strong><Text type="secondary">{d.author || 'Автор не указан'}</Text></span>
      </button>)}
      {!documents.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={query ? 'Ничего не найдено' : 'Нет текстов'} />}
    </nav>
    <div className="sidebar-bottom"><Button type="text" block icon={icon(HelpCircle)} onClick={() => setModal('method')}>Как работает анализ</Button>
      <Divider /><div className="user-row"><Button type="text" className="profile-button" onClick={() => setModal('profile')}>
        <Avatar size={28} src={user.avatar || undefined}>{user.name?.slice(0, 1)}</Avatar><span>{user.name}</span><Settings size={15} />
      </Button><IconButton label="Выйти из аккаунта" onClick={logout}>{icon(LogOut)}</IconButton></div>
    </div>
  </>;
}

export default function App() {
  const [user, setUser] = useState(null), [checking, setChecking] = useState(true);
  const [documents, setDocuments] = useState([]), [doc, setDoc] = useState(null), [docBusy, setDocBusy] = useState(false);
  const [error, setError] = useState(''), [toast, setToast] = useState('');
  const [query, setQuery] = useState(''), [sort, setSort] = useState('created'), [tab, setTab] = useState('graph');
  const [modal, setModal] = useState(null), [selectedId, setSelectedId] = useState(null), [edge, setEdge] = useState(null);
  const [filters, setFilters] = useState(Object.keys(TYPES)), [entitySearch, setEntitySearch] = useState('');
  const [inspector, setInspector] = useState(() => window.innerWidth > 1100), [inspectorMode, setInspectorMode] = useState('entity');
  const [sidebar, setSidebar] = useState(false), [notes, setNotes] = useState([]), [questions, setQuestions] = useState([]);
  const [fragment, setFragment] = useState(null), [noteEntity, setNoteEntity] = useState(''), [exportBusy, setExportBusy] = useState('');
  const [mobile, setMobile] = useState(() => window.innerWidth <= 760);
  const [narrow, setNarrow] = useState(() => window.innerWidth <= 1100);
  useEffect(() => {
    const resize = () => { setMobile(window.innerWidth <= 760); setNarrow(window.innerWidth <= 1100); };
    window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  }, []);
  useEffect(() => { if (narrow) setInspector(false); }, [narrow]);
 const guardRef=useRef(null);if(!guardRef.current)guardRef.current=createRequestGuard();
 const requestGuard=guardRef.current,{documentEpoch:docRequest,sessionEpoch,listEpoch:listRequest}=requestGuard;
 const renderedEpoch=sessionEpoch.current,renderedDocumentRequest=docRequest.current;
 const {sessionValid,documentValid}=requestGuard;
 const flash=t=>{setToast(t);const epoch=sessionEpoch.current;setTimeout(()=>{if(sessionValid(epoch))setToast('')},3800)};
 function clearWorkspace(){setDoc(null);setDocuments([]);setNotes([]);setQuestions([]);setSelectedId(null);setEdge(null);setFragment(null);setNoteEntity('');setModal(null);setSidebar(false);setDocBusy(false);setExportBusy('');setToast('');setError('');setQuery('');setTab('graph');setInspectorMode('entity')}
 useEffect(()=>{const epoch=sessionEpoch.current,c=new AbortController();api('/auth/me',{signal:c.signal}).then(u=>{if(sessionValid(epoch))setUser(u)}).catch(()=>{}).finally(()=>{if(!c.signal.aborted&&sessionValid(epoch))setChecking(false)});return()=>c.abort()},[]);
 async function loadDocuments(){const epoch=sessionEpoch.current,req=++listRequest.current;try{const r=await api(`/documents?q=${encodeURIComponent(query)}&sort=${sort}`);if(sessionValid(epoch)&&req===listRequest.current)setDocuments(r);return r}catch(e){if(sessionValid(epoch)&&req===listRequest.current)setError(e.message);return []}}
 useEffect(()=>{if(!user)return;const epoch=sessionEpoch.current,req=++listRequest.current,c=new AbortController();const timeout=setTimeout(()=>api(`/documents?q=${encodeURIComponent(query)}&sort=${sort}`,{signal:c.signal}).then(r=>{if(sessionValid(epoch)&&req===listRequest.current)setDocuments(r)}).catch(e=>{if(e.name!=='AbortError'&&sessionValid(epoch)&&req===listRequest.current)setError(e.message)}),180);return()=>{clearTimeout(timeout);c.abort()}},[user,query,sort]);
 async function openDoc(id){const req=++docRequest.current,epoch=sessionEpoch.current;setDocBusy(true);setError('');setSidebar(false);try{await requestGuard.runDocument(epoch,req,()=>Promise.all([api(`/documents/${id}`),api(`/documents/${id}/notes`),api(`/documents/${id}/questions`)]),([d,n,q])=>{setDoc(d);setNotes(n);setQuestions([...q].reverse());setSelectedId(initialEntity(d.analysis));setEdge(null);setEntitySearch('');setFilters(defaultFilters(d.analysis));setTab('graph');setFragment(null)})}catch(e){if(documentValid(epoch,req))setError(e.message)}finally{if(documentValid(epoch,req))setDocBusy(false)}}
 function onAuth(u,id){sessionEpoch.current++;docRequest.current++;listRequest.current++;clearWorkspace();setUser(u);setChecking(false);if(id)openDoc(id)}
 async function created(d,epoch){if(!sessionValid(epoch))return;docRequest.current++;setDocBusy(false);setModal(null);setDoc(d);setNotes([]);setQuestions([]);setTab('graph');setSelectedId(initialEntity(d.analysis));setEdge(null);setInspector(window.innerWidth>1100);setEntitySearch('');setFilters(defaultFilters(d.analysis));await loadDocuments();if(sessionValid(epoch))flash('Текст добавлен. Граф готов к исследованию.')}
 function selectEntity(id){const n=doc?.analysis.entities.find(e=>e.id===id);if(n&&!filters.includes(n.type))setFilters(f=>[...new Set([...f,n.type])]);setSelectedId(id);setEdge(null);setInspector(true);setInspectorMode('entity')}
 async function addNote(body){const epoch=sessionEpoch.current,req=docRequest.current,id=doc.id;await requestGuard.runDocument(epoch,req,()=>api(`/documents/${id}/notes`,{method:'POST',body}),n=>{setNotes(v=>[n,...v]);flash('Заметка сохранена')})}
 async function deleteNote(id){const epoch=sessionEpoch.current,req=docRequest.current,documentId=doc.id;await requestGuard.runDocument(epoch,req,()=>api(`/documents/${documentId}/notes/${id}`,{method:'DELETE'}),()=>{setNotes(v=>v.filter(n=>n.id!==id));flash('Заметка удалена')})}
 async function ask(question){const epoch=sessionEpoch.current,req=docRequest.current,id=doc.id;await requestGuard.runDocument(epoch,req,()=>api(`/documents/${id}/questions`,{method:'POST',body:{question}}),q=>setQuestions(v=>[...v,q]))}
 async function exportDoc(type){const epoch=sessionEpoch.current,req=docRequest.current,id=doc.id,title=doc.title;setExportBusy(type);setError('');try{const r=await fetch(`/api/documents/${id}/export.${type}`,{credentials:'same-origin'});if(!r.ok){const e=await r.json();throw new Error(e.detail||'Не удалось экспортировать файл.')}const blob=await r.blob();if(!documentValid(epoch,req))return;const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=`${title.slice(0,70)}.${type}`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);flash('Файл подготовлен к скачиванию')}catch(e){if(documentValid(epoch,req))setError(e.message)}finally{if(documentValid(epoch,req))setExportBusy('')}}
 async function logout(){const epoch=++sessionEpoch.current;docRequest.current++;listRequest.current++;clearWorkspace();setUser(null);setChecking(true);try{await api('/auth/logout',{method:'POST'})}catch(e){if(sessionValid(epoch))setError(e.message)}finally{if(sessionValid(epoch))setChecking(false)}}

  if (checking) return <div className="boot"><Brand /><Busy text="Загрузка приложения…" /></div>;
  if (!user) return <Auth onAuth={onAuth} />;

  const sidebarContent = <SidebarContent {...{ user, documents, doc, query, setQuery, sort, setSort, openDoc, logout }}
    setModal={value => { setModal(value); setSidebar(false); }} />;
  const inspectorPanel = doc && inspector ? <Inspector key={doc.id} doc={doc} selectedId={selectedId} edge={edge}
    onSelect={selectEntity} onClose={() => setInspector(false)} onNote={id => {
      setNoteEntity(id); setTab('notes'); setFragment(null); if (narrow) setInspector(false);
    }} questions={questions} onAsk={ask} mode={inspectorMode} setMode={setInspectorMode} /> : null;

  return <div className="app-shell">
    {mobile ? <Drawer title={<Brand />} placement="left" size={280} open={sidebar} onClose={() => setSidebar(false)} styles={{ body: { padding: 0 } }}>
      <div className="mobile-sidebar">{sidebarContent}</div>
    </Drawer> : <aside className="sidebar">{sidebarContent}</aside>}
    <main className="workspace">
      <header className="workspace-header">
        <Space size={10}>{mobile && <IconButton label="Открыть библиотеку" onClick={() => setSidebar(true)}>{icon(Menu, 20)}</IconButton>}
          <Text type="secondary">Библиотека</Text>{doc && <><ChevronRight size={14} /><Text ellipsis className="header-document">{doc.title}</Text></>}
        </Space>
        <Button type="text" icon={icon(HelpCircle)} onClick={() => setModal('method')} className="header-help">Об анализе</Button>
      </header>
      {error && <div className="workspace-error"><ErrorBox error={error} /></div>}
      {docBusy ? <Busy text="Загрузка текста и результатов анализа…" /> : doc ? <>
        <div className="document-heading">
          <div><Title level={3}>{doc.title}</Title><Space split={<span>·</span>} size={8} wrap>
            <Text type="secondary">{doc.author || 'Автор не указан'}</Text>{doc.year && <Text type="secondary">{doc.year}</Text>}
            <Text type="secondary">{doc.genre || 'Текст'}</Text><Text type="secondary">{num(doc.word_count)} слов</Text>
          </Space></div>
          <Space className="document-actions" wrap>
            <Tooltip title="Редактировать сведения"><Button icon={icon(Pencil)} aria-label="Редактировать сведения о тексте" onClick={() => setModal('meta')} /></Tooltip>
            <Tooltip title="Удалить текст"><Button danger icon={icon(Trash2)} aria-label="Удалить текст" onClick={() => setModal('delete')} /></Tooltip>
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
        <div className="analysis-notice"><Alert type="info" showIcon
          title="Связи определены автоматически. Проверяйте предположения по цитатам." /></div>
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
            </> : tab === 'reader' ? <Reader key={doc.id} doc={doc} onFragment={value => {
              setFragment(value); setNoteEntity(''); setTab('notes'); if (narrow) setInspector(false);
            }} /> : tab === 'analytics' ? <Analytics doc={doc} onEntity={id => { selectEntity(id); setTab('graph'); }} /> :
              <Notes key={doc.id} notes={notes} entities={doc.analysis.entities} onAdd={addNote} onDelete={deleteNote}
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
      </> : <div className="welcome"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={
        <><Title level={4}>Библиотека</Title><Paragraph type="secondary">Выберите текст слева или добавьте новый для анализа.</Paragraph></>
      }><Button type="primary" icon={icon(Plus)} onClick={() => setModal('upload')}>Добавить текст</Button></Empty></div>}
    </main>
    {modal === 'upload' && <UploadModal key={user.id} onClose={() => setModal(null)} onCreated={d => created(d, renderedEpoch)} />}
    {modal === 'meta' && doc && <MetaModal doc={doc} onClose={() => setModal(null)} onSaved={async d => {
      if (!documentValid(renderedEpoch, renderedDocumentRequest)) return;
      setDoc(d); setModal(null); await loadDocuments();
      if (documentValid(renderedEpoch, renderedDocumentRequest)) flash('Сведения о тексте сохранены');
    }} />}
    {modal === 'profile' && <ProfileModal user={user} onClose={() => setModal(null)} onSaved={u => {
      if (!sessionValid(renderedEpoch)) return; setUser(u); setModal(null); flash('Профиль сохранён');
    }} />}
    {modal === 'method' && <MethodModal analysis={doc?.analysis} onClose={() => setModal(null)} />}
    {modal === 'delete' && doc && <DeleteModal doc={doc} onClose={() => setModal(null)} onDelete={async () => {
      const id = doc.id; await api(`/documents/${id}`, { method: 'DELETE' });
      if (!documentValid(renderedEpoch, renderedDocumentRequest)) return;
      docRequest.current++; setModal(null); setDoc(null); setNotes([]); setQuestions([]); setSelectedId(null); setEdge(null);
      await loadDocuments(); if (sessionValid(renderedEpoch)) flash('Текст удалён');
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
