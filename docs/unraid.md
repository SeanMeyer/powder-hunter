# Unraid Setup

An Unraid Docker template is included at `unraid/powder-hunter.xml`.

## Installation

1. Build the image from the repo directory:

   ```bash
   docker build -t powder-hunter .
   ```

2. Copy the template to your Unraid boot drive:

   ```bash
   cp unraid/powder-hunter.xml /boot/config/plugins/dockerMan/templates-user/my-powder-hunter.xml
   ```

3. In the Unraid Docker tab, click **Add Container** and select **powder-hunter**.

4. Fill in your API keys and preferences in the form, then click **Apply**.

## Updating

```bash
cd /mnt/user/appdata/powder-hunter
git pull
docker build -t powder-hunter .
```

Then restart the container from the Unraid Docker UI.
