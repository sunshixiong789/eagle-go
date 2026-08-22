# 部署入口

当前发布单元只有 `admin`、`product`、`order` 三个服务。
它们共用构建模板，但分别生成镜像、运行迁移并独立部署。

- 本地完整环境：`make up`
- 单服务镜像：`make image SERVICE=product VERSION=v1.2.0`
- 全部镜像：`make images VERSION=v1.2.0`
- 编排文件：`docker-compose.yml`
- 网关规则：`gateway/nginx.conf`
- 详细发布顺序与端口：[../docs/deployment.md](../docs/deployment.md)

file、notification 是 admin 拥有的模块，不是独立容器。
