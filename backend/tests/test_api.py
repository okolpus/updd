import io
import base64
import uuid
import zipfile

from fastapi.testclient import TestClient
from pypdf import PdfReader
from PIL import Image
from reportlab.pdfgen import canvas

TEXT = '''Глава 1. Письмо
Анна встретила Ивана в Томске. Иван передал Анне письмо о научной экспедиции.
Мария изучала карту города Томска. Анна и Мария обсуждали исследование реки.
Иван рассказал Марии об экспедиции и передал карту. Томск стал отправной точкой путешествия.

Глава 2. Экспедиция
Анна и Иван приехали на реку Обь. Мария ждала Ивана на берегу реки.
Исследование воды помогло экспедиции. Иван и Мария записали результаты исследования.
Анна привезла карту из Томска. Экспедиция завершилась, и Мария отправила Анне письмо.
'''


def register(client, login=None):
    payload = {"login": login or "user_" + uuid.uuid4().hex[:12], "name": "Читатель", "password": "good-password-42"}
    r = client.post("/api/auth/register", json=payload)
    assert r.status_code == 200 or r.status_code == 201, r.text
    return payload, r.json()["user"]


def document(client, content=TEXT):
    r = client.post("/api/documents", json={"title": "Экспедиция", "author": "Учебный пример", "year": 2026, "genre": "Художественный", "content": content})
    assert r.status_code in (200, 201), r.text
    return r.json()


def test_auth_profile_and_revoked_cookie(client):
    assert client.get("/api/auth/me").status_code == 401
    payload, user = register(client)
    assert client.get("/api/auth/me").json()["id"] == user["id"]
    assert any("HttpOnly" in c._rest for c in client.cookies.jar)
    cookie = client.headers.get("cookie") or "; ".join(f"{k}={v}" for k, v in client.cookies.items())
    r = client.patch("/api/profile", json={"name": "Новое имя", "language": "ru", "private": False})
    assert r.status_code == 200, r.text
    assert r.json()["name"] == "Новое имя"
    assert client.post("/api/auth/logout").status_code == 200
    assert client.get("/api/auth/me", headers={"Cookie": cookie}).status_code == 401
    assert client.post("/api/auth/login", json={"login": payload["login"], "password": "wrong-password"}).status_code == 401
    assert client.post("/api/auth/login", json=payload).status_code == 200
    assert client.get("/api/auth/me").json()["name"] == "Новое имя"


def test_duplicate_and_validation(client):
    payload, _ = register(client)
    r = client.post("/api/auth/register", json=payload)
    assert r.status_code in (400, 409), r.text
    assert client.post("/api/documents", json={"title": "Пусто", "content": "   "}).status_code in (400, 422)
    assert client.patch("/api/profile", json={"avatar": "data:image/svg+xml;base64,PHN2Zz4="}).status_code in (400, 422)
    assert client.patch("/api/profile", json={"avatar": "https://example.com/a.png"}).status_code in (400, 422)


def test_document_analysis_crud_and_search(client):
    register(client)
    d = document(client)
    a = d["analysis"]
    assert a["method"] and a["limitations"]
    assert len(a["chapters"]) >= 2
    assert a["entities"] and a["edges"]
    assert d["word_count"] > 30
    content = d["content"]
    ids = {e["id"] for e in a["entities"]}
    for e in a["entities"]:
        assert e["type"] in ("person", "concept", "place", "event")
        assert e["count"] >= 1
        for q in e["quotes"]:
            assert q["text"] in content
    for e in a["edges"]:
        assert e["source"] in ids and e["target"] in ids
        assert e["inferred"] is True
        assert e["evidence"]
        assert all(q in content for q in e["evidence"])
    assert client.get(f'/api/documents/{d["id"]}').json()["analysis"] == a
    assert any(x["id"] == d["id"] for x in client.get("/api/documents?q=Экспедиция&sort=title").json())
    assert client.get("/api/documents?q=неттакоготекста").json() == []
    r = client.patch(f'/api/documents/{d["id"]}', json={"title": "Новое название", "author": "Другой автор", "year": None, "genre": "Научный"})
    assert r.status_code == 200, r.text
    assert r.json()["title"] == "Новое название"
    assert client.delete(f'/api/documents/{d["id"]}').status_code == 200
    assert client.get(f'/api/documents/{d["id"]}').status_code == 404


