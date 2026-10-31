FROM ghcr.io/pgaskin/nickeltc@sha256:3296eb400d270cfd26f5773ab09625d4469f85680b3a191951dfb78d8ade7840
WORKDIR /src
COPY src /src/src
COPY Makefile /src/Makefile
RUN make -j2 arm
