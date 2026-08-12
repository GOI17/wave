FROM golang:1.23-alpine

# Install dependencies
RUN apk add --no-cache \
    git \
    bash \
    make \
    build-base \
    curl \
    ca-certificates

# Set working directory
WORKDIR /app

# Copy project
COPY . /app/

# Build Wave
RUN make build

# Create test user
RUN addgroup -S testuser && adduser -S testuser -G testuser

# Set up environment
ENV HOME=/home/testuser
RUN mkdir -p $HOME && chown -R testuser:testuser $HOME

# Switch to test user
USER testuser

# Set entrypoint
ENTRYPOINT ["./wave"]

# Default command
CMD ["--help"]
