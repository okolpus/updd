import React, { useEffect, useMemo, useRef, useState } from 'react';
import * as d3 from 'd3';
import {
  Alert, Avatar, Badge, Button, Checkbox, Descriptions, Divider, Drawer,
  Empty, Form, Input, InputNumber, List, Modal as AntModal, Popconfirm,
  Segmented, Select, Space, Spin, Switch, Table, Tabs, Tag, Tooltip,
  Progress, Typography, Upload,
} from 'antd';
import {
  ArrowRight, BookOpen, Check, ChevronRight, Download, FileText,
  HelpCircle, Link2, LogOut, Menu, MessageSquare, Network, Pencil,
  Plus, Search, Send, Settings, StickyNote, Trash2, Upload as UploadIcon,
  X, ZoomIn, ZoomOut, Maximize2, Brain,
} from 'lucide-react';

const { TextArea, Password } = Input;
const { Text, Title, Paragraph } = Typography;
export const TYPES = {
  person: { label: 'Персонажи', one: 'Персонаж', color: '#4f46e5' },
  concept: { label: 'Понятия', one: 'Понятие', color: '#10b981' },
  place: { label: 'Места', one: 'Место', color: '#f59e0b' },
  event: { label: 'События', one: 'Событие', color: '#a855f7' },
};
export const GENRES = ['Художественный', 'Учебный', 'Научный', 'Другое'];
export const num = n => new Intl.NumberFormat('ru-RU').format(n || 0);
export const date = s => s ? new Date(s.endsWith('Z') || s.includes('+') ? s : s + 'Z')
  .toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' }) : '';
export const defaultFilters = a => a.entities.some(n => n.type === 'person') ? ['person'] : Object.keys(TYPES);
export const initialEntity = a => [...a.entities].sort((x, y) =>
  (x.type === 'person' ? -1 : 0) - (y.type === 'person' ? -1 : 0) || y.count - x.count)[0]?.id || null;
export const icon = (Component, size = 16) => <Component size={size} />;

