# NPX-based lightweight Docker image for Tingly Box
# This image uses npm to install tingly-box globally, resulting in a smaller image size

ARG TINGLY_VERSION=latest
FROM node:20-slim

# Expose the default port
EXPOSE 12580

# Environment variables for configuration
ENV TINGLY_PORT=12580
ENV TINGLY_HOST=0.0.0.0

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for security. Home is pinned to /app (rather than
# left to useradd's system-account default) so it unambiguously matches the
# /app/.tingly-box path this image documents and mounts as a volume — the
# app resolves its config dir from $HOME/.tingly-box (pkg/fs.GetUserPath).
RUN groupadd -r tingly && \
    useradd -r -g tingly -d /app tingly

# update modules, spec version to confirm security
RUN npm install -g npm@10.8.2
RUN npm install -g pm2@7.0.1

# Install tingly-box globally during build (as root)
RUN npm install -g tingly-box@${TINGLY_VERSION}

# Grant tingly user access to npm global directories and cache
RUN chown -R tingly:tingly /usr/local/lib/node_modules /usr/local/bin /root/.npm

# Set working directory
WORKDIR /app

# Create the config/data directory (memory, logs and db all live under this
# single tree, see internal/config/app_config.go) with proper permissions.
RUN mkdir -p /app/.tingly-box && \
    chown -R tingly:tingly /app

# Entrypoint fixes up ownership of a bind-mounted data directory at runtime
# (a freshly `mkdir`ed host directory is owned by the host user, not the
# container's UID/GID, and the non-root "tingly" user can't write into it)
# before dropping from root down to "tingly". Deliberately does NOT switch
# to USER tingly here, so the entrypoint still starts as root.
COPY build/docker/docker-entrypoint-npx.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

# HOME is set here (after the npm/root install layers, so it doesn't perturb
# root's own npm cache/config lookups) and applies to the container's
# runtime env from this point on.
ENV HOME=/app

RUN su tingly -c "tingly-box version"

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD tingly-box version || exit 1

# Default command: run tingly-box
CMD ["sh", "-c", "echo '======================================' && \
    echo '  Tingly Box is starting up...' && \
    echo '  Installing version:' ${TINGLY_VERSION} && \
    echo '  Web UI will be available at:' && \
    echo '  http://localhost:'${TINGLY_PORT}'/dashboard?user_auth_token=tingly-box-user-token' && \
    echo '======================================' && \
    pm2 start \"tingly-box restart --host ${TINGLY_HOST} --port ${TINGLY_PORT} ${TINGLY_DEBUG:+--verbose --debug}\" --name tingly-box && \
    exec pm2 logs --raw"]


# Volume for persistent data (memory, logs and db all live under this tree)
VOLUME ["/app/.tingly-box"]
