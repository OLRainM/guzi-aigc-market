FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir --index-url https://pypi.org/simple --default-timeout=120 --retries=5 -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
