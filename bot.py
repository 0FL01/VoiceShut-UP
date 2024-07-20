import asyncio
import io
import os
import html
import aiohttp
import subprocess
from aiogram import Bot, Dispatcher, F, Router, types
from aiogram.types import Message, Voice, Video, Document, Animation, Sticker
from aiogram.utils.markdown import hbold, hitalic
from aiogram.filters import Command
from groq import AsyncGroq
from pydub import AudioSegment
import moviepy.editor as mp
from dotenv import load_dotenv

# Загрузка переменных окружения из файла .env
load_dotenv()

GROQ_API_KEY = os.getenv("GROQ_API_KEY")
BOT_TOKEN = os.getenv("BOT_TOKEN")
MAX_MESSAGE_LENGTH = 4096
MAX_FILE_SIZE = 20 * 1024 * 1024  # 20 MB in bytes

router: Router = Router()
groq_client = AsyncGroq(api_key=GROQ_API_KEY)

async def audio_to_text(file_path: str) -> str:
    """Принимает путь к аудио файлу, возвращает текст файла."""
    with open(file_path, "rb") as audio_file:
        transcript = await groq_client.audio.transcriptions.create(
            file=(os.path.basename(file_path), audio_file),
            model="whisper-large-v3"
        )
    return transcript.text

async def convert_oga_to_mp3(input_path: str, output_path: str):
    """Конвертирует .oga файл в .mp3 с помощью ffmpeg."""
    command = ['ffmpeg', '-y', '-i', input_path, '-acodec', 'libmp3lame', output_path]
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
    os.makedirs('audio_files', exist_ok=True)
    input_path = f"audio_files/audio-{file_unique_id}"
    output_path = f"audio_files/audio-{file_unique_id}.mp3"

    with open(input_path, 'wb') as f:
        f.write(file_content.getvalue())

    if file_info.file_path.lower().endswith('.oga'):
        await convert_oga_to_mp3(input_path, output_path)
    else:
        audio = AudioSegment.from_file(input_path, format=file_info.file_path.split('.')[-1])
        audio.export(output_path, format="mp3")

    os.remove(input_path)  # Удаляем временный файл
    return output_path

async def save_video_as_mp3(bot: Bot, video: Video) -> str:
    """Скачивает видео и сохраняет аудиодорожку в формате mp3."""
    if video.file_size > MAX_FILE_SIZE:
        raise ValueError("Файл слишком большой")

    video_file_info = await bot.get_file(video.file_id)
    video_file = io.BytesIO()
    await bot.download_file(video_file_info.file_path, video_file)
    os.makedirs('video_files', exist_ok=True)
    video_path = f"video_files/video-{video.file_unique_id}.mp4"
    with open(video_path, "wb") as f:
        f.write(video_file.getvalue())

    video_clip = mp.VideoFileClip(video_path)
    audio_path = f"video_files/audio-{video.file_unique_id}.mp3"
    video_clip.audio.write_audiofile(audio_path, codec='libmp3lame', ffmpeg_params=['-y'])
    video_clip.close()

    os.remove(video_path)  # Удаляем временный видеофайл
    return audio_path

async def summarize_text(text: str) -> str:
    """Создает краткое резюме текста с использованием доступной модели Groq."""
    chat_completion = await groq_client.chat.completions.create(
        messages=[
            {"role": "system", "content": "Отвечаешь всегда на русском языке, ты ассистент, пишешь краткое суммарайз данной голосовухи, в краткой сводке можешь использовать стиль общения как в источнике, не используй смайлики или emoji."},
            {"role": "user", "content": f"Напиши пересказ текста, но при этом сохрани все ключевые детали, не более 50 слов: {text}"}
        ],
        model="gemma2-9b-it",
        temperature=1,
        max_tokens=1024,
        top_p=1,
        stream=False
    )
    return chat_completion.choices[0].message.content

def split_message(message: str, max_length: int) -> list:
    """Разбивает сообщение на части длиной не более max_length."""
    return [message[i:i + max_length] for i in range(0, len(message), max_length)]

@router.message(Command("start"))
async def cmd_start(message: types.Message):
    """Обработчик команды /start"""
    welcome_message = (
        "Привет! Я бот, который может транскрибировать и суммировать голосовые сообщения, видео и аудиофайлы. "
        "Просто отправь мне голосовое сообщение, видео или аудиофайл (mp3, wav, oga), и я преобразую его в текст и создам краткое резюме.\n\n"
        "P.S Данный бот работает на мощностях Groq, использует две модели: whisper-large-v3 и gemma2-9b-it\n\n"
        f"Важно: максимальный размер файла для обработки - {MAX_FILE_SIZE // (1024 * 1024)} МБ."
    )
    await message.answer(welcome_message)

async def process_audio(message: Message, bot: Bot, audio_path: str):
    """Обрабатывает аудио, выполняет транскрипцию и суммаризацию."""
    try:
        transcripted_text = await audio_to_text(audio_path)
        if transcripted_text:
            summary = await summarize_text(transcripted_text)
            response = (
                f"{hbold('Transcription:')}\n\n"
                f"{transcripted_text}\n\n"
                f"{hbold('Summary:')}\n\n"
                f"<i>{html.escape(summary)}</i>"
            )
            messages = split_message(response, MAX_MESSAGE_LENGTH)
            for msg in messages:
                await message.reply(text=msg, parse_mode="HTML")
    finally:
        # Удаляем аудиофайл после обработки, независимо от результата
        os.remove(audio_path)

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
        await message.reply(f"Произошла ошибка при обработке аудио: {str(e)}")
        # В случае ошибки также удаляем файл, если он был создан
        if 'audio_path' in locals():
            os.remove(audio_path)

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
        await message.reply(f"Произошла ошибка при обработке видео: {str(e)}")
        # В случае ошибки также удаляем файл, если он был создан
        if 'audio_path' in locals():
            os.remove(audio_path)

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
