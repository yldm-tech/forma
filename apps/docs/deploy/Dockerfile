# The documentation as a static site, served from `/docs/` on the app's own domain.
#
# Two stages so the 18 MB of sources and the Mintlify toolchain stay out of the published image;
# what ships is the exported site and nginx.
FROM node:22-alpine AS export

RUN apk add --no-cache bash python3 unzip
WORKDIR /docs
COPY docs/ ./
RUN bash deploy/build.sh /out

FROM nginx:1.27-alpine

# `/docs` is part of the path the browser asks for, so the files live under that name rather than
# being stripped by the proxy: the exported HTML links to `/docs/...` and a stripped prefix would
# make every one of those a miss.
COPY --from=export /out /usr/share/nginx/html/docs
COPY docs/deploy/nginx.conf /etc/nginx/conf.d/default.conf

RUN addgroup -g 10001 -S docs \
    && adduser -u 10001 -S -G docs docs \
    && chown -R docs:docs /usr/share/nginx/html /var/cache/nginx /var/run \
    && touch /var/run/nginx.pid && chown docs:docs /var/run/nginx.pid

USER docs
EXPOSE 8080