export async function api(path, { method = 'GET', body, ...opts } = {}) {
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

export function IconButton({ label, children, className = '', ...props }) {
  return <Tooltip title={label}><Button type="text" className={className}
    aria-label={label} icon={children} {...props} /></Tooltip>;
}
export function Busy({ text = 'Загрузка…' }) {
  return <div className="busy"><Spin /><Text type="secondary">{text}</Text></div>;
}
export function ErrorBox({ error }) {
  return error ? <Alert type="error" showIcon title={error} className="error-message" /> : null;
}
export function Modal({ title, children, onClose, wide = false, ...props }) {
  return <AntModal open title={title} onCancel={onClose} footer={null}
    width={wide ? 640 : 480} destroyOnHidden {...props}>{children}</AntModal>;
}
export function Brand() {
  return <div className="brand"><span className="brand-mark"><Brain size={20} /></span><span>Semantiq</span></div>;
}
export function EntityTag({ type }) {
  const t = TYPES[type];
  return t ? <Tag color={t.color}>{t.one}</Tag> : null;
}

export function Auth({ onAuth }) {
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
export function Graph({analysis,selected,onSelect,onEdge,filters,search}){
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

export function MetadataFields({ genre }) {
  const genres = [...new Set([genre, ...GENRES].filter(Boolean))];
  return <>
    <Form.Item label="Название" name="title" rules={[{ required: true, whitespace: true, message: 'Укажите название.' }]}>
      <Input maxLength={200} />
    </Form.Item>
    <div className="form-columns">
      <Form.Item label="Автор" name="author"><Input maxLength={160} /></Form.Item>
      <Form.Item label="Год" name="year"><InputNumber min={1} max={2200} placeholder="Необязательно" /></Form.Item>
    </div>
    <div className="form-columns">
      <Form.Item label="Тип текста" name="genre"><Select options={genres.map(value => ({ value, label: value }))} /></Form.Item>
      <Form.Item label="Жанр" name="subgenre"><Input maxLength={80} placeholder="Роман, повесть…" /></Form.Item>
    </div>
  </>;
}
export function MetaModal({ doc, onClose, onSaved }) {
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
export function ProfileModal({ user, onClose, onSaved }) {
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
export function MethodModal({ onClose, analysis }) {
  return <Modal title="Как работает анализ" onClose={onClose} wide>
    <Typography className="method-content">
      <Title level={5}>Сущности и связи</Title>
      <Paragraph>Локальный анализ на сервере сводит формы слов по окончаниям русского языка, учитывает частотность и контекст (предлоги, заглавные буквы, слова-маркеры) и по этим правилам распознаёт имена, места и события. Совместные упоминания образуют связи. Типизированные связи — предположения по языковым признакам.</Paragraph>
      <Title level={5}>Вопросы по тексту</Title>
      <Paragraph>Система находит подходящие фрагменты и сущности графа. Ответы опираются на источник без генеративной модели и не дополняют содержание произведения.</Paragraph>
      <Title level={5}>Нейросеть Qwen</Title>
      <Paragraph>Каждый текст разбирается через локальную нейросеть Qwen (Ollama на этом компьютере). Сначала быстрый локальный анализ находит сущности и предложения-доказательства, затем Qwen их проверяет. Если Qwen временно недоступна, текст разбирается упрощённо и перепроверяется автоматически, когда она заработает. Модель получает только найденные сущности и предложения-доказательства: исправляет названия и типы, объединяет дубли, убирает мусор и подписывает связи. Вопросы к тексту тогда получают короткий ответ с номерами цитат. Всё работает локально, текст никуда не отправляется. Нейросеть тоже ошибается, поэтому подписи помечены и сопровождаются цитатами.</Paragraph>
      <Title level={5}>Ограничения</Title>
      <Paragraph>{analysis?.limitations || 'Имена могут объединяться или разделяться ошибочно. Значения терминов, мотивы персонажей и причинные связи требуют проверки. Граф показывает наиболее значимые сущности текста.'}</Paragraph>
      <Title level={5}>Файлы и хранение</Title>
      <Paragraph>Поддерживаются TXT, EPUB и PDF с текстовым слоем до 12 МБ. Для сканов нужен предварительный OCR. Результаты, вопросы и заметки сохраняются в базе данных сервера.</Paragraph>
    </Typography><div className="modal-actions"><Button type="primary" onClick={onClose}>Закрыть</Button></div>
  </Modal>;
}
export function Analytics({ doc, onEntity }) {
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
    {a.ai && <Alert className="ai-summary" type={a.ai.error ? 'warning' : 'info'} showIcon title={a.ai.error
      ? `Нейросеть ${a.ai.model} не ответила: ${a.ai.error}. Показан локальный анализ.`
      : `Проверено нейросетью ${a.ai.model}: уточнено сущностей ${a.ai.entities}, объединено дублей ${a.ai.merged}, убрано лишних ${a.ai.removed}, подписано связей ${a.ai.edges}.`} />}
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
export function Reader({ doc, onFragment }) {
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
export function Notes({ notes, entities, onAdd, onDelete, pendingFragment, onCancelFragment, selectedId }) {
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
export function Questions({ questions, onAsk, ai }) {
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
    <Text type="secondary" className="qa-description">{ai ? `Отвечает ${ai} на вашем компьютере, только по найденным цитатам; номера в скобках — ссылки на фрагменты.` : 'Поиск по содержанию текста с цитатами. Без генерации дополнительных фактов.'}</Text>
    <div className="qa-history">
      {!questions.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="История вопросов пуста" />}
      {questions.map(q => <article className="qa-message" key={q.id}>
        <Text strong>{q.question}</Text>{q.mode?.startsWith('ai:') && <Tag color="purple" className="qa-mode">{q.mode.slice(3)}</Tag>}<Paragraph className="qa-answer">{q.answer}</Paragraph>
        {q.sources?.map((s, i) => <details className="qa-source" key={i}><summary>[{i + 1}] {s.chapter || 'Фрагмент текста'}</summary><blockquote>{s.text}</blockquote></details>)}
      </article>)}<div ref={bottom} />
    </div>
    <div className="qa-form"><TextArea aria-label="Вопрос по тексту" value={question} onChange={e => setQuestion(e.target.value)}
      placeholder="Например, что нашла Анна?" rows={3} maxLength={1000} disabled={busy}
      onPressEnter={e => { if (!e.shiftKey) { e.preventDefault(); submit(); } }} />
      <ErrorBox error={error} /><Button type="primary" block onClick={submit} loading={busy} disabled={!question.trim()} icon={icon(Send)}>Отправить вопрос</Button>
    </div>
  </div>;
}
export function Inspector({ doc, selectedId, edge, onSelect, onClose, onNote, questions, onAsk, mode, setMode, ai }) {
  const { entities, edges } = doc.analysis;
  const selected = entities.find(n => n.id === selectedId);
  const related = selected ? edges.filter(e => e.source === selected.id || e.target === selected.id) : [];
  return <aside className="inspector">
    <div className="inspector-heading"><Tabs activeKey={mode} onChange={setMode} size="small" items={[
      { key: 'entity', label: 'Сущность' }, { key: 'questions', label: `Вопросы${questions.length ? ` (${questions.length})` : ''}` },
    ]} /><IconButton label="Закрыть панель" onClick={onClose}>{icon(X)}</IconButton></div>
    {mode === 'questions' ? <Questions questions={questions} onAsk={onAsk} ai={ai} /> : edge ? <div className="inspector-body">
      <Text type="secondary">Предполагаемая связь</Text>
      <Title level={4}>{entities.find(n => n.id === edge.source)?.label} → {entities.find(n => n.id === edge.target)?.label}</Title>
      <Space size={6} wrap><Tag>{edge.label}</Tag>{edge.by_ai && <Tag color="purple">Qwen</Tag>}</Space>{edge.description && <Paragraph className="ai-description">{edge.description}</Paragraph>}<Paragraph type="secondary">{edge.by_ai ? 'Подпись предложена локальной нейросетью Qwen по цитатам ниже. Проверьте её.' : 'Тип определён автоматически и требует проверки.'}</Paragraph>
      <Title level={5}>Подтверждение в тексте</Title>
      {edge.evidence?.map((q, i) => <blockquote className="source-quote" key={i}>{typeof q === 'string' ? q : q.text}</blockquote>)}
      <Button block onClick={() => onSelect(edge.source)}>Открыть сущность</Button>
    </div> : selected ? <div className="inspector-body">
      <Space size={6}><EntityTag type={selected.type} />{selected.by_ai && <Tag color="purple">Проверено Qwen</Tag>}</Space><Title level={4}>{selected.label}</Title>
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
export function DeleteModal({ doc, onClose, onDelete }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  return <Modal title="Удалить текст?" onClose={onClose}>
    <Paragraph>«{doc.title}», его граф, заметки и история вопросов будут удалены.</Paragraph><ErrorBox error={error} />
    <div className="modal-actions"><Button onClick={onClose} disabled={busy}>Отмена</Button>
      <Button danger type="primary" loading={busy} onClick={async () => {
        setBusy(true); try { await onDelete(); } catch (e) { setError(e.message); setBusy(false); }
      }}>Удалить</Button></div>
  </Modal>;
}

