# --- Stage 1: build the sample app binary ---
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY sampleapp/go.mod sampleapp/go.sum ./
RUN go mod download
COPY sampleapp/ .
RUN CGO_ENABLED=0 go build -o /out/app .

# --- Stage 2: the "VM" itself ---
FROM debian:bookworm-slim

ARG TARGET_LABEL
ARG ENABLE_INITIAL_SERVICE=false

RUN apt-get update && apt-get install -y --no-install-recommends \
    systemd systemd-sysv dbus openssh-server sudo curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# "deploy" is the SSH user canopy connects as. sudoers grants exactly the
# two commands canopy's systemd deploy step needs — nothing broader.
RUN useradd -m -s /bin/bash deploy \
    && mkdir -p /home/deploy/.ssh \
    && echo "deploy ALL=(ALL) NOPASSWD: /usr/bin/tee, /usr/bin/systemctl" > /etc/sudoers.d/deploy \
    && chmod 440 /etc/sudoers.d/deploy
COPY keys/deploy_key.pub /home/deploy/.ssh/authorized_keys
RUN chown -R deploy:deploy /home/deploy/.ssh \
    && chmod 700 /home/deploy/.ssh \
    && chmod 600 /home/deploy/.ssh/authorized_keys

# "appuser" is the unprivileged user the systemd-managed service runs as.
RUN useradd -m -s /usr/sbin/nologin appuser

RUN mkdir -p /etc/canopy && echo "${TARGET_LABEL}" > /etc/canopy/target-label

COPY --from=build /out/app /opt/checkout-svc/bin/app
COPY vm/releases/ /opt/checkout-svc/releases/
RUN chmod +x /opt/checkout-svc/releases/*/app

COPY vm/checkout-svc.service /etc/systemd/system/checkout-svc.service
RUN if [ "$ENABLE_INITIAL_SERVICE" = "true" ]; then \
    systemctl enable checkout-svc.service ; \
    fi

RUN systemctl enable ssh \
    && mkdir -p /run/sshd

EXPOSE 22 8080
STOPSIGNAL SIGRTMIN+3
CMD ["/lib/systemd/systemd"]
