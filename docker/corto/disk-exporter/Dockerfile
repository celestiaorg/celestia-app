FROM python:3.13-alpine3.21
COPY exporter.py /opt/exporter.py
USER 10001
EXPOSE 9101
CMD ["python3", "/opt/exporter.py"]
