# Deploying Your Mate

One server runs everything with Docker Compose: the app, the database, the
video server, and Caddy for HTTPS. This guide uses AWS EC2 in Mumbai. Any
Ubuntu 24.04 server with a public IP works the same way.

| File | What it does |
|---|---|
| `docker-compose.prod.yml` | Production settings layered on `../docker-compose.yml`: Caddy with HTTPS, private internal ports, Secure cookies, rotated logs |
| `Caddyfile` | HTTPS for `APP_DOMAIN`. `/livekit/*` goes to the video server, everything else to the web app |
| `setup-server.sh` | Run once on a new server: Docker, 2 GB swap, firewall, security updates, nightly backup |
| `make-env.sh` | Run once: writes the server's `.env` with fresh random secrets |
| `deploy.sh` | Builds and starts the app, or updates it (`--pull`), then checks it answers over HTTPS |
| `backup.sh` | Database and uploads backup; runs nightly at 03:00 IST and keeps 14 days |
| `restore.sh` | Puts a backup back |

## 1. Before you start

- **A name for HTTPS.** Browsers only allow the camera on HTTPS, so video
  rooms need a name. Either:
  - **DuckDNS (free):** sign in at duckdns.org and add a name, e.g.
    `yourmate.duckdns.org`; or
  - **your own domain:** later, add an `A` record pointing to the server.
- **Mail.** A Gmail account with an App Password (see the main README), or
  another SMTP provider. Without it, signup codes aren't delivered.
- **An AWS account** with a budget alarm: Billing → Budgets → monthly budget of
  $10, alert at 80%.

## 2. Create the server (AWS console, region Asia Pacific (Mumbai))

1. **EC2 → Launch instance**
   - Name: `yourmate`
   - Image: **Ubuntu Server 24.04 LTS**, architecture **64-bit (Arm)**
   - Type: **t4g.medium** (2 vCPU, 4 GB). Video and image builds need the
     memory. t4g.small (2 GB) works for a quiet pilot without video.
   - Key pair: create one, download the `.pem`, keep it safe
   - Storage: **30 GB gp3**
2. **Security group** (inbound rules):

   | Type | Port | Source | For |
   |---|---|---|---|
   | SSH | 22 | My IP | you |
   | HTTP | 80 | Anywhere | certificate checks, redirect |
   | HTTPS | 443 | Anywhere | the app |
   | Custom UDP | 443 | Anywhere | HTTP/3 |
   | Custom TCP | 7881 | Anywhere | video, fallback when UDP is blocked |
   | Custom UDP | 52000–52100 | Anywhere | video |

3. **Elastic IP:** EC2 → Elastic IPs → Allocate, then Associate it with the
   instance. The address now survives restarts.
4. **Point the name at it:** on DuckDNS, put the Elastic IP in the box next
   to your name and click *update ip*. For your own domain, add an `A` record.
   Check with `ping yourmate.duckdns.org`; it should show the Elastic IP.

## 3. Set up the server

```bash
ssh -i yourmate.pem ubuntu@<elastic-ip>

git clone https://github.com/knovate211/yourmate.git
cd yourmate
sudo bash deploy/setup-server.sh
exit                                   # log out and back in, so docker works without sudo
```

## 4. Configure

```bash
ssh -i yourmate.pem ubuntu@<elastic-ip>
cd yourmate

APP_DOMAIN=yourmate.duckdns.org \
ACME_EMAIL=you@gmail.com \
ADMIN_EMAIL=you@gmail.com \
ADMIN_CAMPUS_SLUG=lpu \
SMTP_USERNAME=yourmate.app@gmail.com \
SMTP_PASSWORD=yourapppassword \
deploy/make-env.sh
```

- `ACME_EMAIL` receives certificate notices from Let's Encrypt.
- `ADMIN_EMAIL` is the first admin login. Its password is printed into `.env`
  (`grep ADMIN_PASSWORD .env`).
- To keep the App Password out of your shell history, leave `SMTP_PASSWORD`
  off this command and add it to `.env` afterwards with `nano .env`.

## 5. Deploy

```bash
deploy/deploy.sh
```

The first run builds every image, which takes about 10 minutes on a t4g.medium.
The script then waits until `https://<APP_DOMAIN>` answers. Caddy obtains the
certificate on its own the first time. Open the site, log in as the admin,
and sign up a test student to check that the email code arrives.

## Updating

```bash
cd yourmate && deploy/deploy.sh --pull
```

Database changes apply automatically: each service runs its new migrations
when it starts.

## Backups

- Nightly at 03:00 India time, into `/var/backups/yourmate`, kept for 14 days.
- Log: `/var/backups/yourmate/backup.log`. Run one now with
  `deploy/backup.sh`.
- **Copy them off the server.** A backup on the same disk doesn't survive
  losing the server. Either create an S3 bucket, give the instance access to
  it, install the AWS CLI (`sudo snap install aws-cli --classic`) and set
  `BACKUP_S3_BUCKET=<bucket>` in `.env`; or copy the folder off now and then:
  `scp -r -i yourmate.pem ubuntu@<ip>:/var/backups/yourmate .`
- Restore (this replaces the live data):
  ```bash
  deploy/restore.sh /var/backups/yourmate/db-YYYYMMDD-HHMM.dump /var/backups/yourmate/uploads-YYYYMMDD-HHMM.tar.gz --yes
  ```

## Everyday commands

```bash
alias yc='docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml'
yc ps                      # what's running
yc logs -f gateway         # follow one service's logs
yc restart identity        # restart one service
df -h /; free -h           # disk and memory
```

## When something's wrong

| Symptom | Check |
|---|---|
| `deploy.sh` says the site didn't answer over HTTPS | `ping <APP_DOMAIN>` shows the Elastic IP; ports 80/443 are open in the security group; `yc logs caddy` |
| Signup code never arrives | `yc logs identity`. With Gmail, "Username and Password not accepted" means the App Password or `SMTP_USERNAME` is wrong |
| Video room connects but nobody sees anyone | UDP 52000–52100 and TCP 7881 are open in the security group; `LIVEKIT_NODE_IP` in `.env` is the Elastic IP |
| Disk filling up | `docker system df`; `docker image prune -a` after a deploy |

## Cost (approximate; check current AWS pricing)

| Item | Per month |
|---|---|
| t4g.medium, Mumbai | ~$25 |
| 30 GB gp3 disk | ~$2.5 |
| Elastic IP (attached) | ~$3.6 |
| Data out beyond the free allowance | ~$0.11/GB (a 1-hour 720p interview is ~1 GB) |

New accounts' Free plan credits cover this for the first months. Upgrade the
account before the credits or the plan run out, or AWS stops the account and
the site goes down.