def test_cross_user_access_isolation(client, initialized_app):
    register(client)
    d = document(client)
    path = f'/api/documents/{d["id"]}'
    n = client.post(path + "/notes", json={"text": "Личная заметка"}).json()
    with TestClient(initialized_app) as other:
        register(other)
        assert other.get("/api/documents").json() == []
        checks = [("GET", path, None), ("PATCH", path, {"title": "Взлом"}), ("DELETE", path, None),
                  ("GET", path + "/notes", None), ("POST", path + "/notes", {"text": "Нет"}),
                  ("DELETE", path + f'/notes/{n["id"]}', None), ("GET", path + "/questions", None),
                  ("POST", path + "/questions", {"question": "Где Иван?"}),
                  ("GET", path + "/export.pdf", None), ("GET", path + "/export.png", None)]
        for method, url, body in checks:
            r = other.request(method, url, json=body)
            assert r.status_code == 404, (method, url, r.status_code, r.text)


def test_notes_and_questions_are_persistent_and_sourced(client):
    register(client)
    d = document(client)
    path = f'/api/documents/{d["id"]}'
    e = d["analysis"]["entities"][0]
    r = client.post(path + "/notes", json={"text": "Проверить связь", "entity_id": e["id"]})
    assert r.status_code in (200, 201), r.text
    note = r.json()
    assert any(n["id"] == note["id"] for n in client.get(path + "/notes").json())
    q = client.post(path + "/questions", json={"question": "Что делал Иван?"})
    assert q.status_code in (200, 201), q.text
    answer = q.json()
    assert answer["answer"] and answer["mode"] and answer["sources"]
    assert all(s["text"] in d["content"] for s in answer["sources"])
    assert len(client.get(path + "/questions").json()) == 1
    assert client.post(path + "/notes", json={"text": "   "}).status_code in (400, 422)
    assert client.post(path + "/notes", json={"text": "x", "entity_id": "nonexistent-entity"}).status_code in (400, 404, 422)
    assert client.delete(path + f'/notes/{note["id"]}').status_code == 200
    assert client.get(path + "/notes").json() == []


def test_upload_txt_and_epub(client):
    register(client)
    r = client.post("/api/documents/upload", files={"file": ("story.txt", TEXT.encode("utf-8"), "text/plain")}, data={"title": "Файл", "author": "Автор", "genre": "Учебный"})
    assert r.status_code in (200, 201), r.text
    assert "Анна" in r.json()["content"]
    epub = io.BytesIO()
    with zipfile.ZipFile(epub, "w") as z:
        z.writestr("mimetype", "application/epub+zip")
        z.writestr("META-INF/container.xml", '<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>')
        z.writestr("OEBPS/content.opf", '<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata/><manifest><item id="one" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="one"/></spine></package>')
        z.writestr("OEBPS/chapter.xhtml", '<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Глава 1</h1><p>' + TEXT + '</p></body></html>')
    r = client.post("/api/documents/upload", files={"file": ("story.epub", epub.getvalue(), "application/epub+zip")}, data={"title": "EPUB"})
    assert r.status_code in (200, 201), r.text
    assert "Иван" in r.json()["content"]


def test_pdf_upload_and_exports(client):
    register(client)
    d = document(client)
    path = f'/api/documents/{d["id"]}'
    r = client.get(path + "/export.pdf")
    assert r.status_code == 200, r.text
    assert r.content.startswith(b"%PDF")
    pdf = PdfReader(io.BytesIO(r.content))
    assert "Экспедиция" in "".join(p.extract_text() for p in pdf.pages)
    r = client.get(path + "/export.png")
    assert r.status_code == 200
    im = Image.open(io.BytesIO(r.content))
    assert im.width >= 600 and im.height >= 400
    data = io.BytesIO()
    c = canvas.Canvas(data)
    c.drawString(50, 700, "Anna met Ivan in Tomsk. Ivan gave Anna a letter about science.")
    c.save()
    r = client.post("/api/documents/upload", files={"file": ("story.pdf", data.getvalue(), "application/pdf")}, data={"title": "PDF"})
    assert r.status_code in (200, 201), r.text
    assert "Ivan" in r.json()["content"]


