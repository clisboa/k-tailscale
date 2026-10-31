FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    zbar-tools g++ make pkg-config qtbase5-dev qtbase5-dev-tools python3 binutils procps \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
ENV QT_QPA_PLATFORM=offscreen
CMD ["make", "test"]
