# Allow overriding the base image registry (e.g. a mirror) at build time.
# Defaults keep the original Docker Hub references.
ARG NODE_IMAGE=node:22-alpine
ARG NGINX_IMAGE=nginx:1.25-alpine

# Build stage
FROM ${NODE_IMAGE} AS builder

WORKDIR /app

# if you located in China, you can use aliyun mirror to speed up
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# Install bash git dos2unix for TypeScript compiler
RUN apk add --no-cache bash git dos2unix

# if you located in China, you can use aliyun mirror to speed up
RUN npm config set registry https://registry.npmmirror.com

# Copy rush.json from root directory first
COPY rush.json ./

# Copy the frontend source code (excluding node_modules and build artifacts)
COPY frontend/ ./frontend/

# Copy common directory for Rush
COPY common/ ./common/

# Copy scripts directory for Rush hooks
COPY scripts/ ./scripts/

# Convert line endings for shell scripts in scripts directory to Unix format
RUN find . -name "*.sh" -type f -exec dos2unix {} \;

# The committed common/scripts/install-run*.js files have a 16-line Apache-2.0
# header prepended by the coze-dev maintainers on top of the stock Microsoft
# bootstrap scripts. Rush 5.147.1 SHA-compares these files against what it
# expects to ship and aborts with:
#   "ERROR: The standard files in the 'common/scripts' folders need to be
#    updated for this Rush version."
# Strip the prepended header so the file content matches upstream byte-for-byte.
# We only rewrite the file if the custom header is present, keeping this
# idempotent and safe if upstream ever reverts the change.
RUN for f in common/scripts/install-run.js \
             common/scripts/install-run-rush.js \
             common/scripts/install-run-rush-pnpm.js \
             common/scripts/install-run-rushx.js; do \
      if head -n 1 "$f" | grep -q '^/\*$' && sed -n '2p' "$f" | grep -q 'coze-dev Authors'; then \
        tail -n +17 "$f" > "$f.stripped" && mv "$f.stripped" "$f"; \
        echo "Stripped custom header from $f"; \
      fi; \
    done

# Install all dependencies
RUN chmod +x scripts/hooks/post-rush-install.sh && rm -rf /app/common/temp && node common/scripts/install-run-rush.js install

# Use rush build to build the specific project
RUN node common/scripts/install-run-rush.js build --to @coze-studio/app

# Production image stage
FROM ${NGINX_IMAGE}


# if you located in China, you can use aliyun mirror to speed up
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
# Install necessary tools for SSL configuration
RUN apk add --no-cache bash

# Copy build artifacts to nginx static directory
COPY --from=builder /app/frontend/apps/coze-studio/dist /usr/share/nginx/html

EXPOSE 8888

# Start nginx
CMD ["nginx", "-g", "daemon off;"]
