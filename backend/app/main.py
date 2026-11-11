from __future__ import annotations
import base64
import hashlib
import hmac
import io
import os
import re
import secrets
import threading
import time
import uuid
from contextlib import asynccontextmanager
from pathlib import Path
from urllib.parse import urlparse

import jwt
from fastapi import Depends, FastAPI, File, Form, HTTPException, Request, Response, UploadFile
from fastapi.exceptions import RequestValidationError
from fastapi.responses import FileResponse, JSONResponse
from fastapi.staticfiles import StaticFiles
from PIL import Image
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import delete, select, text
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from .analysis import analyze, answer_question
from .database import AuthSession, Base, DATA_DIR, Document, Note, Question, SessionLocal, User, engine, now
from .exports import export_pdf, export_png
from .parsing import MAX_BYTES, parse_file, validate_content
from .sample import DEMO_AUTHOR, DEMO_TEXT, DEMO_TITLE

COOKIE='atlas_session'
SESSION_SECONDS=7*24*60*60
SECURE=os.getenv('COOKIE_SECURE','false').lower()=='true'
KEY_PATH=DATA_DIR/'.jwt-key'
if os.getenv('SECRET_KEY'):
    SECRET=os.environ['SECRET_KEY']
else:
    try:
        fd=os.open(KEY_PATH,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as key_file: key_file.write(secrets.token_urlsafe(48))
    except FileExistsError: pass
    SECRET=KEY_PATH.read_text().strip()

@asynccontextmanager
async def lifespan(app):
    Base.metadata.create_all(engine)
    yield

app=FastAPI(title='Семантический атлас',version='1.0.0',lifespan=lifespan)

@app.exception_handler(RequestValidationError)
async def validation_error(request,exc):
    details=[]
    for error in exc.errors():
        field='.'.join(str(p) for p in error['loc'] if p!='body')
        details.append(f'{field}: {error["msg"]}')
    return JSONResponse(status_code=422,content={'detail':'Проверьте поля формы. '+'; '.join(details)})

@app.middleware('http')
async def protections(request,call_next):
    if request.method in ('POST','PUT','PATCH','DELETE'):
        origin=request.headers.get('origin')
        allowed={str(request.base_url).rstrip('/'),os.getenv('FRONTEND_ORIGIN','http://127.0.0.1:5173'),'http://localhost:5173'}
        if origin and origin not in allowed:
            return JSONResponse(status_code=403,content={'detail':'Запрос с другого сайта отклонён.'})
        try:
            if int(request.headers.get('content-length','0'))>16*1024*1024:
                return JSONResponse(status_code=413,content={'detail':'Размер запроса больше 16 МБ.'})
        except ValueError:
            return JSONResponse(status_code=400,content={'detail':'Некорректный размер запроса.'})
    response=await call_next(request)
    response.headers['X-Content-Type-Options']='nosniff'
    response.headers['Referrer-Policy']='same-origin'
    response.headers['X-Frame-Options']='DENY'
    if request.url.path.startswith('/api/'):
        response.headers['Cache-Control']='no-store'
    return response

def db():
    with SessionLocal() as session: yield session

def uid(): return str(uuid.uuid4())
def user_dict(user): return {k:getattr(user,k) for k in ('id','login','name','language','private','avatar')}
def summary(doc):
    return {**{k:getattr(doc,k) for k in ('id','title','author','year','genre','created_at')},'word_count':doc.analysis['word_count'],'entity_count':len(doc.analysis['entities']),'edge_count':len(doc.analysis['edges']),'status':'ready'}
def full(doc): return {**summary(doc),'content':doc.content,'analysis':doc.analysis}
def note_dict(n): return {k:getattr(n,k) for k in ('id','entity_id','fragment','text','created_at')}
def question_dict(q): return {k:getattr(q,k) for k in ('id','question','answer','sources','mode','created_at')}

def hash_password(password):
    salt=secrets.token_bytes(16)
    digest=hashlib.pbkdf2_hmac('sha256',password.encode(),salt,260000)
    return 'pbkdf2_sha256$260000$'+salt.hex()+'$'+digest.hex()
def verify_password(password,hashed):
    try:
        _,rounds,salt,digest=hashed.split('$')
        actual=hashlib.pbkdf2_hmac('sha256',password.encode(),bytes.fromhex(salt),int(rounds)).hex()
        return hmac.compare_digest(actual,digest)
    except (ValueError,TypeError): return False

def current_user(request:Request,session:Session=Depends(db)):
    raw=request.cookies.get(COOKIE)
    if not raw: raise HTTPException(401,'Войдите в аккаунт.')
    try:
        payload=jwt.decode(raw,SECRET,algorithms=['HS256'],options={'require':['exp','sub','jti']})
        auth=session.get(AuthSession,payload['jti'])
        if not auth or auth.user_id!=payload['sub'] or auth.expires_at<int(time.time()): raise ValueError()
        user=session.get(User,payload['sub'])
        if not user: raise ValueError()
        return user
    except (jwt.PyJWTError,ValueError,TypeError,KeyError): raise HTTPException(401,'Сеанс завершён. Войдите снова.')

def login_cookie(response,user,session):
    stamp=int(time.time()); sid=uid()
    session.execute(delete(AuthSession).where(AuthSession.expires_at<stamp))
    session.add(AuthSession(id=sid,user_id=user.id,expires_at=stamp+SESSION_SECONDS))
    session.commit()
    token=jwt.encode({'sub':user.id,'jti':sid,'iat':stamp,'exp':stamp+SESSION_SECONDS},SECRET,algorithm='HS256')
    response.set_cookie(COOKIE,token,max_age=SESSION_SECONDS,httponly=True,secure=SECURE,samesite='lax',path='/')

def owned(document_id,user,session):
    doc=session.get(Document,document_id)
    if not doc or doc.user_id!=user.id: raise HTTPException(404,'Текст не найден.')
    return doc

class Register(BaseModel):
    login:str=Field(min_length=3,max_length=80)
    name:str=Field(min_length=1,max_length=100)
    password:str=Field(min_length=8,max_length=128)
class Login(BaseModel):
    login:str=Field(min_length=1,max_length=80)
    password:str=Field(min_length=1,max_length=128)
class Profile(BaseModel):
    name:str|None=Field(default=None,min_length=1,max_length=100)
    language:str|None=None
    private:bool|None=None
    avatar:str|None=None
class DocumentInput(BaseModel):
    title:str=Field(min_length=1,max_length=250)
    author:str=Field(default='',max_length=180)
    year:int|None=Field(default=None,ge=1,le=2200)
    genre:str=Field(default='Художественный',max_length=80)
    content:str=Field(min_length=1,max_length=12_000_000)
class DocumentPatch(BaseModel):
    title:str|None=Field(default=None,min_length=1,max_length=250)
    author:str|None=Field(default=None,max_length=180)
    year:int|None=Field(default=None,ge=1,le=2200)
    genre:str|None=Field(default=None,max_length=80)
class NoteInput(BaseModel):
    text:str=Field(min_length=1,max_length=10000)
    entity_id:str|None=None
    fragment:str|None=Field(default=None,max_length=5000)
class QuestionInput(BaseModel):
    question:str=Field(min_length=2,max_length=1000)

_ATTEMPTS={}
_LOCK=threading.Lock()
def rate_limit(request,key,limit=25):
    address=request.client.host if request.client else 'unknown'
    key=f'{address}:{key}'
    stamp=time.monotonic()
    with _LOCK:
        if len(_ATTEMPTS)>5000:
            for k in list(_ATTEMPTS):
                if not _ATTEMPTS[k] or stamp-_ATTEMPTS[k][-1]>300: _ATTEMPTS.pop(k,None)
        values=[x for x in _ATTEMPTS.get(key,[]) if stamp-x<300]
        if len(values)>=limit: raise HTTPException(429,'Слишком много попыток. Подождите пять минут.')
        _ATTEMPTS[key]=values+[stamp]

@app.get('/api/health')
def health(session:Session=Depends(db)):
    session.execute(text('SELECT 1'))
    return {'status':'ok','database':'ok'}

@app.post('/api/auth/register',status_code=201)
def register(data:Register,request:Request,response:Response,session:Session=Depends(db)):
    rate_limit(request,'register')
    login=data.login.strip().lower()
    name=data.name.strip()
    if not re.fullmatch(r'[a-zа-яё0-9_.@-]{3,80}',login): raise HTTPException(422,'Логин: от 3 до 80 букв, цифр или символов _ . @ -')
    if not name: raise HTTPException(422,'Укажите имя.')
    user=User(id=uid(),login=login,name=name,password_hash=hash_password(data.password))
    session.add(user)
    try: session.commit()
    except IntegrityError:
        session.rollback(); raise HTTPException(409,'Этот логин уже занят.')
    login_cookie(response,user,session)
    return {'user':user_dict(user)}

@app.post('/api/auth/login')
def login(data:Login,request:Request,response:Response,session:Session=Depends(db)):
    rate_limit(request,'login')
    user=session.scalar(select(User).where(User.login==data.login.strip().lower()))
    if not user or not verify_password(data.password,user.password_hash): raise HTTPException(401,'Неверный логин или пароль.')
    login_cookie(response,user,session)
    return {'user':user_dict(user)}

@app.post('/api/auth/demo')
def demo(request:Request,response:Response,session:Session=Depends(db)):
    rate_limit(request,'demo',limit=15)
    user=User(id=uid(),login='demo_'+secrets.token_hex(8),name='Исследователь',password_hash=hash_password(secrets.token_urlsafe(32)))
    session.add(user);session.commit()
    doc=Document(id=uid(),user_id=user.id,title=DEMO_TITLE,author=DEMO_AUTHOR,year=2026,genre='Учебный рассказ',content=DEMO_TEXT,analysis=analyze(DEMO_TEXT))
    session.add(doc);session.commit()
    login_cookie(response,user,session)
    return {'user':user_dict(user),'document_id':doc.id}

@app.get('/api/auth/me')
def me(user:User=Depends(current_user)): return user_dict(user)

@app.post('/api/auth/logout')
def logout(request:Request,response:Response,session:Session=Depends(db)):
    raw=request.cookies.get(COOKIE)
    if raw:
        try:
            payload=jwt.decode(raw,SECRET,algorithms=['HS256'])
            session.execute(delete(AuthSession).where(AuthSession.id==payload.get('jti')));session.commit()
        except jwt.PyJWTError: pass
    response.delete_cookie(COOKIE,path='/')
    return {'ok':True}

@app.patch('/api/profile')
def update_profile(data:Profile,user:User=Depends(current_user),session:Session=Depends(db)):
    values=data.model_dump(exclude_unset=True)
    if 'name' in values:
        if not values['name'] or not values['name'].strip(): raise HTTPException(422,'Имя не может быть пустым.')
        values['name']=values['name'].strip()
    if 'language' in values and values['language']!='ru': raise HTTPException(422,'Сейчас доступен русский интерфейс (ru).')
    if 'private' in values and values['private'] is None: raise HTTPException(422,'Укажите настройку приватности.')
    if values.get('avatar'):
        avatar=values['avatar']
        match=re.fullmatch(r'data:image/(png|jpeg|webp);base64,([A-Za-z0-9+/=\r\n]+)',avatar)
        if not match or len(avatar)>700000: raise HTTPException(422,'Фото должно быть PNG, JPEG или WebP до 500 КБ.')
        try:
            raw=base64.b64decode(match.group(2),validate=True)
            if len(raw)>500000: raise ValueError()
            with Image.open(io.BytesIO(raw)) as image:
                if image.width>3000 or image.height>3000 or image.format not in ('PNG','JPEG','WEBP'): raise ValueError()
                image.verify()
        except Exception: raise HTTPException(422,'Не удалось прочитать фото. Выберите корректный PNG, JPEG или WebP.')
    for key,value in values.items(): setattr(user,key,value)
    session.add(user);session.commit()
    return user_dict(user)

@app.get('/api/documents')
def documents(q:str='',sort:str='created',user:User=Depends(current_user),session:Session=Depends(db)):
    docs=list(session.scalars(select(Document).where(Document.user_id==user.id)))
    query=q.strip().casefold()
    if query: docs=[d for d in docs if query in f'{d.title} {d.author} {d.genre}'.casefold()]
    if sort=='created': docs.sort(key=lambda d:d.created_at,reverse=True)
    elif sort in ('title','author'): docs.sort(key=lambda d:getattr(d,sort).casefold())
    else: raise HTTPException(422,'Сортировка: created, title или author.')
    return [summary(d) for d in docs]

def create_document(values,user,session):
    try: content=validate_content(values.pop('content'))
    except ValueError as exc: raise HTTPException(422,str(exc))
    title=values.get('title','').strip()
    if not title: raise HTTPException(422,'Укажите название текста.')
    values['title']=title
    doc=Document(id=uid(),user_id=user.id,**values,content=content,analysis=analyze(content))
    session.add(doc);session.commit()
    return full(doc)

@app.post('/api/documents',status_code=201)
def create(data:DocumentInput,user:User=Depends(current_user),session:Session=Depends(db)):
    return create_document(data.model_dump(),user,session)

@app.post('/api/documents/upload',status_code=201)
def upload(file:UploadFile=File(...),title:str=Form(''),author:str=Form(''),year:str=Form(''),genre:str=Form('Художественный'),user:User=Depends(current_user),session:Session=Depends(db)):
    data=file.file.read(MAX_BYTES+1)
    if len(data)>MAX_BYTES: raise HTTPException(413,'Файл больше 12 МБ.')
    try:
        content=parse_file(file.filename or '',data)
        yr=int(year) if year.strip() else None
        model=DocumentInput(title=title.strip() or Path(file.filename or 'Текст').stem,author=author,year=yr,genre=genre,content=content)
    except ValueError as exc: raise HTTPException(422,str(exc))
    return create_document(model.model_dump(),user,session)

@app.get('/api/documents/{document_id}')
def get_document(document_id:str,user:User=Depends(current_user),session:Session=Depends(db)):
    return full(owned(document_id,user,session))

@app.patch('/api/documents/{document_id}')
def update_document(document_id:str,data:DocumentPatch,user:User=Depends(current_user),session:Session=Depends(db)):
    doc=owned(document_id,user,session)
    values=data.model_dump(exclude_unset=True)
    for key,value in values.items():
        if key in ('title','author','genre'):
            if value is None or (key=='title' and not value.strip()): raise HTTPException(422,'Название и строковые поля не могут быть пустыми null.')
            value=value.strip()
        setattr(doc,key,value)
    session.commit()
    return full(doc)

@app.delete('/api/documents/{document_id}')
def remove_document(document_id:str,user:User=Depends(current_user),session:Session=Depends(db)):
    doc=owned(document_id,user,session);session.delete(doc);session.commit();return {'ok':True}

@app.get('/api/documents/{document_id}/notes')
def notes(document_id:str,user:User=Depends(current_user),session:Session=Depends(db)):
    owned(document_id,user,session)
    return [note_dict(n) for n in session.scalars(select(Note).where(Note.document_id==document_id).order_by(Note.created_at.desc()))]

@app.post('/api/documents/{document_id}/notes',status_code=201)
def create_note(document_id:str,data:NoteInput,user:User=Depends(current_user),session:Session=Depends(db)):
    doc=owned(document_id,user,session)
    if not data.text.strip(): raise HTTPException(422,'Напишите заметку.')
    if data.entity_id and not any(e['id']==data.entity_id for e in doc.analysis['entities']): raise HTTPException(422,'Сущность не найдена в этом тексте.')
    if data.fragment and data.fragment not in doc.content: raise HTTPException(422,'Выбранного фрагмента нет в тексте.')
    note=Note(id=uid(),document_id=document_id,**data.model_dump())
    session.add(note);session.commit();return note_dict(note)

@app.delete('/api/documents/{document_id}/notes/{note_id}')
def remove_note(document_id:str,note_id:str,user:User=Depends(current_user),session:Session=Depends(db)):
    owned(document_id,user,session)
    note=session.get(Note,note_id)
    if not note or note.document_id!=document_id: raise HTTPException(404,'Заметка не найдена.')
    session.delete(note);session.commit();return {'ok':True}

@app.get('/api/documents/{document_id}/questions')
def questions(document_id:str,user:User=Depends(current_user),session:Session=Depends(db)):
    owned(document_id,user,session)
    return [question_dict(q) for q in session.scalars(select(Question).where(Question.document_id==document_id).order_by(Question.created_at.desc()))]

@app.post('/api/documents/{document_id}/questions',status_code=201)
def create_question(document_id:str,data:QuestionInput,user:User=Depends(current_user),session:Session=Depends(db)):
    doc=owned(document_id,user,session)
    if not data.question.strip(): raise HTTPException(422,'Напишите вопрос.')
    answer,sources=answer_question(data.question,doc.analysis)
    question=Question(id=uid(),document_id=document_id,question=data.question.strip(),answer=answer,sources=sources,mode='extractive')
    session.add(question);session.commit();return question_dict(question)

@app.get('/api/documents/{document_id}/export.pdf')
def pdf(document_id:str,user:User=Depends(current_user),session:Session=Depends(db)):
    doc=owned(document_id,user,session)
    return Response(export_pdf(doc),media_type='application/pdf',headers={'Content-Disposition':'attachment; filename="semantic-atlas-report.pdf"'})

@app.get('/api/documents/{document_id}/export.png')
def png(document_id:str,user:User=Depends(current_user),session:Session=Depends(db)):
    doc=owned(document_id,user,session)
    return Response(export_png(doc),media_type='image/png',headers={'Content-Disposition':'attachment; filename="semantic-atlas-graph.png"'})

FRONTEND=Path(__file__).resolve().parents[2]/'frontend'/'dist'
if (FRONTEND/'assets').is_dir(): app.mount('/assets',StaticFiles(directory=FRONTEND/'assets'),name='assets')

@app.get('/{path:path}',include_in_schema=False)
def spa(path:str):
    if path.startswith('api/') or path=='api': raise HTTPException(404,'Маршрут API не найден.')
    # Only known build files are served; never map an arbitrary user path onto disk.
    if path in ('favicon.svg',) and (FRONTEND/path).is_file(): return FileResponse(FRONTEND/path)
    if (FRONTEND/'index.html').is_file(): return FileResponse(FRONTEND/'index.html')
    return JSONResponse(status_code=503,content={'detail':'Интерфейс ещё не собран. Выполните npm install и npm run build в frontend.'})
