FROM ghcr.io/pgaskin/nickeltc@sha256:3296eb400d270cfd26f5773ab09625d4469f85680b3a191951dfb78d8ade7840
RUN printf 'deb https://archive.debian.org/debian buster main\n' > /etc/apt/sources.list \
    && apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends bc lzop \
    && rm -rf /var/lib/apt/lists/*
COPY kernel.tar.bz2 /kernel.tar.bz2
RUN echo 'b504fe838004476890a4246d93bd5e1fb94af0359515b1645ae385efae7b3667  /kernel.tar.bz2' | sha256sum -c - \
    && tar -xjf /kernel.tar.bz2 -C / && rm /kernel.tar.bz2
COPY clara-hd.config /kernel/.config
WORKDIR /kernel
RUN scripts/config --module TUN --set-str LOCALVERSION '-00136-g12655eaaef89' --disable LOCALVERSION_AUTO \
    && : > .scmversion \
    && make ARCH=arm CROSS_COMPILE=arm-nickel-linux-gnueabihf- olddefconfig \
    && make -j4 ARCH=arm CROSS_COMPILE=arm-nickel-linux-gnueabihf- \
       KBUILD_BUILD_USER=k-tailscale KBUILD_BUILD_HOST=builder \
       KBUILD_BUILD_TIMESTAMP='2022-05-06 06:04:51 UTC' KBUILD_BUILD_VERSION=68 zImage modules \
    && mkdir /output && cp drivers/net/tun.ko /output/tun.ko \
    && arm-nickel-linux-gnueabihf-strip --strip-debug /output/tun.ko
