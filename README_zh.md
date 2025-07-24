# 腾讯云 CLS 容器日志驱动

[English](./README.md)

将容器日志转发到腾讯云 CLS 的 Docker 日志驱动。

## 快速开始

```bash
# 安装插件
docker plugin install k8scat/docker-log-driver-tencent-cls:latest \
  --alias tencent-cls \
  --grant-all-permissions

# 使用该驱动运行容器
docker run --log-driver=tencent-cls \
    --log-opt endpoint="<endpoint>" \
    --log-opt secret_id="<secret_id>" \
    --log-opt secret_key="<secret_key>" \
    --log-opt topic_id="<topic_id>" \
    your_image
```

## 安装

### 插件管理

```bash
# 安装
docker plugin install k8scat/docker-log-driver-tencent-cls:latest \
  --alias tencent-cls \
  --grant-all-permissions

# 升级
docker plugin disable tencent-cls --force
docker plugin upgrade tencent-cls k8scat/docker-log-driver-tencent-cls:latest \
  --grant-all-permissions
docker plugin enable tencent-cls
systemctl restart docker

# 卸载
docker plugin disable tencent-cls --force
docker plugin rm tencent-cls
```

## 配置

### 容器级别

与 `docker run` 一起使用：

```bash
docker run --log-driver=tencent-cls \
    --log-opt endpoint="<endpoint>" \
    --log-opt secret_id="<secret_id>" \
    --log-opt secret_key="<secret_key>" \
    --log-opt topic_id="<topic_id>" \
    --log-opt template="{container_name}: {log}" \
    your_image
```

与 `docker-compose.yml` 一起使用：

```yaml
version: '3.8'
services:
  app:
    image: your/image
    logging:
      driver: tencent-cls
      options:
        endpoint: "<endpoint>"
        secret_id: "<secret_id>"
        secret_key: "<secret_key>"
        topic_id: "<topic_id>"
        template: "{container_name}: {log}"
```

### 守护进程级别（所有容器的默认设置）

编辑 `/etc/docker/daemon.json`：

```json
{
  "log-driver": "tencent-cls",
  "log-opts": {
    "endpoint": "<endpoint>",
    "secret_id": "<secret_id>",
    "secret_key": "<secret_key>",
    "topic_id": "<topic_id>"
  }
}
```

修改后重启 Docker：`systemctl restart docker`

## 选项

| 选项                           | 必需     | 默认值   | 描述                                                                                                                                               |
| ------------------------------ | -------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| endpoint                       | 是       |          | 腾讯云 CLS 端点                                                                                                                                     |
| secret_id                      | 是       |          | 腾讯云 CLS 密钥 ID                                                                                                                                  |
| secret_key                     | 是       |          | 腾讯云 CLS 密钥                                                                                                                                     |
| topic_id                       | 是       |          | 腾讯云 CLS 主题 ID                                                                                                                                  |
| template                       | 否       | {log}    | 消息格式模板                                                                                                                                       |
| filter-regex                   | 否       |          | 过滤日志的正则表达式                                                                                                                               |
| retries                        | 否       | 10       | 最大重试次数（0 = 无限）                                                                                                                           |
| timeout                        | 否       | 10s      | API 请求超时时间（单位：ns, us/µs, ms, s, m, h）                                                                                                   |
| no-file                        | 否       | false    | 禁用日志文件（禁用 `docker logs`）                                                                                                                 |
| keep-file                      | 否       | true     | 容器停止后保留日志文件                                                                                                                             |
| mode                           | 否       | blocking | 日志处理模式：`blocking`/`non-blocking`                                                                                                            |
| instance_info                  | 否       |          | JSON 格式的实例信息                                                                                                                                 |
| append_container_details_keys  | 否       |          | 追加容器详情键，用逗号分隔。可用键：`container_id`, `container_name`, `container_image_id`, `container_image_name` |

### 模板标签

要使用 `template` 选项自定义日志消息格式，您可以使用以下标签：

| 标签                 | 描述           |
| ------------------- | -------------- |
| {log}               | 日志消息       |
| {timestamp}         | 日志时间戳     |
| {container_id}      | 短容器 ID      |
| {container_full_id} | 完整容器 ID    |
| {container_name}    | 容器名称       |
| {image_id}          | 短镜像 ID      |
| {image_full_id}     | 完整镜像 ID    |
| {image_name}        | 镜像名称       |
| {daemon_name}       | Docker 守护进程名称 | 