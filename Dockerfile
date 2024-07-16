# Используем официальный образ Python 3.10 в качестве базового образа
FROM python:3.10-slim

# Устанавливаем ffmpeg, необходимый для работы с аудио
RUN apt-get update && apt-get install -y ffmpeg

# Устанавливаем зависимости, необходимые для Pydub
RUN apt-get install -y libasound2-dev

# Устанавливаем зависимости для установки библиотек Python
RUN apt-get install -y gcc

# Создаем рабочую директорию
WORKDIR /app

# Копируем файлы в контейнер
COPY requirements.txt requirements.txt
COPY . .

# Устанавливаем зависимости
RUN pip install --upgrade pip
RUN pip install -r requirements.txt

# Устанавливаем переменные окружения для корректной работы aiogram
ENV PYTHONUNBUFFERED=1

# Запускаем скрипт
CMD ["python", "bot.py"]
