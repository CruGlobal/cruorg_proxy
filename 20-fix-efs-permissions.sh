#!/bin/sh

set -e

fix_efs_permissions() {
  chmod -R 700 /var/run/openresty/mod_pagespeed
}

fix_efs_permissions

exit 0
