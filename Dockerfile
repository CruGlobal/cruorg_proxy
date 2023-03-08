FROM openresty/openresty:1.21.4.1-alpine-apk

LABEL com.datadoghq.ad.check_names='["nginx"]'
LABEL com.datadoghq.ad.init_configs='[{}]'
LABEL com.datadoghq.ad.instances='[{"nginx_status_url": "http://%%host%%:81/nginx_status/"}]'
LABEL com.datadoghq.ad.logs='[{"source": "nginx"}]'

HEALTHCHECK --interval=10s --timeout=5s CMD curl -f http://127.0.0.1:81/health-check || exit 1

RUN apk add --no-cache openresty-opm \
    && opm get 3scale/lua-resty-url

COPY usr/ /usr/

EXPOSE 80

CMD ["openresty", "-g", "daemon off;"]