def test_rejected_files_and_safe_profile_photo(client):
    register(client)
    for name, data in [("bad.exe", b"executable"), ("bad.pdf", b"not a PDF"), ("bad.epub", b"not a zip"), ("empty.txt", b"   ")]:
        r = client.post("/api/documents/upload", files={"file": (name, data)}, data={"title": "Ошибка"})
        assert r.status_code in (400, 413, 415, 422), (name, r.status_code, r.text)
    r = client.post("/api/documents/upload", files={"file": ("huge.txt", b"x" * (13 * 1024 * 1024))}, data={"title": "Большой"})
    assert r.status_code == 413, r.text[:300]
    data = io.BytesIO()
    canvas.Canvas(data).save()
    r = client.post("/api/documents/upload", files={"file": ("scan.pdf", data.getvalue())}, data={"title": "Пустой PDF"})
    assert r.status_code in (400, 422)


def test_demo_and_health(client):
    assert client.get("/api/health").json()["status"] == "ok"
    r = client.post("/api/auth/demo", json={})
    assert r.status_code in (200, 201), r.text
    d = client.get(f'/api/documents/{r.json()["document_id"]}').json()
    assert len(d["analysis"]["chapters"]) >= 5
    assert d["analysis"]["entities"] and d["analysis"]["edges"]


def test_origin_and_profile_photo(client):
    register(client)
    assert client.post("/api/auth/logout", headers={"Origin": "https://unrelated.example"}).status_code == 403
    assert client.get("/api/auth/me").status_code == 200
    photo = io.BytesIO()
    Image.new("RGB", (24, 24), "blue").save(photo, "PNG")
    avatar = "data:image/png;base64," + base64.b64encode(photo.getvalue()).decode()
    r = client.patch("/api/profile", json={"avatar": avatar})
    assert r.status_code == 200, r.text
    assert client.get("/api/auth/me").json()["avatar"] == avatar


def test_epub_decompression_limit(client):
    register(client)
    data = io.BytesIO()
    with zipfile.ZipFile(data, "w", compression=zipfile.ZIP_DEFLATED) as z:
        z.writestr("huge.xhtml", "x" * (13 * 1024 * 1024))
    r = client.post("/api/documents/upload", files={"file": ("bomb.epub", data.getvalue())}, data={"title": "Сжатый"})
    assert r.status_code in (400, 413, 422), r.text


def test_unknown_question_and_fragment_validation(client):
    register(client)
    d = document(client)
    path = f'/api/documents/{d["id"]}'
    r = client.post(path + "/questions", json={"question": "Квазиплазменная нейтронизация?"})
    assert r.status_code in (200, 201), r.text
    assert r.json()["sources"] == []
    assert client.post(path + "/notes", json={"text": "Заметка", "fragment": "Такого фрагмента тут нет"}).status_code == 422
    assert client.patch(path, json={"title": None}).status_code == 422


def test_epub_parent_paths_preserve_all_chapters(client):
    register(client)
    data = io.BytesIO()
    with zipfile.ZipFile(data, "w") as z:
        z.writestr("META-INF/container.xml", '<container><rootfiles><rootfile full-path="OEBPS/pkg/content.opf"/></rootfiles></container>')
        z.writestr("OEBPS/pkg/content.opf", '<package><manifest><item id="one" href="../Text/ch1.xhtml"/><item id="two" href="../Text/ch2.xhtml"/></manifest><spine><itemref idref="one"/><itemref idref="two"/></spine></package>')
        z.writestr("OEBPS/Text/ch1.xhtml", '<html><body><h1>Глава 1</h1><p>Анна встретила Ивана на берегу реки.</p></body></html>')
        z.writestr("OEBPS/Text/ch2.xhtml", '<html><body><h1>Глава 2</h1><p>Мария передала Анне письмо из Томска.</p></body></html>')
    r = client.post("/api/documents/upload", files={"file": ("valid.epub", data.getvalue())}, data={"title": "Главы"})
    assert r.status_code == 201, r.text
    assert "Анна встретила Ивана" in r.json()["content"]
    assert "Мария передала Анне" in r.json()["content"]
    assert len(r.json()["analysis"]["chapters"]) == 2
    broken = io.BytesIO()
    with zipfile.ZipFile(data) as old, zipfile.ZipFile(broken, "w") as z:
        for name in old.namelist():
            if not name.endswith("ch2.xhtml"):
                z.writestr(name, old.read(name))
    r = client.post("/api/documents/upload", files={"file": ("broken.epub", broken.getvalue())}, data={"title": "Пропавшая глава"})
    assert r.status_code == 422, r.text
