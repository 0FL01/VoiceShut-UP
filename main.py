import asyncio
import io
import os
import html
import re
import aiohttp
import subprocess
from aiogram import Bot, Dispatcher, F, Router, types
from aiogram.types import Message, Voice, Video, Document, Animation, Sticker, InlineKeyboardMarkup, InlineKeyboardButton
from aiogram.filters import Command
from pydub import AudioSegment
import moviepy.editor as mp
from dotenv import load_dotenv
import tempfile
import logging
from ai_providers import summarize_text_groq, summarize_text_gemini, audio_to_text

# Настройка логирования
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Загрузка переменных окружения из файла .env
load_dotenv()

BOT_TOKEN = os.getenv("BOT_TOKEN")
MAX_MESSAGE_LENGTH = 4096
MAX_FILE_SIZE = 20 * 1024 * 1024  # 20 MB in bytes

router: Router = Router()

# Dictionary to store user preferences
user_preferences = {}

def get_model_keyboard():
    keyboard = InlineKeyboardMarkup(inline_keyboard=[
        [InlineKeyboardButton(text="Gemma2-9B (Groq)", callback_data="model_groq")],
        [InlineKeyboardButton(text="Gemini 1.5 Flash (Google)", callback_data="model_gemini")]
    ])
    return keyboard

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
    text = re.sub(r'&lt;(/?(?:b|strong|i|em|u|ins|s|strike|del|code|pre))&gt;', r'<\1>', text)
    
    return text

async def convert_oga_to_mp3(input_path: str, output_path: str):
    """Конвертирует .oga файл в .mp3 с помощью ffmpeg."""
    command = ['ffmpeg', '-y', '-i', input_path, '-c:a', 'libmp3lame', '-q:a', '3', '-ac', '1', '-ar', '22050', output_path]
    process = await asyncio.create_subprocess_exec(
        *command,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE
    )
    stdout, stderr = await process.communicate()
    if process.returncode != 0:
        raise Exception(f"FFmpeg error: {stderr.decode()}")

async def save_audio_as_mp3(bot: Bot, file: types.File, file_id: str, file_unique_id: str) -> str:
    """Скачивает аудио файл и сохраняет в формате mp3."""
    file_info = await bot.get_file(file_id)
    file_content = io.BytesIO()
    await bot.download_file(file_info.file_path, file_content)
    
    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp3') as temp_file:
        temp_path = temp_file.name

    if file_info.file_path.lower().endswith('.oga'):
        with tempfile.NamedTemporaryFile(delete=False, suffix='.oga') as input_file:
            input_file.write(file_content.getvalue())
            input_path = input_file.name
        await convert_oga_to_mp3(input_path, temp_path)
        os.unlink(input_path)
    else:
        audio = AudioSegment.from_file(io.BytesIO(file_content.getvalue()), format=file_info.file_path.split('.')[-1])
        audio.export(temp_path, format="mp3")

    return temp_path

async def save_video_as_mp3(bot: Bot, video: Video) -> str:
    """Скачивает видео и сохраняет аудиодорожку в формате mp3."""
    if video.file_size > MAX_FILE_SIZE:
        raise ValueError("Файл слишком большой")

    video_file_info = await bot.get_file(video.file_id)
    video_file = io.BytesIO()
    await bot.download_file(video_file_info.file_path, video_file)
    
    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp4') as video_temp_file:
        video_temp_file.write(video_file.getvalue())
        video_path = video_temp_file.name

    with tempfile.NamedTemporaryFile(delete=False, suffix='.mp3') as audio_temp_file:
        audio_path = audio_temp_file.name

    video_clip = mp.VideoFileClip(video_path)
    video_clip.audio.write_audiofile(audio_path, codec='libmp3lame', ffmpeg_params=['-y'])
    video_clip.close()

    os.unlink(video_path)
    return audio_path

async def summarize_text(text: str, user_id: int) -> str:
    """Создает краткое резюме текста с использованием выбранной модели."""
    model = user_preferences.get(user_id, 'groq')
    if model == 'groq':
        return await summarize_text_groq(text)
    else:
        return await summarize_text_gemini(text)

def split_message(message: str, max_length: int) -> list:
    """Разбивает сообщение на части длиной не более max_length."""
    return [message[i:i + max_length] for i in range(0, len(message), max_length)]

