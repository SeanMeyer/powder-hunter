# Unraid Setup

## Installation

1. Download the template to your Unraid boot drive:

   ```bash
   curl -o /boot/config/plugins/dockerMan/templates-user/my-powder-hunter.xml \
     https://raw.githubusercontent.com/seanmeyer/powder-hunter/main/unraid/powder-hunter.xml
   ```

2. In the Unraid Docker tab, click **Add Container** and select **powder-hunter**.

3. Fill in your API keys, home location, and preferences in the form, then click **Apply**.

The image is pulled automatically from `ghcr.io/seanmeyer/powder-hunter:latest`.

## Updating

Click the container icon in the Docker tab and select **Update**. Unraid pulls the latest image automatically.
