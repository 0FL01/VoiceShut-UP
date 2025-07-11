import asyncio
import io
import os
import html
import re
import aiohttp
import subprocess
from aiogram import Bot, Dispatcher, F, Router, types
from aiogram.types import Message, Voice, Video, Document, Animation, Sticker, VideoNote
from aiogram.filters import Command
from google import genai
from google.genai import types as genai_types
from pydub import AudioSegment
from dotenv import load_dotenv
import tempfile
import logging
from html.parser import HTMLParser
from functools import wraps


# Настройка логирования
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Загрузка переменных окружения из файла .env
load_dotenv()

GOOGLE_API_KEY = os.getenv("GOOGLE_API_KEY")
BOT_TOKEN = os.getenv("BOT_TOKEN")
MAX_MESSAGE_LENGTH = 4096
MAX_FILE_SIZE = 20 * 1024 * 1024  # 20 MB in bytes

router: Router = Router()
gemini_client = genai.Client(api_key=GOOGLE_API_KEY)

class HTMLValidator(HTMLParser):
    def __init__(self):
        super().__init__()
        self.tags = []

    def handle_starttag(self, tag, attrs):
        self.tags.append(tag)

    def handle_endtag(self, tag):
        if self.tags and self.tags[-1] == tag:
            self.tags.pop()
        else:
            self.tags.append(f"/{tag}")

    def is_valid(self):
        return len(self.tags) == 0

def validate_html(html_string):
    validator = HTMLValidator()
    validator.feed(html_string)
    return validator.is_valid()

def format_html(text):
    def code_block_replacer(match):
        code = match.group(2)
        language = match.group(1) or ''
        escaped_code = html.escape(code.strip())
        return f'<pre><code class="{language}">{escaped_code}</code></pre>'

    # Replace code blocks with triple backticks
    text = re.sub(r'```(\w+)?\n(.*?)```', code_block_replacer, text, flags=re.DOTALL)

    # Replace code blocks with single backticks
    text = re.sub(r'`(\w+)\n(.*?)`', code_block_replacer, text, flags=re.DOTALL)

    # Replace list markers with HTML tags
    text = re.sub(r'^\* ', '• ', text, flags=re.MULTILINE)

    # Replace bold text
    text = re.sub(r'\*\*(.*?)\*\*', r'<b>\1</b>', text)

    # Replace italic text
    text = re.sub(r'\*(.*?)\*', r'<i>\1</i>', text)

    # Replace inline code
    text = re.sub(r'`(.*?)`', r'<code>\1</code>', text)

    # Remove Markdown-style headers (##, ###, etc.)
    text = re.sub(r'^#+\s+', '', text, flags=re.MULTILINE)

    # Escape special HTML characters in the remaining text
    text = html.escape(text, quote=False)

    # Unescape the HTML tags we've explicitly added
    text = re.sub(r'&lt;(/?(?:b|strong|i|em|u|ins|s|strike|del|code|pre|tg-spoiler))&gt;', r'<\1>', text)

    return text

def retry_on_api_error(max_retries=5, delay=3):
    """Декоратор для повторных попыток при ошибках API"""
    def decorator(func):
        @wraps(func)
        async def wrapper(*args, **kwargs):
            last_exception = None
            
            for attempt in range(max_retries):
                try:
                    return await func(*args, **kwargs)
                except Exception as e:
                    last_exception = e
                    error_str = str(e).lower()
                    
                    # Проверяем на временные ошибки API
                    if any(code in error_str for code in ['503', '429', '500', 'overloaded', 'unavailable', 'timeout']):
                        if attempt < max_retries - 1:  # Не последняя попытка
                            logger.warning(f"API error on attempt {attempt + 1}/{max_retries}: {str(e)}. Retrying in {delay} seconds...")
                            await asyncio.sleep(delay)
                            continue
                    
                    # Если это не временная ошибка или последняя попытка, поднимаем исключение
                    raise e
            
            # Если все попытки исчерпаны
            raise last_exception
        return wrapper
    return decorator

