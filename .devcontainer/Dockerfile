FROM mcr.microsoft.com/devcontainers/go:1-1.25-bookworm

# Remove stale Yarn apt repo — its GPG key is expired/missing, which breaks
# apt-get update during feature installation (docker-in-docker, kubectl, etc.)
RUN rm -f /etc/apt/sources.list.d/yarn.list \
    && rm -f /usr/share/keyrings/yarn-keyring.gpg \
    && apt-get update && apt-get clean
