FROM 056154071827.dkr.ecr.us-east-1.amazonaws.com/base-image-openresty:latest
MAINTAINER cru.org <wmd@cru.org>

ARG RESOLVER
ARG AEM_LB
ARG DOMAIN

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

RUN mkdir -p /home/app/webapp/public/mod_pagespeed && chmod 777 /home/app/webapp/public/mod_pagespeed \
  && mkdir -p /var/log/pagespeed && chmod 777 /var/log/pagespeed

COPY monitor.html /home/app/webapp/public/monitor.html
COPY env.conf /usr/local/openresty/nginx/env.conf
COPY nginx-httpd.conf.template /usr/local/openresty/nginx/conf.d/webapp.conf.t
COPY pagespeed.conf.template /usr/local/openresty/nginx/conf/pagespeed.conf.t

# Resolver IP needs to be dynamic for docker-compose tests to work.
RUN envsubst '$RESOLVER' < /usr/local/openresty/nginx/conf.d/webapp.conf.t > /usr/local/openresty/nginx/conf.d/webapp.conf \
  && rm /usr/local/openresty/nginx/conf.d/webapp.conf.t

RUN envsubst '$DOMAIN $AEM_LB' < /usr/local/openresty/nginx/conf/pagespeed.conf.t > /usr/local/openresty/nginx/conf/pagespeed.conf \
  && rm /usr/local/openresty/nginx/conf/pagespeed.conf.t

COPY redirect.lua /home/app/redirect.lua
COPY target.lua /home/app/target.lua

RUN mkdir -p /usr/local/openresty/nginx/cert
COPY cru.self.crt /usr/local/openresty/nginx/cert/cru.self.crt
COPY cru.self.key /usr/local/openresty/nginx/cert/cru.self.key
