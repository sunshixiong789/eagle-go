# 就绪探针与摘流验收

容器显示 unhealthy 不会自动停止业务端口接收请求。生产入口必须持续探测应用的独立健康端口
`GET /readyz`，仅把 200 视为可接流；`/livez` 用于进程存活，不能用于判断策略是否已同步。

仓库提供 HAProxy 3.2 配置 `deploy/ingress/haproxy.cfg`。以下例子在 ECS 同机代理到已部署服务；
外层 ALB/网关负责 TLS，健康端口仅对代理和监控开放，业务端口也应通过防火墙限制为入口可访问。

```bash
export EAGLE_INGRESS_BIND=127.0.0.1:8080
export EAGLE_UPSTREAM_HOST=127.0.0.1
export EAGLE_UPSTREAM_PORT=8000
export EAGLE_HEALTH_PORT=9101
haproxy -c -f deploy/ingress/haproxy.cfg
haproxy -db -f deploy/ingress/haproxy.cfg
```

根据真实网络修改地址；多副本为每个实例增加 `server` 行，并给每个实例指定其独立健康端口。
不要通过负载均衡后的共享健康地址检测单个副本。样例连续两次检查失败摘流、两次成功恢复，检查间隔 1 秒。
当前授权策略持续落后 30 秒后 `/readyz` 返回 503；数据库版本查询失败立即返回 503。
健康探测、连接超时和在途请求会带来额外延迟，这不提供请求级即时撤权。

## 自动验收

安装官方 HAProxy 3.2（本次在 3.2.24 验证），然后执行：

```bash
HAPROXY_BIN=/absolute/path/to/haproxy make test-ingress
```

测试使用真实 HAProxy 配置、Eagle 的真实 `/readyz`/`/livez` handler、策略版本检查和临时 PostgreSQL，
不会连接生产数据库。它制造数据库策略版本领先且本地未重载的状态，等待真实 30 秒宽限期，然后检查：

1. 正常时请求穿过代理抵达业务 handler。
2. `/readyz` 变为 503 后，代理返回 503，连续请求不再抵达仍能响应 200 的业务端口。
3. `/livez` 持续 200，进程无需重启。
4. 重载策略后 `/readyz` 和代理恢复 200。

验收失败会输出代理日志并以非零状态退出。普通 `make test` 在未设置 `HAPROXY_BIN` 时跳过该外部进程测试，
CI 的 `all` 门禁会强制执行该测试，也可单独运行 `sh deploy/scripts/ci.sh ingress`。

## 实际部署验收

上线前在隔离预发布环境重复上述故障链路，并确认真实 ALB/网关的目标实例状态和访问日志：
使一个测试副本的策略同步持续失败，观察其就绪探针失败后不再获得新请求，其他副本继续接流；
恢复同步后确认自动回池。不能只看容器 health 状态或浏览器得到的 503。
仓库自动测试验证生产样例配置，但不代替实际部署的安全组、代理配置、TLS 和网络拓扑验收。