def retry_with_model_fallback():
    """Декоратор с fallback на резервную модель"""
    def decorator(func):
        @wraps(func)
        async def wrapper(*args, **kwargs):
            # Сначала пытаемся с gemini-2.5-flash
            primary_model = 'gemini-2.5-flash'
            fallback_model = 'gemini-2.0-flash-001'
            
            # 3 попытки с основной моделью
            for attempt in range(3):
                try:
                    logger.info(f"Attempting with {primary_model}, attempt {attempt + 1}/3")
                    return await func(*args, model=primary_model, **kwargs)
                except Exception as e:
                    error_str = str(e).lower()
                    logger.warning(f"Error with {primary_model} on attempt {attempt + 1}: {str(e)}")
                    
                    if attempt < 2:  # Не последняя попытка с основной моделью
                        await asyncio.sleep(2)
                        continue
                    else:
                        logger.info(f"All attempts with {primary_model} failed, switching to {fallback_model}")
                        break
            
            # Переключаемся на резервную модель с ретраями
            last_exception = None
            for attempt in range(5):
                try:
                    logger.info(f"Attempting with {fallback_model}, attempt {attempt + 1}/5")
                    return await func(*args, model=fallback_model, **kwargs)
                except Exception as e:
                    last_exception = e
                    error_str = str(e).lower()
                    
                    # Проверяем на временные ошибки API
                    if any(code in error_str for code in ['503', '429', '500', 'overloaded', 'unavailable', 'timeout']):
                        if attempt < 4:  # Не последняя попытка
                            logger.warning(f"API error with {fallback_model} on attempt {attempt + 1}/5: {str(e)}. Retrying in 3 seconds...")
                            await asyncio.sleep(3)
                            continue
                    
                    # Если это не временная ошибка или последняя попытка, поднимаем исключение
                    raise e
            
            # Если все попытки исчерпаны
            raise last_exception
        return wrapper
    return decorator

@retry_with_model_fallback()
async def audio_to_text(file_path: str, model: str = 'gemini-2.5-flash') -> str:
    """Принимает путь к аудио файлу, возвращает текст файла используя Gemini."""
    try:
        logger.info(f"Starting audio transcription with model {model}")
        logger.info(f"Reading audio file: {file_path}")
        
        with open(file_path, "rb") as audio_file:
            audio_data = audio_file.read()
        
        audio_size_mb = len(audio_data) / (1024 * 1024)
        logger.info(f"Audio file size: {audio_size_mb:.2f} MB")
        
        # Определяем MIME тип на основе расширения файла
        file_extension = os.path.splitext(file_path)[1].lower()
        mime_type_map = {
            '.mp3': 'audio/mpeg',
            '.wav': 'audio/wav',
            '.oga': 'audio/ogg',
            '.ogg': 'audio/ogg',
            '.m4a': 'audio/mp4',
            '.aac': 'audio/aac'
        }
        mime_type = mime_type_map.get(file_extension, 'audio/mpeg')
        logger.info(f"Detected MIME type: {mime_type}")
        
        # Создаем Part с аудио данными
        logger.info("Creating audio part for Gemini API")
        audio_part = genai_types.Part.from_bytes(
            data=audio_data,
            mime_type=mime_type
        )
        
        # Используем Gemini для транскрипции
        logger.info(f"Sending transcription request to {model}")
        response = await asyncio.to_thread(
            lambda: gemini_client.models.generate_content(
                model=model,
                contents=[
                    "Пожалуйста, транскрибируйте этот аудио файл в текст на том языке, на котором говорят в записи. Верните только текст транскрипции без дополнительных комментариев.",
                    audio_part
                ]
            )
        )
        
        # Проверяем, что ответ содержит текст
        if not response or not response.text:
            logger.warning(f"Empty or None response from {model}")
            raise Exception("Получен пустой ответ от API. Возможно, аудио не содержит речи или модель не смогла его обработать.")
        
        transcript_length = len(response.text)
        logger.info(f"Transcription completed successfully. Text length: {transcript_length} characters")
        return response.text
    except Exception as e:
        logger.error(f"Error in audio_to_text with model {model}: {str(e)}")
        raise Exception(f"Ошибка при транскрипции аудио: {str(e)}")

