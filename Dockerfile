# Используем официальный образ Python 3.10 Alpine в качестве базового образа
FROM python:3.10-alpine

# Устанавливаем зависимости для работы без сохранения кэша
RUN apk add --no-cache \
    gcc \
    musl-dev \
    alsa-lib \
    alsa-lib-dev \
    ffmpeg \
    build-base

# Создаем рабочую директорию
WORKDIR /app

# Копируем файлы в контейнер
COPY requirements.txt bot.py ./

# Устанавливаем зависимости без сохранения кэша
RUN pip install --upgrade pip \
    && pip install --no-cache-dir -r requirements.txt

# Устанавливаем переменные окружения для корректной работы aiogram
ENV PYTHONUNBUFFERED=1

# Запускаем скрипт
CMD ["python", "bot.py"]

