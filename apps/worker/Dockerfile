FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir \
    --index-url https://mirrors.aliyun.com/pypi/simple/ \
    --trusted-host mirrors.aliyun.com \
    --default-timeout=120 \
    --retries=8 \
    -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