async def convert_oga_to_mp3(input_path: str, output_path: str):
    """Конвертирует .oga файл в .mp3 с помощью ffmpeg."""
    logger.info(f"Converting OGA to MP3: {input_path} -> {output_path}")
    command = ['ffmpeg', '-y', '-i', input_path, '-c:a', 'libmp3lame', '-q:a', '3', '-ac', '1', '-ar', '22050', output_path]
    process = await asyncio.create_subprocess_exec(
        *command,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE
    )
    stdout, stderr = await process.communicate()
    if process.returncode != 0:
        logger.error(f"FFmpeg conversion failed: {stderr.decode()}")
        raise Exception(f"FFmpeg error: {stderr.decode()}")
    logger.info("OGA to MP3 conversion completed successfully")

async def save_audio_as_mp3(bot: Bot, file: types.File, file_id: str, file_unique_id: str) -> str:
    """Скачивает аудио файл и сохраняет в формате mp3."""
    logger.info(f"Starting audio download and conversion. File ID: {file_id}")
    
    file_info = await bot.get_file(file_id)
    logger.info(f"File info retrieved. Path: {file_info.file_path}, Size: {file_info.file_size} bytes")
    
    file_content = io.BytesIO()
    logger.info("Downloading file content...")
    await bot.download_file(file_info.file_path, file_content)
    logger.info("File download completed")
    
    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp3') as temp_file:
        temp_path = temp_file.name

    if file_info.file_path.lower().endswith('.oga'):
        logger.info("Processing OGA file format")
        with tempfile.NamedTemporaryFile(delete=False, suffix='.oga') as input_file:
            input_file.write(file_content.getvalue())
            input_path = input_file.name
        await convert_oga_to_mp3(input_path, temp_path)
        os.unlink(input_path)
    else:
        logger.info("Processing non-OGA audio format with pydub")
        audio = AudioSegment.from_file(io.BytesIO(file_content.getvalue()), format=file_info.file_path.split('.')[-1])
        audio.export(temp_path, format="mp3")
        logger.info("Audio conversion with pydub completed")

    logger.info(f"Audio saved as MP3: {temp_path}")
    return temp_path

async def save_video_as_mp3(bot: Bot, video: Video) -> str:
    """Скачивает видео файл и сохраняет аудиодорожку в формате mp3."""
    logger.info(f"Starting video processing. Video duration: {video.duration}s, Size: {video.file_size} bytes")
    
    if video.file_size > MAX_FILE_SIZE:
        raise ValueError("Файл слишком большой")

    video_file_info = await bot.get_file(video.file_id)
    logger.info(f"Video file info retrieved: {video_file_info.file_path}")
    
    video_file = io.BytesIO()
    logger.info("Downloading video file...")
    await bot.download_file(video_file_info.file_path, video_file)
    logger.info("Video download completed")
    
    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp4') as video_temp_file:
        video_temp_file.write(video_file.getvalue())
        video_path = video_temp_file.name

    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp3') as audio_temp_file:
        audio_path = audio_temp_file.name

    logger.info(f"Extracting audio from video using ffmpeg: {video_path} -> {audio_path}")
    # Используем ffmpeg для извлечения аудио из видео
    command = ['ffmpeg', '-y', '-i', video_path, '-vn', '-acodec', 'libmp3lame', '-q:a', '2', audio_path]
    process = await asyncio.create_subprocess_exec(
        *command,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE
    )
    stdout, stderr = await process.communicate()
    if process.returncode != 0:
        logger.error(f"FFmpeg video processing failed: {stderr.decode()}")
        raise Exception(f"FFmpeg error: {stderr.decode()}")

    logger.info("Audio extraction from video completed successfully")
    os.unlink(video_path)
    return audio_path

