#!/bin/sh
# 建 Keycloak 自己的 database。
#
# 本地开发让 Keycloak 与业务共用一个 PostgreSQL 实例以省一个容器，
# 但数据所有权必须隔离：业务库 eagle 由 POSTGRES_DB 创建，
# IdP 的表不应该混进去。生产环境两者应各自使用独立实例与账号。
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    SELECT format('CREATE DATABASE %I OWNER %I', name, '$POSTGRES_USER')
    FROM (VALUES
        ('keycloak')
    ) AS databases(name)
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = databases.name)\gexec
EOSQL
