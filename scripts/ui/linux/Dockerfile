# Ubuntu 24.04 with what the Linux app needs to build and run on a virtual
# display, and what a UI check needs to drive and photograph it.
FROM ubuntu:24.04
ARG TARGETARCH
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential pkg-config curl ca-certificates git jq sqlite3 python3 \
    libgtk-4-dev libadwaita-1-dev librsvg2-common \
    xvfb xauth x11-utils xdotool xclip imagemagick \
    dbus dbus-x11 at-spi2-core python3-gi gir1.2-atspi-2.0 \
    fonts-cantarell fonts-dejavu-core adwaita-icon-theme \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL "https://go.dev/dl/go1.25.3.linux-${TARGETARCH}.tar.gz" | tar -C /usr/local -xz
RUN curl -fsSL https://sh.rustup.rs | sh -s -- -y --profile minimal
ENV PATH=/root/.cargo/bin:/usr/local/go/bin:$PATH