async def save_video_note_as_mp3(bot: Bot, video_note: VideoNote) -> str:
    """Скачивает видео-заметку (кружочек) и сохраняет аудиодорожку в формате mp3."""
    logger.info(f"Starting video note processing. Duration: {video_note.duration}s, Size: {video_note.file_size} bytes")
    
    if video_note.file_size > MAX_FILE_SIZE:
        raise ValueError("Файл слишком большой")

    video_file_info = await bot.get_file(video_note.file_id)
    logger.info(f"Video note file info retrieved: {video_file_info.file_path}")
    
    video_file = io.BytesIO()
    logger.info("Downloading video note...")
    await bot.download_file(video_file_info.file_path, video_file)
    logger.info("Video note download completed")
    
    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp4') as video_temp_file:
        video_temp_file.write(video_file.getvalue())
        video_path = video_temp_file.name

    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp3') as audio_temp_file:
        audio_path = audio_temp_file.name

    logger.info(f"Extracting audio from video note using ffmpeg: {video_path} -> {audio_path}")
    # Используем ffmpeg для извлечения аудио из видео
    command = ['ffmpeg', '-y', '-i', video_path, '-vn', '-acodec', 'libmp3lame', '-q:a', '2', audio_path]
    process = await asyncio.create_subprocess_exec(
        *command,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE
    )
    stdout, stderr = await process.communicate()
    if process.returncode != 0:
        logger.error(f"FFmpeg video note processing failed: {stderr.decode()}")
        raise Exception(f"FFmpeg error: {stderr.decode()}")

    logger.info("Audio extraction from video note completed successfully")
    os.unlink(video_path)
    return audio_path

@router.message(F.content_type == "video_note")
async def process_video_note_message(message: Message, bot: Bot):
    """Обрабатывает видео-заметки (кружочки), извлекает аудио, транскрибирует в текст и создает резюме."""
    if message.video_note.file_size > MAX_FILE_SIZE:
        await message.reply(f"Извините, максимальный размер файла - {MAX_FILE_SIZE // (1024 * 1024)} МБ. Ваша видео-заметка слишком большая.")
        return

    await message.reply("Обрабатываю ваше видео, это может занять некоторое время...")
    try:
        audio_path = await save_video_note_as_mp3(bot, message.video_note)
        await process_audio(message, bot, audio_path)
    except Exception as e:
        logger.error(f"Error processing video note message: {str(e)}")
        await message.reply(f"Произошла ошибка при обработке видео-кружка: {str(e)}")


@retry_with_model_fallback()
async def summarize_text(text: str, model: str = 'gemini-2.5-flash') -> str:
    """Создает краткое резюме текста с использованием Google Gemini."""
    logger.info(f"Starting text summarization with model {model}")
    logger.info(f"Input text length: {len(text)} characters")
    
    system_prompt = """Вы - высококвалифицированный ассистент по обработке и анализу текста, специализирующийся на создании кратких и информативных резюме голосовых сообщений. Ваши ответы всегда должны быть на русском языке. Избегайте использования эмодзи, смайликов и разговорных выражений, таких как 'говорящий' или 'говоритель'. При форматировании текста используйте следующие обозначения: 
    * **жирный текст** для выделения ключевых понятий 
    * *курсив* для обозначения важных, но второстепенных деталей 
    * ```python для обозначения начала и конца блоков кода 
    * * в начале строки для создания маркированных списков. 
    Ваша задача - создавать краткие, но содержательные резюме, выделяя наиболее важную информацию и ключевые моменты из предоставленного текста. Стремитесь к ясности и лаконичности изложения, сохраняя при этом основной смысл и контекст исходного сообщения."""

    user_prompt = f"""Ваша цель - обработать и проанализировать следующий текст, полученный из расшифровки голосового сообщения:
    {text}
    Пожалуйста, создайте краткое резюме, соблюдая следующие правила:
    1. Начните резюме с горизонтальной линии (---) для визуального разделения.
    2. Ограничьте абстрактное резюме максимум шестью предложениями.
    3. Выделите жирным шрифтом ключевые слова и фразы в каждом предложении.
    4. Если в тексте присутствуют числовые данные или статистика, включите их в резюме, выделив курсивом.
    5. Определите основную тему или темы сообщения и укажите их в начале резюме.
    6. Если в тексте есть какие-либо действия или рекомендации, выделите их в отдельный маркированный список.
    7. В конце резюме добавьте короткий параграф (2-3 предложения) с аналитическим заключением или выводом на основе содержания сообщения."""

    try:
        logger.info(f"Sending summarization request to {model}")
        response = await asyncio.to_thread(
            lambda: gemini_client.models.generate_content(
                model=model,
                contents=[
                    genai_types.UserContent(
                        parts=[genai_types.Part.from_text(text=system_prompt)]
                    ),
                    genai_types.ModelContent(
                        parts=[genai_types.Part.from_text(text="Понял, буду следовать указанным правилам форматирования и структуры.")]
                    ),
                    genai_types.UserContent(
                        parts=[genai_types.Part.from_text(text=user_prompt)]
                    )
                ]
            )
        )
        
        # Проверяем, что ответ содержит текст
        if not response or not response.text:
            logger.warning(f"Empty or None response from {model} during summarization")
            return "Ошибка: Получен пустой ответ от API при создании резюме."
        
        summary_length = len(response.text)
        logger.info(f"Summarization completed successfully. Summary length: {summary_length} characters")
        return response.text
    except Exception as e:
        logger.error(f"Error in summarize_text with model {model}: {str(e)}")
        return f"Ошибка при создании резюме: {str(e)}"


