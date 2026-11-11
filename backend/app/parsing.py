from __future__ import annotations
import io
import re
import zipfile
import posixpath
from html.parser import HTMLParser
from pathlib import PurePosixPath
from xml.etree import ElementTree as ET
from pypdf import PdfReader

MAX_BYTES = 12 * 1024 * 1024
MAX_WORDS = 500_000
MAX_CHARS = 12_000_000

class TextExtractor(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parts = []
        self.hidden = 0
    def handle_starttag(self, tag, attrs):
        if tag in ('script', 'style'): self.hidden += 1
        if tag in ('p', 'div', 'br', 'h1', 'h2', 'h3', 'li'): self.parts.append('\n')
    def handle_endtag(self, tag):
        if tag in ('script', 'style'): self.hidden = max(0, self.hidden - 1)
        if tag in ('p', 'div', 'h1', 'h2', 'h3', 'li'): self.parts.append('\n')
    def handle_data(self, data):
        if not self.hidden: self.parts.append(data)

def validate_content(text: str) -> str:
    text = text.replace('\x00', '').replace('\r\n', '\n').strip()
    if len(text) > MAX_CHARS:
        raise ValueError('Текст слишком большой. Максимум 12 миллионов символов.')
    words = re.findall(r'[\wЁё]+', text)
    if len(words) > MAX_WORDS:
        raise ValueError('В тексте больше 500 000 слов. Разделите его на несколько частей.')
    if len(words) < 3:
        raise ValueError('Нужно хотя бы три слова для анализа.')
    return text

def parse_file(filename: str, data: bytes) -> str:
    if len(data) > MAX_BYTES: raise ValueError('Файл больше 12 МБ.')
    suffix = PurePosixPath(filename).suffix.lower()
    if suffix == '.txt':
        try: text = data.decode('utf-8-sig')
        except UnicodeDecodeError:
            try: text = data.decode('cp1251')
            except UnicodeDecodeError: raise ValueError('Не удалось прочитать TXT. Сохраните его в UTF-8.')
    elif suffix == '.pdf':
        try:
            reader = PdfReader(io.BytesIO(data))
            if reader.is_encrypted: raise ValueError('PDF защищён паролем. Загрузите незашифрованный файл.')
            if len(reader.pages) > 1500: raise ValueError('В PDF больше 1500 страниц. Разделите документ.')
            chunks = []
            total = 0
            for page in reader.pages:
                chunk = page.extract_text() or ''
                total += len(chunk)
                if total > MAX_CHARS: raise ValueError('В PDF слишком много текста.')
                chunks.append(chunk)
            text = '\n\n'.join(chunks)
            if not text.strip(): raise ValueError('В PDF нет текстового слоя. Сначала распознайте скан с помощью OCR.')
        except ValueError: raise
        except Exception as exc: raise ValueError('Не удалось прочитать PDF. Проверьте, что файл не повреждён.') from exc
    elif suffix == '.epub':
        try:
            with zipfile.ZipFile(io.BytesIO(data)) as archive:
                infos = archive.infolist()
                if len(infos) > 2500 or sum(x.file_size for x in infos) > 40 * 1024 * 1024:
                    raise ValueError('EPUB слишком велик после распаковки. Разделите книгу.')
                if any(x.file_size > 12 * 1024 * 1024 or (x.file_size > 1_000_000 and x.file_size / max(x.compress_size, 1) > 250) for x in infos):
                    raise ValueError('EPUB содержит чрезмерно сжатые или большие элементы.')
                paths = {x.filename for x in infos}
                if 'META-INF/container.xml' not in paths: raise ValueError('В EPUB отсутствует описание контейнера.')
                container = ET.fromstring(archive.read('META-INF/container.xml'))
                rootfile = container.find('.//{*}rootfile')
                if rootfile is None: raise ValueError('Некорректная структура EPUB.')
                opf_path = rootfile.attrib.get('full-path', '')
                if opf_path not in paths: raise ValueError('В EPUB не найдено оглавление.')
                opf = ET.fromstring(archive.read(opf_path))
                base = PurePosixPath(opf_path).parent
                manifest = {x.attrib['id']: x.attrib.get('href', '') for x in opf.findall('.//{*}manifest/{*}item') if 'id' in x.attrib}
                ordered = [manifest.get(x.attrib.get('idref', ''), '') for x in opf.findall('.//{*}spine/{*}itemref')]
                if not ordered: raise ValueError('В EPUB отсутствует порядок глав.')
                chunks = []
                from urllib.parse import unquote, urlsplit
                for href in ordered:
                    link = urlsplit(href)
                    if link.scheme or link.netloc:
                        raise ValueError('EPUB содержит внешнюю ссылку вместо главы.')
                    path = posixpath.normpath(posixpath.join(str(base), unquote(link.path)))
                    if path.startswith('../') or path.startswith('/') or path not in paths:
                        raise ValueError('В EPUB не найдена одна из глав. Проверьте целостность книги.')
                    parser = TextExtractor()
                    parser.feed(archive.read(path).decode('utf-8-sig'))
                    chunks.append(''.join(parser.parts).strip())
                text = '\n\n'.join(chunks)
        except ValueError: raise
        except Exception as exc: raise ValueError('Не удалось прочитать EPUB. Проверьте структуру файла.') from exc
    else:
        raise ValueError('Поддерживаются только TXT, PDF и EPUB.')
    return validate_content(text)
