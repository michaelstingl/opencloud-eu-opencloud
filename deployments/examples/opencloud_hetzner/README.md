# OpenCloud Hetzner Deployment

This is a production-ready configuration for deploying OpenCloud on Hetzner Cloud with the following features:

- Domain: shniq.cloud
- SSL certificates: Let's Encrypt (auto-renewal)
- Authentication: Basic auth enabled for WebDAV clients
- Demo users: Disabled for production

## Quick Start

1. Clone this repository on your Hetzner server:

```bash
mkdir -p /opt/opencloud
cd /opt/opencloud
git clone https://github.com/opencloud-eu/opencloud.git .
git checkout hetzner-deployment
cd deployments/examples/opencloud_hetzner
```

2. Edit the `.env` file to set a secure admin password:

```bash
nano .env
```

3. Create necessary directories for persistent storage:

```bash
mkdir -p /opt/opencloud/config /opt/opencloud/data
```

4. Update the `.env` file to use these directories by uncommenting and setting:

```
OC_CONFIG_DIR=/opt/opencloud/config
OC_DATA_DIR=/opt/opencloud/data
```

5. Start OpenCloud:

```bash
docker-compose -f docker-compose.yml -f opencloud.yml up -d
```

## Accessing Your OpenCloud Instance

- OpenCloud: https://shniq.cloud
- Admin Dashboard: Log in with username `admin` and the password from your `.env` file

## Adding Office Integration

To enable document editing with Collabora or OnlyOffice, uncomment the appropriate lines in the `.env` file and add the corresponding service file:

```bash
# For Collabora:
docker-compose -f docker-compose.yml -f opencloud.yml -f ../opencloud_full/collabora.yml up -d

# For OnlyOffice:
docker-compose -f docker-compose.yml -f opencloud.yml -f ../opencloud_full/onlyoffice.yml up -d
```

## Maintenance

- Updating OpenCloud:
```bash
docker-compose -f docker-compose.yml -f opencloud.yml pull
docker-compose -f docker-compose.yml -f opencloud.yml up -d
```

- Viewing logs:
```bash
docker-compose -f docker-compose.yml -f opencloud.yml logs -f
```