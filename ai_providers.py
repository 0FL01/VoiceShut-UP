import os
from groq import AsyncGroq
import google.generativeai as genai
from dotenv import load_dotenv

load_dotenv()

GROQ_API_KEY = os.getenv("GROQ_API_KEY")
GOOGLE_API_KEY = os.getenv("GOOGLE_API_KEY")

groq_client = AsyncGroq(api_key=GROQ_API_KEY)
genai.configure(api_key=GOOGLE_API_KEY)

async def summarize_text_groq(text: str) -> str:
    chat_completion = await groq_client.chat.completions.create(
        messages=[
            {
                "role": "system",
                "content": "Вы - высококвалифицированный ассистент по обработке и анализу текста, специализирующийся на создании кратких и информативных резюме голосовых сообщений. Ваши ответы всегда должны быть на русском языке. Избегайте использования эмодзи, смайликов и разговорных выражений, таких как 'говорящий' или 'говоритель'. При форматировании текста используйте следующие обозначения: * **жирный текст** для выделения ключевых понятий * *курсив* для обозначения важных, но второстепенных деталей * ```python для обозначения начала и конца блоков кода * * в начале строки для создания маркированных списков. Ваша задача - создавать краткие, но содержательные резюме, выделяя наиболее важную информацию и ключевые моменты из предоставленного текста. Стремитесь к ясности и лаконичности изложения, сохраняя при этом основной смысл и контекст исходного сообщения."
            },
            {
                "role": "user",
                "content": f"""
                Ваша цель - обработать и проанализировать следующий текст, полученный из расшифровки голосового сообщения:

                {text}

                Пожалуйста, создайте краткое резюме, соблюдая следующие правила:
                1. Начните резюме с горизонтальной линии (---) для визуального разделения.
                2. Ограничьте абстрактное резюме максимум шестью предложениями.
                3. Выделите **жирным шрифтом** ключевые слова и фразы в каждом предложении.
                4. Если в тексте присутствуют числовые данные или статистика, включите их в резюме, выделив *курсивом*.
                5. Определите основную тему или темы сообщения и укажите их в начале резюме.
                6. Если в тексте есть какие-либо действия или рекомендации, выделите их в отдельный маркированный список.
                7. В конце резюме добавьте короткий параграф (2-3 предложения) с аналитическим заключением или выводом на основе содержания сообщения.

                Создайте краткое резюме текста. Ваше резюме должно быть информативным, структурированным и легким для быстрого восприятия.
                """
            }
        ],
        model="gemma2-9b-it",
        temperature=0.5,
        max_tokens=4096,
        top_p=1,
        stream=False
    )
    return chat_completion.choices[0].message.content

async def summarize_text_gemini(text: str) -> str:
    model = genai.GenerativeModel('gemini-1.5-pro')
    prompt = f"""
    Вы - высококвалифицированный ассистент по обработке и анализу текста, специализирующийся на создании кратких и информативных резюме голосовых сообщений. Ваши ответы всегда должны быть на русском языке. Избегайте использования эмодзи, смайликов и разговорных выражений, таких как 'говорящий' или 'говоритель'. При форматировании текста используйте следующие обозначения: * **жирный текст** для выделения ключевых понятий * *курсив* для обозначения важных, но второстепенных деталей * ```python для обозначения начала и конца блоков кода * * в начале строки для создания маркированных списков. Ваша задача - создавать краткие, но содержательные резюме, выделяя наиболее важную информацию и ключевые моменты из предоставленного текста. Стремитесь к ясности и лаконичности изложения, сохраняя при этом основной смысл и контекст исходного сообщения.

    Ваша цель - обработать и проанализировать следующий текст, полученный из расшифровки голосового сообщения:

    {text}

    Пожалуйста, создайте краткое резюме, соблюдая следующие правила:
    1. Начните резюме с горизонтальной линии (---) для визуального разделения.
    2. Ограничьте абстрактное резюме максимум шестью предложениями.
    3. Выделите **жирным шрифтом** ключевые слова и фразы в каждом предложении.
    4. Если в тексте присутствуют числовые данные или статистика, включите их в резюме, выделив *курсивом*.
    5. Определите основную тему или темы сообщения и укажите их в начале резюме.
    6. Если в тексте есть какие-либо действия или рекомендации, выделите их в отдельный маркированный список.
    7. В конце резюме добавьте короткий параграф (2-3 предложения) с аналитическим заключением или выводом на основе содержания сообщения.

    Создайте краткое резюме текста. Ваше резюме должно быть информативным, структурированным и легким для быстрого восприятия.
    """
    response = await model.generate_content_async(prompt)
    return response.text

async def audio_to_text(file_path: str) -> str:
    with open(file_path, "rb") as audio_file:
        transcript = await groq_client.audio.transcriptions.create(
            file=(os.path.basename(file_path), audio_file),
            model="whisper-large-v3"
        )
    return transcript.text