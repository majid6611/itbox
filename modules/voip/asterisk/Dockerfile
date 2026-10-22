FROM alpine:3.22
# Asterisk 20 LTS straight from Alpine's own package — nothing compiled
# here. sounds-en is the voicemail/"number not available" prompts; without
# it voicemail still records but the caller hears silence.
RUN apk add --no-cache asterisk asterisk-sounds-en \
 && rm -rf /etc/asterisk/*
COPY conf/ /etc/asterisk/
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
ENTRYPOINT ["entrypoint.sh"]
