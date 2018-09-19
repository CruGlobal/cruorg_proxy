FROM 056154071827.dkr.ecr.us-east-1.amazonaws.com/base-image-openresty:latest
MAINTAINER cru.org <wmd@cru.org>

ARG RESOLVER
ARG AEM_ADDR
ARG DOMAIN
ARG S3_ADDR
ARG STORYLINES_ADDR
ARG REDIS_PORT_6379_TCP_ADDR_A
ARG REDIS_PORT_6379_TCP_ADDR_PS

RUN /usr/local/openresty/luajit/bin/luarocks install lua-resty-url

COPY target.lua /home/app/target.lua
COPY pagespeed-site.conf.template /usr/local/openresty/nginx/conf/pagespeed-site.conf.t
COPY location.conf.template /usr/local/openresty/nginx/conf/location.conf.t
COPY location_familylife.conf /usr/local/openresty/nginx/conf/location_familylife.conf
COPY env-site.conf /usr/local/openresty/nginx/env.d/env-site.conf
COPY nginx-httpd.conf /usr/local/openresty/nginx/conf.d/nginx-httpd.conf
COPY .htpasswd /usr/local/openresty/nginx/conf/.htpasswd

# Resolver IP needs to be dynamic for docker-compose tests to work.
RUN envsubst '$RESOLVER $S3_ADDR $STORYLINES_ADDR' < /usr/local/openresty/nginx/conf/location.conf.t > /usr/local/openresty/nginx/conf/location.conf \
  && rm /usr/local/openresty/nginx/conf/location.conf.t
RUN envsubst '$DOMAIN $PROXY_ADDR $REDIS_PORT_6379_TCP_ADDR_PS' < /usr/local/openresty/nginx/conf/pagespeed-site.conf.t > /usr/local/openresty/nginx/conf/pagespeed-site.conf \
  && rm /usr/local/openresty/nginx/conf/pagespeed-site.conf.t