def split_message(message: str, max_length: int) -> list:
    """Разбивает сообщение на части длиной не более max_length."""
    if len(message) <= max_length:
        return [message]
    
    parts = []
    current_part = ""
    paragraphs = message.split('\n\n')
    
    for paragraph in paragraphs:
        if len(current_part) + len(paragraph) + 2 <= max_length:
            if current_part:
                current_part += '\n\n'
            current_part += paragraph
        else:
            if current_part:
                parts.append(current_part)
            current_part = paragraph
            
            # Если параграф больше max_length, разбиваем его
            while len(current_part) > max_length:
                split_point = current_part[:max_length].rfind(' ')
                if split_point == -1:
                    split_point = max_length
                parts.append(current_part[:split_point])
                current_part = current_part[split_point:].lstrip()
    
    if current_part:
        parts.append(current_part)
    
    return parts

async def send_formatted_message(message: Message, text: str, title: str = None, parse_mode: str = "HTML", use_spoiler: bool = False):
    """Отправляет отформатированное сообщение с учетом максимальной длины."""
    if title:
        text = f"<b>{title}</b>\n\n{text}"
    
    if use_spoiler:
        # Оборачиваем весь текст после заголовка в тег спойлера
        parts = text.split('\n\n', 1)  # Разделяем заголовок и содержимое
        if len(parts) > 1:
            text = f"{parts[0]}\n\n<tg-spoiler>{parts[1]}</tg-spoiler>"
        else:
            text = f"<tg-spoiler>{text}</tg-spoiler>"

    if not validate_html(text):
        logger.warning(f"Invalid HTML detected for {title}. Falling back to plain text.")
        parse_mode = None
        if title:
            text = f"{title}\n\n{text}"
    
    messages = split_message(text, MAX_MESSAGE_LENGTH)
    sent_messages = []
    
    for msg in messages:
        sent_msg = await message.reply(text=msg, parse_mode=parse_mode)
        sent_messages.append(sent_msg)
    
    return sent_messages


@router.message(Command("start"))
async def cmd_start(message: types.Message):
    """Обработчик команды /start"""
    welcome_message = (
        "Привет! Я бот, который может транскрибировать и суммировать голосовые сообщения, видео и аудиофайлы. "
        "Просто отправь мне голосовое сообщение, видео или аудиофайл (mp3, wav, oga), и я преобразую его в текст и создам краткое резюме.\n\n"
        "P.S Данный бот работает на мощностях Google Gemini AI, использует модель gemini-2.0-flash для транскрипции и суммаризации\n\n"
        f"Важно: максимальный размер файла для обработки - {MAX_FILE_SIZE // (1024 * 1024)} МБ."
    )
    await message.answer(welcome_message)