@router.message(Command("start"))
async def cmd_start(message: types.Message):
    """Обработчик команды /start"""
    welcome_message = (
        "Привет! Я бот, который может транскрибировать и суммировать голосовые сообщения, "
        "видео и аудиофайлы. Просто отправь мне голосовое сообщение, видео или аудиофайл "
        "(mp3, wav, oga), и я преобразую его в текст и создам краткое резюме.\n\n"
        "Вы можете выбрать модель для суммаризации, используя команду /change_model.\n\n"
        f"Важно: максимальный размер файла для обработки - {MAX_FILE_SIZE // (1024 * 1024)} МБ."
    )
    await message.answer(welcome_message)

@router.message(Command("change_model"))
async def cmd_change_model(message: types.Message):
    """Обработчик команды /change_model"""
    await message.answer("Выберите модель для суммаризации:", reply_markup=get_model_keyboard())

@router.callback_query(F.data.startswith("model_"))
async def process_callback_model(callback_query: types.CallbackQuery):
    """Обработчик выбора модели"""
    user_id = callback_query.from_user.id
    model = callback_query.data.split("_")[1]
    user_preferences[user_id] = model
    model_name = "Gemma2-9B (Groq)" if model == "groq" else "Gemini 1.5 Flash (Google)"
    await callback_query.answer(f"Выбрана модель: {model_name}")
    await callback_query.message.answer(f"Вы выбрали модель: {model_name}")

async def process_audio(message: Message, bot: Bot, audio_path: str):
    """Обрабатывает аудио, выполняет транскрипцию и суммаризацию."""
    try:
        transcripted_text = await audio_to_text(audio_path)
        if transcripted_text:
            summary = await summarize_text(transcripted_text, message.from_user.id)
            formatted_summary = format_html(summary)
            response = (
                f"<b>Transcription:</b>\n\n"
                f"{html.escape(transcripted_text)}\n\n"
                f"<b>Summary:</b>\n\n"
                f"{formatted_summary}"
            )
            messages = split_message(response, MAX_MESSAGE_LENGTH)
            for msg in messages:
                await message.reply(text=msg, parse_mode="HTML")
    except Exception as e:
        logger.error(f"Error processing audio: {str(e)}")
        await message.reply(f"Произошла ошибка при обработке аудио: {str(e)}")
    finally:
        if os.path.exists(audio_path):
            os.unlink(audio_path)

@router.message(F.content_type.in_({"voice", "audio", "document"}))
async def process_audio_message(message: Message, bot: Bot):
    """Обрабатывает голосовые сообщения, аудио и документы (mp3, wav, oga)."""
    file = message.voice or message.audio or message.document
    if file.file_size > MAX_FILE_SIZE:
        await message.reply(f"Извините, максимальный размер файла - {MAX_FILE_SIZE // (1024 * 1024)} МБ. Ваш файл слишком большой.")
        return

    if message.document and not message.document.file_name.lower().endswith(('.mp3', '.wav', '.oga')):
        await message.reply("Извините, я могу обрабатывать только аудиофайлы форматов mp3, wav и oga.")
        return

    await message.reply("Обрабатываю ваш аудиофайл, это может занять некоторое время...")
    try:
        audio_path = await save_audio_as_mp3(bot, file, file.file_id, file.file_unique_id)
        await process_audio(message, bot, audio_path)
    except Exception as e:
        logger.error(f"Error processing audio message: {str(e)}")
        await message.reply(f"Произошла ошибка при обработке аудио: {str(e)}")

@router.message(F.content_type == "video")
async def process_video_message(message: Message, bot: Bot):
    """Принимает все видео сообщения, извлекает аудио, транскрибирует в текст и создает резюме."""
    if message.video.file_size > MAX_FILE_SIZE:
        await message.reply(f"Извините, максимальный размер файла - {MAX_FILE_SIZE // (1024 * 1024)} МБ. Ваше видео слишком большое.")
        return

    await message.reply("Обрабатываю ваше видео, это может занять некоторое время...")
    try:
        audio_path = await save_video_as_mp3(bot, message.video)
        await process_audio(message, bot, audio_path)
    except Exception as e:
        logger.error(f"Error processing video message: {str(e)}")
        await message.reply(f"Произошла ошибка при обработке видео: {str(e)}")

@router.message(F.content_type.in_({"animation", "sticker"}))
async def process_unsupported_content(message: Message):
    """Обрабатывает анимации и стикеры."""
    content_type = "анимацией" if isinstance(message.content_type, Animation) else "гифками и стикерами"
    response = (
        f"Извините, я не могу работать с {content_type}. "
        "Я обрабатываю только голосовые сообщения, видео и аудиофайлы (mp3, wav, oga). "
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
