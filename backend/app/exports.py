from __future__ import annotations
import io
import math
from pathlib import Path
from html import escape

from PIL import Image, ImageDraw, ImageFont
from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle, KeepTogether

FONT = Path(__file__).parent/'assets'/'DejaVuSans.ttf'
COLORS = {'person':'#2c6bba','concept':'#288574','place':'#b37b34','event':'#9865b8'}

def export_pdf(document):
    if 'Atlas' not in pdfmetrics.getRegisteredFontNames(): pdfmetrics.registerFont(TTFont('Atlas',str(FONT)))
    out = io.BytesIO()
    styles = {
        'title':ParagraphStyle('title',fontName='Atlas',fontSize=22,leading=28,spaceAfter=14,textColor=colors.HexColor('#172d42')),
        'heading':ParagraphStyle('heading',fontName='Atlas',fontSize=14,leading=20,spaceBefore=16,spaceAfter=8),
        'body':ParagraphStyle('body',fontName='Atlas',fontSize=10,leading=14,spaceAfter=6),
        'small':ParagraphStyle('small',fontName='Atlas',fontSize=8,leading=11.5,spaceAfter=4),
    }
    analysis = document.analysis
    story = [Paragraph(escape(document.title),styles['title']),Paragraph(escape(document.author or 'Автор не указан'),styles['body']),
             Paragraph(f'Слов: {analysis["word_count"]} · Сущностей: {len(analysis["entities"])} · Связей: {len(analysis["edges"])} · Разделов: {len(analysis["chapters"])}',styles['body']),
             Paragraph('Метод и ограничения',styles['heading']),Paragraph(escape(analysis['method']),styles['body']),Paragraph(escape(analysis['limitations']),styles['body']),
             Paragraph('Основные сущности',styles['heading'])]
    labels = {'person':'Персонаж','concept':'Понятие','place':'Место','event':'Событие'}
    rows = [[Paragraph(x,styles['small']) for x in ('Сущность','Тип','Упоминания')]]
    for entity in sorted(analysis['entities'],key=lambda e:e['count'],reverse=True):
        rows.append([Paragraph(escape(entity['label']),styles['small']),Paragraph(labels[entity['type']],styles['small']),str(entity['count'])])
    table = Table(rows,colWidths=[260,130,85],repeatRows=1,hAlign='LEFT')
    table.setStyle(TableStyle([('FONTNAME',(0,0),(-1,-1),'Atlas'),('FONTSIZE',(0,0),(-1,-1),9),('BACKGROUND',(0,0),(-1,0),colors.HexColor('#e9eef3')),('GRID',(0,0),(-1,-1),0.5,colors.HexColor('#d9d9d9')),('VALIGN',(0,0),(-1,-1),'MIDDLE'),('TOPPADDING',(0,0),(-1,-1),7),('BOTTOMPADDING',(0,0),(-1,-1),7),('ALIGN',(-1,1),(-1,-1),'CENTER')]))
    story.append(table)
    story.append(Paragraph('Структура текста',styles['heading']))
    for chapter in analysis['chapters']:
        story.extend([Paragraph(escape(chapter['title']),styles['body']),Paragraph(f'Слов: {chapter["word_count"]}. Темы: {escape(", ".join(chapter["themes"]) or "не определены")}.',styles['small'])])
    story.append(Paragraph('Связи и подтверждающие фрагменты',styles['heading']))
    story.append(Paragraph(f'Показаны {min(30,len(analysis["edges"]))} наиболее частых связей из {len(analysis["edges"])}.',styles['small']))
    by_id = {e['id']:e for e in analysis['entities']}
    for edge in analysis['edges'][:30]:
        block=[Paragraph(escape(f'{by_id[edge["source"]]["label"]} — {by_id[edge["target"]]["label"]}: {edge["label"]} (предположение)'),styles['body'])]
        if edge['evidence']: block.append(Paragraph(escape(edge['evidence'][0]),styles['small']))
        story.append(KeepTogether(block))
    def footer(canvas,doc):
        canvas.setFont('Atlas',8)
        canvas.setFillColor(colors.HexColor('#576477'))
        canvas.drawString(60,28,'Семантический атлас · Аналитический отчёт')
        canvas.drawRightString(A4[0]-60,28,str(doc.page))
    SimpleDocTemplate(out,pagesize=A4,rightMargin=60,leftMargin=60,topMargin=55,bottomMargin=50,title=document.title,author='Семантический атлас').build(story,onFirstPage=footer,onLaterPages=footer)
    return out.getvalue()

