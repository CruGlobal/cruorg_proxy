FROM 056154071827.dkr.ecr.us-east-1.amazonaws.com/base-image-openresty:latest
MAINTAINER cru.org <wmd@cru.org>

ARG RESOLVER

# Install luarocks
RUN apt-get update \
	&& apt-get install -y -q \
    unzip \
    gettext-base \
  && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

RUN mkdir -p /usr/src/luarocks \
  && curl -SL "http://luarocks.github.io/luarocks/releases/luarocks-2.4.2.tar.gz" -o luarocks.tar.gz  \
	&& tar -xzf luarocks.tar.gz -C /usr/src/luarocks --strip-components=1 \
	&& rm luarocks.tar.gz \
  && cd /usr/src/luarocks \
  && ./configure --prefix=/usr/local/openresty/luajit \
    --with-lua=/usr/local/openresty/luajit/ \
    --lua-suffix=jit \
    --with-lua-include=/usr/local/openresty/luajit/include/luajit-2.1 \
  && make build && make install \
  && /usr/local/openresty/luajit/bin/luarocks install lpeg

COPY monitor.html /home/app/webapp/public/monitor.html
COPY env.conf /usr/local/openresty/nginx/env.conf
COPY nginx-httpd.conf.template /usr/local/openresty/nginx/conf.d/webapp.conf.t

# Resolver IP needs to be dynamic for docker-compose tests to work.
RUN envsubst '$RESOLVER' < /usr/local/openresty/nginx/conf.d/webapp.conf.t > /usr/local/openresty/nginx/conf.d/webapp.conf \
  && rm /usr/local/openresty/nginx/conf.d/webapp.conf.t
COPY redirect.lua /home/app/redirect.lua
COPY target.lua /home/app/target.lua
