FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN addgroup --system collector \
    && adduser --system --ingroup collector --home /app collector

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY collector.py .

RUN mkdir -p /data \
    && chown -R collector:collector /app /data

USER collector

CMD ["python", "/app/collector.py"]