def export_png(document):
    """Readable complete graph export using non-overlapping, type-grouped slots."""
    w = 2400
    background = '#f7f9fc'
    font = ImageFont.truetype(str(FONT), 27)
    small = ImageFont.truetype(str(FONT), 22)
    title_font = ImageFont.truetype(str(FONT), 43)
    group_font = ImageFont.truetype(str(FONT), 31)
    measure = ImageDraw.Draw(Image.new('RGB', (1, 1)))

    def wrap(value, face, width):
        # Measure real glyph boxes rather than estimating character widths.
        lines, line = [], ''
        for word in str(value).split():
            candidate = f'{line} {word}'.strip()
            if measure.textbbox((0, 0), candidate, font=face)[2] <= width:
                line = candidate
                continue
            if line:
                lines.append(line)
                line = ''
            while measure.textbbox((0, 0), word, font=face)[2] > width:
                split = 1
                while split < len(word) and measure.textbbox((0, 0), word[:split+1], font=face)[2] <= width:
                    split += 1
                lines.append(word[:split])
                word = word[split:]
            line = word
        if line:
            lines.append(line)
        return lines or ['']

    analysis = document.analysis
    entities = sorted(analysis['entities'], key=lambda e:e['count'], reverse=True)
    groups = [('person','Персонажи'),('place','Места'),('event','События'),('concept','Понятия')]
    title_lines = wrap(document.title, title_font, w-120)
    header_end = 55 + len(title_lines)*57 + 100
    columns, margin = 6, 65
    slot_width = (w-margin*2)/columns
    cursor = header_end + 25
    sections, slots, coords = [], [], {}
    for kind, label in groups:
        members = [e for e in entities if e['type']==kind]
        if not members:
            continue
        sections.append((cursor, kind, label, len(members)))
        cursor += 75
        for offset in range(0, len(members), columns):
            row = members[offset:offset+columns]
            wrapped = [wrap(e['label'], font, slot_width-25) for e in row]
            row_height = max(180, 112+max(len(lines) for lines in wrapped)*36)
            for col, (entity, lines) in enumerate(zip(row, wrapped)):
                x = margin+(col+.5)*slot_width
                y = cursor+35
                coords[entity['id']] = (x,y)
                slots.append((entity,x,y,lines))
            cursor += row_height
        cursor += 30
    h = max(650, int(cursor+125))
    image = Image.new('RGB', (w,h), background)
    draw = ImageDraw.Draw(image)
    for i,line in enumerate(title_lines):
        draw.text((60,45+i*57),line,fill='#172d42',font=title_font)
    baseline = 55+len(title_lines)*57
    draw.text((60,baseline),f'Семантический атлас · {analysis["word_count"]} слов · {len(entities)} из {len(analysis["entities"])} сущностей · {len(analysis["edges"])} связей',fill='#576477',font=font)
    draw.text((60,baseline+42),'Узлы сгруппированы по типу. Связи являются предположениями по тексту.',fill='#576477',font=small)

    # Links are deliberately subordinate; every node keeps a protected, measured label slot.
    for edge in analysis['edges']:
        if edge['source'] in coords and edge['target'] in coords:
            draw.line((coords[edge['source']],coords[edge['target']]),fill='#dce3ed',width=min(3,1+edge['weight']//4))
    for y,kind,label,count in sections:
        draw.rectangle((45,y-5,w-45,y+55),fill=background)
        draw.ellipse((60,y+8,82,y+30),fill=COLORS[kind])
        draw.text((98,y),f'{label} · {count}',fill='#24384e',font=group_font)
        draw.line((60,y+52,w-60,y+52),fill='#d9e1ea',width=1)
    for entity,x,y,lines in slots:
        radius = min(33,18+math.sqrt(entity['count'])*2)
        draw.ellipse((x-radius-6,y-radius-6,x+radius+6,y+radius+6),fill=background)
        draw.ellipse((x-radius,y-radius,x+radius,y+radius),fill=COLORS[entity['type']])
        label_top = y+48
        label_height = len(lines)*36+33
        # Only this entity's dedicated cell can be covered by its label background.
        left, right = x-slot_width/2+8, x+slot_width/2-8
        draw.rectangle((left,label_top-3,right,label_top+label_height),fill=background)
        for i,line in enumerate(lines):
            text_width = draw.textbbox((0,0),line,font=font)[2]
            draw.text((x-text_width/2,label_top+i*36),line,font=font,fill='#24384e')
        count_text = f'Упоминаний: {entity["count"]}'
        text_width = draw.textbbox((0,0),count_text,font=small)[2]
        draw.text((x-text_width/2,label_top+len(lines)*36+2),count_text,font=small,fill='#576477')
    draw.rectangle((45,h-105,w-45,h-25),fill=background)
    draw.text((60,h-94),'Локальный эвристический анализ. Проверяйте предполагаемые связи по цитатам в приложении.',font=small,fill='#576477')
    draw.text((60,h-56),'Все выделенные сущности показаны. Размер узла отражает число упоминаний.',font=small,fill='#576477')
    stream=io.BytesIO()
    image.save(stream,format='PNG')
    return stream.getvalue()