async def process_audio(message: Message, bot: Bot, audio_path: str):
    """Обрабатывает аудио, выполняет транскрипцию и суммаризацию."""
    logger.info(f"Starting audio processing pipeline for message {message.message_id}")
    
    try:
        # Получаем транскрипцию
        logger.info("Phase 1: Starting transcription")
        transcripted_text = await audio_to_text(audio_path)
        if transcripted_text:
            logger.info("Phase 1: Transcription completed, sending to user")
            # Отправляем транскрипцию
            formatted_transcript = html.escape(transcripted_text)
            await send_formatted_message(message, formatted_transcript, "Transcription")
            logger.info("Phase 1: Transcription sent to user")
            
            # Получаем и отправляем резюме
            logger.info("Phase 2: Starting summarization")
            summary = await summarize_text(transcripted_text)
            logger.info("Phase 2: Summarization completed, formatting and sending")
            formatted_summary = format_html(summary)
            await send_formatted_message(
                message, 
                formatted_summary, 
                "Summary", 
                use_spoiler=True  # Включаем спойлер для summary
            )
            logger.info("Phase 2: Summary sent to user")
            logger.info(f"Audio processing pipeline completed successfully for message {message.message_id}")
    
    except Exception as e:
        logger.error(f"Error processing audio for message {message.message_id}: {str(e)}")
        await message.reply(f"Произошла ошибка при обработке аудио: {str(e)}")
    finally:
        if os.path.exists(audio_path):
            logger.info(f"Cleaning up temporary audio file: {audio_path}")
            os.unlink(audio_path)


@router.message(F.content_type.in_({"voice", "audio", "document", "video", "video_note"}))
async def process_media_message(message: Message, bot: Bot):
    """Обрабатывает голосовые сообщения, аудио, документы, видео и кружочки-заметки."""
    logger.info(f"Received media message {message.message_id} from user {message.from_user.id}")
    
    file = message.voice or message.audio or message.document or message.video or message.video_note
    logger.info(f"Media file details - Size: {file.file_size} bytes, File ID: {file.file_id}")
    
    if file.file_size > MAX_FILE_SIZE:
        logger.warning(f"File too large: {file.file_size} bytes > {MAX_FILE_SIZE} bytes")
        await message.reply(f"Извините, максимальный размер файла - {MAX_FILE_SIZE // (1024 * 1024)} МБ. Ваш файл слишком большой.")
        return

    if message.document and not message.document.file_name.lower().endswith(('.mp3', '.wav', '.oga')):
        logger.warning(f"Unsupported document format: {message.document.file_name}")
        await message.reply("Извините, я могу обрабатывать только аудиофайлы форматов mp3, wav и oga.")
        return

    await message.reply("Обрабатываю ваш медиафайл, это может занять некоторое время...")
    logger.info("Starting media file processing...")
    
    try:
        if message.video:
            logger.info("Processing video file")
            audio_path = await save_video_as_mp3(bot, message.video)
        elif message.video_note:
            logger.info("Processing video note")
            audio_path = await save_video_note_as_mp3(bot, message.video_note)
        else:
            logger.info("Processing audio/voice/document file")
            audio_path = await save_audio_as_mp3(bot, file, file.file_id, file.file_unique_id)
        
        logger.info(f"Media conversion completed, starting audio processing pipeline")
        await process_audio(message, bot, audio_path)
    except Exception as e:
        logger.error(f"Error processing media message {message.message_id}: {str(e)}")
        await message.reply(f"Произошла ошибка при обработке медиафайла: {str(e)}")

@router.message(F.content_type.in_({"animation", "sticker"}))
async def process_unsupported_content(message: Message):
    """Обрабатывает анимации и стикеры."""
    content_type = "анимацией" if isinstance(message.content_type, Animation) else "гифками и стикерами"
    response = (
        f"Извините, я не могу работать с {content_type}. "
        "Я обрабатываю только голосовые сообщения, видео, видео-кружочки и аудиофайлы (mp3, wav, oga). "
        "Пожалуйста, отправьте один из поддерживаемых типов файлов. "
        f"Максимальный размер файла - {MAX_FILE_SIZE // (1024 * 1024)} МБ."
    )
    await message.reply(text=response)

@router.message(F.text)
async def process_text_message(message: Message):
    """Handles incoming text messages"""
    response = (
        "Извините, я работаю только с голосовыми сообщениями, видео и аудиофайлами (mp3, wav, oga). "
        "Пожалуйста, отправьте голосовое сообщение, видео или аудиофайл. "
        f"Максимальный размер файла - {MAX_FILE_SIZE // (1024 * 1024)} МБ."
    )
    await message.reply(text=response)

async def main():
    bot: Bot = Bot(token=BOT_TOKEN)
    dp: Dispatcher = Dispatcher()
    dp.include_router(router)
    await dp.start_polling(bot)

if __name__ == "__main__":
    asyncio.run(main())

