# Hosting Your Own StealthPipe Relay

Hosting a StealthPipe relay yourself can help reduce latency and improve stability.

The exact steps depend on your server provider, but the general process is:

1. Copy the following files to your server:
   - `Dockerfile`
   - All files under `src/`  
2. Set up the Docker container if your provider requires it. Using Docker is recommended, but you can also run it without Docker (though this may cause more issues). You can ask an AI assistant for help if needed.  
3. If your provider allows choosing a server location, pick the one closest to you to reduce latency and improve connection speed.  

Of course, you may always use the EXE file in the repository, but this is not recommended for production.

StealthPipe Relay updates frequently, so you will need to re-upload the files to your server to stay current.

> [!IMPORTANT]
> **Very very important**: Do not forget to set SECRET_KEY in environment variables in the provider! The steps depend on the specific provider you are using to host this on, but if you don't set it, it will be left empty and easily exploitable!

### Security Features (and how to disable them)

The server includes some basic security features to prevent bots from abusing the relay. It includes:
- Proof of Work for creating a session
- Packet size limit and bandwidth throttling (most providers have a limited GB/month throughput)

To disable them, modify the variables at the top of the source code.

## Hosting on Providers

Here are some free platforms to host the relay on:

### Hosting on Render

Render is great platform for personal use. Here are its limitations (free plan):
- Server pauses after 15 minutes of inactivity. This has a minimal impact on your playing experience, it starts within 15 seconds and the mod has retry logic to account for this.
- 100 GB/month outbound bandwidth. This is fine for personal use with a couple of friends.
- You are only given 0.1 CPUs. Again, this is fine when you're playing with less than 10 players.

**Steps**

Approximate steps to host on Render:

1. Go on render.com and create an account
2. It will require you to link to the GitHub repository. You have two options:
    - Link directly to the public relay repository. Your server will automatically update when the main relay updates.
    - You clone the public relay repository and then link to your repository instead. This allows you to modify the config and modify the relay, but it will not update automatically.
3. You can choose the server's location, choose the one that's closest to you
4. Configure everything else like name, etc.
5. After you're done, click deploy and access it via the given URL.

### Hosting on HuggingFace Spaces

Unlike Render, HuggingFace does not have a bandwidth limit and has an incredible amount of RAM and CPU offered to you for free (2 vCPUs and 18 GB of RAM). However, the relay process needs less than 32 MB of RAM and can run on less than 0.1 CPU for personal use.

> [!NOTE]
> If your network blocks domains ending in `.hf.space`, consider using another provider, like Oracle. Oracle allows more flexibility with domain names, though you may need to purchase one separately.

> [!WARNING]
> **Important**: Hosting relays on HF might be against their Terms of Service! It's recommended to choose another provider for a more stable experience. Host this relay on HF space at your own risk. They sweep spaces every few months.

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
