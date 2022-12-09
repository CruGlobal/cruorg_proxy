ARG OPENRESTY_VERSION=1.21.4.1
FROM openresty/openresty:$OPENRESTY_VERSION-bullseye-fat

LABEL com.datadoghq.ad.check_names='["nginx"]'
LABEL com.datadoghq.ad.init_configs='[{}]'
LABEL com.datadoghq.ad.instances='[{"nginx_status_url": "http://%%host%%:81/nginx_status/"}]'
LABEL com.datadoghq.ad.logs='[{"source": "nginx"}]'

HEALTHCHECK --interval=10s --timeout=5s CMD curl -f http://127.0.0.1:81/health-check || exit 1

ARG OPENRESTY_VERSION
RUN echo 'Acquire::Retries "3";' > /etc/apt/apt.conf.d/80-retries \
    && apt-get update \
    && apt-get install --no-install-recommends --fix-missing -y -q \
      build-essential \
      git \
      libuuid1 \
      openresty-openssl111-dev \
      openresty-pcre-dev \
      openresty-zlib-dev \
      uuid-dev \
      wget \
    && set -x \
    && mkdir -p /tmp/openresty \
    && mkdir -p /tmp/pagespeed-ngx/psol \
    && cd /tmp \
    && wget -qO- https://openresty.org/download/openresty-$OPENRESTY_VERSION.tar.gz | tar -xz -C openresty --strip-components 1 \
    && wget -qO- https://github.com/apache/incubator-pagespeed-ngx/archive/refs/tags/v1.13.35.2-stable.tar.gz | tar xz -C pagespeed-ngx --strip-components 1 \
    && wget -qO- https://dl.google.com/dl/page-speed/psol/1.13.35.2-x64.tar.gz | tar -xz -C pagespeed-ngx \
    && export LUAJIT_LIB="/usr/local/openresty/luajit/lib/" \
    && export LUAJIT_INC=$(ls -d -- /tmp/openresty/bundle/LuaJIT*/src)/ \
    && NGINX_OPTIONS=$(openresty -V 2>&1|grep -i "arguments"|cut -d ":" -f2-) \
    && cd /tmp/openresty/bundle/nginx-*/ \
    && eval ./configure $NGINX_OPTIONS --add-dynamic-module=/tmp/pagespeed-ngx \
    && make modules \
    && mkdir -p /usr/local/openresty/nginx/modules \
    && cp objs/ngx_pagespeed.so /usr/local/openresty/nginx/modules \
    && rm -rf /tmp/openresty /tmp/pagespeed-ngx \
    && apt-get remove -y --purge \
      build-essential \
      openresty-openssl111-dev \
      openresty-pcre-dev \
      openresty-zlib-dev \
      uuid-dev \
      wget \
    && rm -rf /var/lib/apt/lists/*

RUN opm get 3scale/lua-resty-url \
    && mkdir -p /var/run/openresty/mod_pagespeed \
    && mkdir /docker-entrypoint.d

COPY usr/ /usr/
COPY docker-entrypoint.sh /
COPY 10-envsubst-on-templates.sh /docker-entrypoint.d
ENTRYPOINT ["/docker-entrypoint.sh"]

ENV NGINX_ENVSUBST_TEMPLATE_DIR=/usr/local/openresty/nginx/templates
ENV NGINX_ENVSUBST_OUTPUT_DIR=/usr/local/openresty/nginx/conf

EXPOSE 80

CMD ["openresty", "-g", "daemon off;"]
