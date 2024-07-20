# Используем официальный образ Python 3.10 в качестве базового образа
FROM python:3.10-slim

# Устанавливаем зависимости для работы
RUN apt-get update && \ 
apt-get install -y libasound2-dev gcc ffmpeg && \
apt-get clean && \
rm -rf /var/lib/apt/lists/*

# Создаем рабочую директорию
WORKDIR /app

# Копируем файлы в контейнер
COPY requirements.txt bot.py ./

# Устанавливаем зависимости
RUN pip install --upgrade pip && pip install -r requirements.txt

# Устанавливаем переменные окружения для корректной работы aiogram
ENV PYTHONUNBUFFERED=1

# Запускаем скрипт
CMD ["python", "bot.py"]
