# Reproducible Linux build environment for the AppImage.
#
# Defaults assume a mainland China network: the base image comes from the
# DaoCloud mirror, apt from Tsinghua, Go from Aliyun, Node from Tsinghua and
# appimagetool through gh-proxy. Override the ARGs to build elsewhere.
#
# The base is the oldest supported distribution (Ubuntu 24.04 LTS): building
# with its glibc 2.39 and WebKitGTK 2.44 keeps the AppImage runnable on newer
# hosts such as Debian 13.

ARG BASE_IMAGE=docker.m.daocloud.io/library/ubuntu:24.04
FROM ${BASE_IMAGE}

ARG DEBIAN_FRONTEND=noninteractive
# Plain HTTP: the minimal base image has no CA bundle yet, so HTTPS apt would
# fail before ca-certificates is installed.
ARG APT_MIRROR=http://mirrors.tuna.tsinghua.edu.cn/ubuntu
ARG GO_VERSION=1.27.3
ARG GO_MIRROR=https://mirrors.aliyun.com/golang
ARG NODE_VERSION=22.22.3
ARG NODE_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/nodejs-release
ARG APPIMAGETOOL_VERSION=1.9.0
ARG GITHUB_PROXY=https://gh-proxy.com/
ARG APPIMAGETOOL_SHA256=46fdd785094c7f6e545b61afcfb0f3d98d8eab243f644b4b17698c01d06083d1
# The type2 runtime is only published as a mutable "continuous" build, so it
# cannot be pinned by checksum. Pre-seed it here: upstream GitHub is not
# reachable during the build, and appimagetool refuses to run without it.
ARG APPIMAGE_RUNTIME_SHA256=
ARG TARGETARCH=amd64

RUN set -eux; \
    sed -i \
      -e "s|http://archive.ubuntu.com/ubuntu|${APT_MIRROR}|g" \
      -e "s|http://security.ubuntu.com/ubuntu|${APT_MIRROR}|g" \
      /etc/apt/sources.list.d/ubuntu.sources; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      build-essential \
      ca-certificates \
      curl \
      dbus \
      dbus-x11 \
      desktop-file-utils \
      file \
      git \
      libegl1 \
      libgl1-mesa-dri \
      libglib2.0-bin \
      libgtk-4-dev \
      libwebkitgtk-6.0-dev \
      pkg-config \
      procps \
      python3 \
      rsync \
      squashfs-tools \
      x11-utils \
      xauth \
      xvfb \
      xz-utils; \
    rm -rf /var/lib/apt/lists/*

# Pin both toolchains; GOTOOLCHAIN=local stops Go from fetching another one.
RUN set -eux; \
    curl -fsSL -o /tmp/go.tar.gz \
      "${GO_MIRROR}/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz; \
    /usr/local/go/bin/go version

RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) node_arch=x64 ;; \
      arm64) node_arch=arm64 ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/node.tar.xz \
      "${NODE_MIRROR}/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${node_arch}.tar.xz"; \
    mkdir -p /usr/local/node; \
    tar -C /usr/local/node --strip-components=1 -xJf /tmp/node.tar.xz; \
    rm /tmp/node.tar.xz; \
    /usr/local/node/bin/node --version

RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) appimage_arch=x86_64 ;; \
      arm64) appimage_arch=aarch64 ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /usr/local/bin/appimagetool \
      "${GITHUB_PROXY}https://github.com/AppImage/appimagetool/releases/download/${APPIMAGETOOL_VERSION}/appimagetool-${appimage_arch}.AppImage"; \
    if [ -n "${APPIMAGETOOL_SHA256}" ]; then \
      echo "${APPIMAGETOOL_SHA256}  /usr/local/bin/appimagetool" | sha256sum -c -; \
    fi; \
    chmod 0755 /usr/local/bin/appimagetool; \
    mkdir -p /usr/local/lib/appimage; \
    curl -fsSL -o /usr/local/lib/appimage/runtime-${appimage_arch} \
      "${GITHUB_PROXY}https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-${appimage_arch}"; \
    if [ -n "${APPIMAGE_RUNTIME_SHA256}" ]; then \
      echo "${APPIMAGE_RUNTIME_SHA256}  /usr/local/lib/appimage/runtime-${appimage_arch}" | sha256sum -c -; \
    fi; \
    chmod 0755 /usr/local/lib/appimage/runtime-${appimage_arch}; \
    ln -sf runtime-${appimage_arch} /usr/local/lib/appimage/runtime; \
    sha256sum /usr/local/lib/appimage/runtime-${appimage_arch}

ENV PATH=/usr/local/go/bin:/usr/local/node/bin:${PATH} \
    GOTOOLCHAIN=local \
    GOPROXY=https://goproxy.cn,direct \
    npm_config_registry=https://registry.npmmirror.com \
    npm_config_replace_registry_host=always \
    APPIMAGE_EXTRACT_AND_RUN=1 \
    APPIMAGE_RUNTIME_FILE=/usr/local/lib/appimage/runtime

WORKDIR /src
