# Hosting Your Own StealthPipe Relay

Hosting a StealthPipe relay yourself can help reduce latency and improve stability.

The exact steps depend on your server provider, but the general process is:

1. Copy the following files to your server:
   - `Dockerfile`
   - All files under `src/`  
2. Set up the Docker container if your provider requires it. Using Docker is recommended, but you can also run it without Docker (though this may cause more issues). You can ask an AI assistant for help if needed.  
3. If your provider allows choosing a server location, pick the one closest to you to reduce latency and improve connection speed.  

StealthPipe Relay updates frequently, so you will need to re-upload the files to your server to stay current.

Below is a guide for hosting on HuggingFace Spaces, which is free and straightforward.

### Hosting on HuggingFace Spaces

> [!NOTE]
> If your network blocks domains ending in `.hf.space`, consider using another provider, like Oracle. Oracle allows more flexibility with domain names, though you may need to purchase one separately.

**Steps**

1. Log in or create a HuggingFace account.  
2. Click your profile picture (top right) and select **New Space** from the dropdown.  
3. Choose a name for your space. Avoid spaces or special characters.  
4. In your space, go to the **Files** tab at the top right.  
5. Click **Contribute**, then select **Upload files**.  
6. Upload the required files (`Dockerfile` and `src/*`).  
7. The Docker container will build automatically. Wait until the status changes to **Running**.  
8. Once running, access your relay at:  
https://[Your HuggingFace Username]-[Your Space Name].hf.space

If you run into issues, an AI assistant can help troubleshoot. Official support is not available 24/7.
