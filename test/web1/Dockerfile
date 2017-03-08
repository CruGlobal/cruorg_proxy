FROM 056154071827.dkr.ecr.us-east-1.amazonaws.com/base-image-openresty:latest
MAINTAINER cru.org <wmd@cru.org>

COPY nginx-httpd.conf /usr/local/openresty/nginx/conf.d/webapp.conf
COPY files /home/app/webapp/public
