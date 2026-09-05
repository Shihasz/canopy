FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    nginx openssh-server sudo curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash deploy \
    && mkdir -p /home/deploy/.ssh \
    && echo "deploy ALL=(ALL) NOPASSWD: /usr/bin/tee, /usr/sbin/nginx" > /etc/sudoers.d/deploy \
    && chmod 440 /etc/sudoers.d/deploy
COPY keys/deploy_key.pub /home/deploy/.ssh/authorized_keys
RUN chown -R deploy:deploy /home/deploy/.ssh \
    && chmod 700 /home/deploy/.ssh \
    && chmod 600 /home/deploy/.ssh/authorized_keys

COPY nginx/nginx.conf /etc/nginx/nginx.conf
RUN mkdir -p /etc/nginx/conf.d
COPY nginx/canopy-upstream.conf /etc/nginx/conf.d/canopy-upstream.conf

COPY nginx/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 22 80
ENTRYPOINT ["/entrypoint.sh"]
