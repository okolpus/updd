# Semantic Atlas API contract

Base /api. Same-origin browser. Auth via HttpOnly cookie (JWT with server-side session revocation). All errors JSON {detail:string}. JSON field names below are authoritative. Server sync analysis on create; browser shows loading.

POST /auth/register {login,name,password} -> {user}; POST /auth/login {login,password} -> {user}; POST /auth/demo {} -> {user,document_id}; POST /auth/logout -> {ok:true}. GET /auth/me -> user directly {id,login,name,language,private,avatar}. PATCH /profile {name,language,private,avatar} -> user. avatar is validated data:image PNG/JPEG/WebP (limit).

GET /documents?q=&sort=created|title|author -> array of summaries {id,title,author,year,genre,created_at,word_count,entity_count,edge_count,status}. POST /documents JSON {title,author,year:null,genre,content} -> full document. POST /documents/upload multipart file,title,author,year,genre -> full document. GET /documents/{id} -> full. PATCH /documents/{id} {title,author,year,genre} -> full. DELETE /documents/{id} -> {ok:true}.

Full document = summary plus {content,analysis:{method,limitations,entities:[{id,label,type:person|concept|place|event,count,description,quotes:[{text,chapter}]}],edges:[{id,source,target,type:family|social|thematic|causal|temporal|cooccurrence,label,weight,evidence:[string],inferred:true}],chapters:[{id,title,text,word_count,themes:[string],entity_counts:{entityId:number},event_count}],keywords:[{word,count}],word_count}}. Entity ids strings. Chapter field in quote string chapter title. Graph entities max 70 for readable layout, all chapters included. Analytical method disclosed in UI. All edges evidence-backed hypotheses; co-occurrence must never claim kinship/causality.

GET /documents/{id}/notes -> array {id,entity_id:null,fragment:null,text,created_at}. POST same {text,entity_id:null,fragment:null} -> note. DELETE /documents/{id}/notes/{note_id} -> {ok:true}.
GET /documents/{id}/questions -> array {id,question,answer,sources:[{text,chapter}],mode,created_at}. POST same {question} -> question object. Extractive QA only (explicit no generative model); cite actual source fragments, don't invent motive/plot.

GET /documents/{id}/export.pdf -> binary PDF (Cyrillic capable font); GET /documents/{id}/export.png -> binary PNG graph/report. Frontend downloads via fetch blob auth cookie. GET /health -> {status:ok,database:ok}.

Demo is isolated random account and original Russian expedition short story 5 chapters. Normal registration empty library. Foreign IDs 404 for all nested routes. Deleted docs cascade notes/questions. Password PBKDF2/scrypt hash, session cookie SameSite=Lax; configurable Secure behind HTTPS. SQLite local, PostgreSQL via DATABASE_URL. Max file 12MB; max text 500000 words, EPUB decompression limits, text PDFs only reject scanned PDFs with useful message. No arbitrary file paths or outbound LLM calls.

Frontend: React18+Vite, D3 force graph, lucide-react. Build dist served by FastAPI. Routes managed SPA internal state (no router needed). Sidebar library and methodology/settings; graph/reader/analytics/notes tabs; question panel. Empty/error/loading states, profile edit, upload, meta edit/delete, search/sort, source quotes entity inspector, zoom/pan/filter, PDF/PNG export. Russian UI. Local launch on 127.0.0.1:8765.
