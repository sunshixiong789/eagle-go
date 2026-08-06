#!/bin/sh
# 为 Keycloak 建独立 database。
#
# 本地开发让 Keycloak 与业务共用一个 PostgreSQL 实例以省一个容器，
# 但 schema 必须隔离——Keycloak 有自己的迁移体系，和业务表混在一起
# 迟早会互相踩到。生产环境应各自独立部署。
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    SELECT 'CREATE DATABASE keycloak OWNER $POSTGRES_USER'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'keycloak')\gexec
EOSQL
