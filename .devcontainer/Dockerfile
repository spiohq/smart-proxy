FROM mcr.microsoft.com/devcontainers/base:ubuntu

# Install build essentials for CGO (needed for SQLite/SQLCipher)
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    gcc \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*
