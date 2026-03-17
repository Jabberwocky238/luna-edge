# cmd/master

## 职责
`master` 进程启动入口，负责组装控制面配置并启动 master engine。

## 架构
- 解析环境变量和命令行参数。
- 构造 `engine/master.Engine`。
- 启动 manage API、replication、证书调和器和 K8s bridge。

## 存在的问题
- 启动参数较多，配置收敛还不够。
- 业务语义主要散落在 engine 层，入口文件仍承担部分装配细节。
