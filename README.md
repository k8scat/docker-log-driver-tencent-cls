# Tencent CLS Log Driver

Docker logging driver that forwards container logs to Tencent CLS.

## Quick Start

```bash
# Install plugin
docker plugin install k8scat/docker-log-driver-tencent-cls:latest --alias tencent-cls --grant-all-permissions

# Run container with the driver
docker run --log-driver=tencent-cls \
    --log-opt endpoint="<endpoint>" \
    --log-opt secret_id="<secret_id>" \
    --log-opt secret_key="<secret_key>" \
    --log-opt topic_id="<topic_id>" \
    your_image
```

## Installation

### Plugin Management

```bash
# Install
docker plugin install k8scat/docker-log-driver-tencent-cls:latest --alias tencent-cls --grant-all-permissions

# Upgrade
docker plugin disable tencent-cls --force
docker plugin upgrade tencent-cls k8scat/docker-log-driver-tencent-cls:latest --grant-all-permissions
docker plugin enable tencent-cls
systemctl restart docker

# Uninstall
docker plugin disable tencent-cls --force
docker plugin rm tencent-cls
```

## Configuration

### Container Level

Use with `docker run`:

```bash
docker run --log-driver=tencent-cls \
    --log-opt endpoint="<endpoint>" \
    --log-opt secret_id="<secret_id>" \
    --log-opt secret_key="<secret_key>" \
    --log-opt topic_id="<topic_id>" \
    --log-opt template="{container_name}: {log}" \
    your_image
```

Use with `docker-compose.yml`:

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

### Daemon Level (Default for All Containers)

Edit `/etc/docker/daemon.json`:

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

Restart Docker after changes: `systemctl restart docker`

## Options

| Option               | Required | Default                  | Description                                                                                  |
|----------------------|----------|--------------------------|----------------------------------------------------------------------------------------------|
| endpoint                  | Yes       |  | Tencent CLS Endpoint                                                                             |
| secret_id                | Yes      |                          | Tencent CLS Secret ID                                                                               |
| secret_key              | Yes      |                          | Tencent CLS Secret Key                                                                              |
| topic_id              | Yes      |                          | Tencent CLS Topic ID                                                                              |
| template             | No       | {log}                    | Message format template                                                                      |
| filter-regex         | No       |                          | Regex to filter logs                                                                         |
| retries              | No       | 10                        | Max retry attempts (0 = infinite)                                                            |
| timeout              | No       | 10s                      | API request timeout (units: ns, us/µs, ms, s, m, h)                                          |
| no-file              | No       | false                    | Disable log files (disables `docker logs`)                                                   |
| keep-file            | No       | false                    | Keep log files after container stop                                                          |
| mode                 | No       | blocking                 | Log processing mode: `blocking`/`non-blocking`                                               |
| max-buffer-size      | No       | 1m                       | Max buffer size (Example values: 32, 32b, 32B, 32k, 32K, 32kb, 32Kb, 32Mb, 32Gb, 32Tb, 32Pb) |
| batch-enabled        | No       | true                     | Enable batch sending                                                                         |
| batch-flush-interval | No       | 3s                       | Batch flush interval (units: ns, us/µs, ms, s, m, h)                                         |

### Template Tags

To customize the log message format using the `template` option, you can use the following tags:

| Tag                 | Description        |
|---------------------|--------------------|
| {log}               | Log message        |
| {timestamp}         | Log timestamp      |
| {container_id}      | Short container ID |
| {container_full_id} | Full container ID  |
| {container_name}    | Container name     |
| {image_id}          | Short image ID     |
| {image_full_id}     | Full image ID      |
| {image_name}        | Image name         |
| {daemon_name}       | Docker daemon name |
